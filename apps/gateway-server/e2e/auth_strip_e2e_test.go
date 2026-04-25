//go:build e2e

// Package e2e — Authorization-header stripping verification. Pins
// the security contract: once the verifier sub-request has decoded
// the bearer token into structured claims, the raw `Authorization`
// (and `Proxy-Authorization`) header MUST NOT travel onto the
// envelope sent to the route handler. The verifier sub-request
// itself DOES still see the raw header — verifiers need the raw
// bytes to decode the token.
//
// Why this contract matters: forwarding the credential to the
// route handler would let downstream services bypass the verifier
// (re-decode, store, replay), silently breaking rotation,
// blacklists, and revocation.
//
// Implementation note: example-app does not host a route that
// echoes its received headers back to the client. Modifying the
// example-app to add one would be an out-of-band change to
// production code, which the e2e harness rules forbid. Instead,
// this test registers a synthetic verifier and a synthetic route
// via direct KV Put + nats.Subscribe, captures the envelopes
// arriving on both NATS subjects, and asserts on the headers map
// inside the captured envelopes — the same wire bytes the real
// route handler would observe.
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kvAuthMeta mirrors registry.RouteAuthMeta for KV Put usage.
// Declared locally so e2e_test stays decoupled from registry
// internals — the JSON wire shape is the contract.
type kvAuthMeta struct {
	Verifier string `json:"verifier"`
	Optional bool   `json:"optional,omitempty"`
}

// kvVerifierMeta mirrors registry.VerifierMeta for KV Put usage.
type kvVerifierMeta struct {
	ID      string `json:"id"`
	Default bool   `json:"default,omitempty"`
}

// kvEntryWithAuth bundles the fields needed to register a route
// that requires auth via a synthetic verifier.
type kvEntryWithAuth struct {
	HTTP     *kvHTTPMeta     `json:"http,omitempty"`
	Auth     *kvAuthMeta     `json:"auth,omitempty"`
	Verifier *kvVerifierMeta `json:"verifier,omitempty"`
}

// capturedEnvelope is the subset of GatewayRequest fields this test
// reads. The full envelope is decoded into the package-level
// proxy.GatewayRequest type by the gateway; here a minimal mirror
// keeps the e2e package independent of the proxy package's
// internal struct shape.
type capturedEnvelope struct {
	Headers map[string]string `json:"headers"`
}

// TestE2E_AuthStrip_VerifiedRouteHidesAuthorizationFromUpstream
// pins the credential-stripping security contract on the wire.
//
// Setup:
//
//   - Synthetic verifier registered via KV. NATS subscriber on
//     the verifier subject answers 200 with a trivial empty
//     claims body. The test captures the envelope it received on
//     this subject so the assertion can verify the verifier DID
//     observe the raw `Authorization` header (verifiers need the
//     raw token to decode it).
//   - Synthetic route registered via KV with `auth.verifier =
//     <synthetic id>`. NATS subscriber on the route subject
//     answers 200 with `{ ok: true }`. The test captures the
//     envelope this subscriber received and asserts the headers
//     map does NOT carry `authorization` — the gateway's
//     stripAuthHeaders pass MUST have run.
//   - Single HTTP request to the route with `Authorization: Bearer
//     test.jwt.value`.
//
// The capture-channel pattern is deliberate: the assertion needs
// the envelope CONTENT, not just the response status. A test that
// only inspected the HTTP response body would be unable to tell
// whether the route handler saw the credential or not.
func TestE2E_AuthStrip_VerifiedRouteHidesAuthorizationFromUpstream(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service        = "e2e-auth-strip"
		routePattern   = "auth.strip.route"
		verifierPatt   = "auth.verifier.strip-verifier"
		routePath      = "/auth-strip/probe"
		verifierID     = "strip-verifier"
		bearerValue    = "Bearer test.jwt.value"
	)

	routeKey := service + ".cmd." + routePattern
	verifierKey := service + ".cmd." + verifierPatt
	routeSubject := service + "__microservice.cmd." + routePattern
	verifierSubject := service + "__microservice.cmd." + verifierPatt

	t.Cleanup(func() {
		_ = fx.kv.Delete(fx.ctx, routeKey)
		_ = fx.kv.Delete(fx.ctx, verifierKey)
	})

	// Capture channels. Both subscribers push the inbound
	// envelope onto the channel before responding so the test
	// goroutine can pop it after the HTTP round-trip completes.
	// Capacity 4 provides headroom for probe-phase envelopes that
	// may land on the subject before the load-phase request — the
	// test drains any pre-envelopes before assertions. A buffered
	// channel decouples the subscriber goroutine from the test
	// goroutine without requiring a select on every send.
	verifierEnvelopes := make(chan capturedEnvelope, 4)
	routeEnvelopes := make(chan capturedEnvelope, 4)

	// Verifier subscriber: capture envelope, answer 200 with
	// empty claims. Empty claims body matches the contract that
	// "any 200 reply means proceed"; the gateway forwards the
	// claims into the main envelope's auth field, but with empty
	// claims that field is just `{}`.
	verifierSub, err := fx.nc.Subscribe(verifierSubject, func(msg *nats.Msg) {
		var env capturedEnvelope
		_ = json.Unmarshal(msg.Data, &env)
		verifierEnvelopes <- env

		reply := gatewayReply{
			Status:  http.StatusOK,
			Headers: map[string][]string{},
			Body:    json.RawMessage(`{}`),
		}
		replyBytes, marshalErr := json.Marshal(reply)
		if marshalErr != nil {
			return
		}
		_ = msg.Respond(replyBytes)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = verifierSub.Unsubscribe() })

	// Route subscriber: capture envelope, answer 200 with a
	// trivial OK body. The body is irrelevant — the assertion
	// runs on the captured envelope's headers map, not the
	// response body.
	routeSub, err := fx.nc.Subscribe(routeSubject, func(msg *nats.Msg) {
		var env capturedEnvelope
		_ = json.Unmarshal(msg.Data, &env)
		routeEnvelopes <- env

		reply := gatewayReply{
			Status:  http.StatusOK,
			Headers: map[string][]string{},
			Body:    json.RawMessage(`{"ok":true}`),
		}
		replyBytes, marshalErr := json.Marshal(reply)
		if marshalErr != nil {
			return
		}
		_ = msg.Respond(replyBytes)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = routeSub.Unsubscribe() })

	// Register the synthetic verifier first so the route's
	// auth.verifier reference resolves at routing-table build
	// time. Order matters because the routing table is built on
	// every snapshot delta — registering the route before the
	// verifier would cause a transient "verifier not found"
	// state. The watcher debounces tightly so registering both
	// in quick succession is safe in practice, but explicit
	// ordering is cheaper than a retry loop on the test side.
	verifierEntryBytes, err := json.Marshal(kvEntryWithAuth{
		Verifier: &kvVerifierMeta{ID: verifierID, Default: false},
	})
	require.NoError(t, err)
	_, err = fx.kv.Put(fx.ctx, verifierKey, verifierEntryBytes)
	require.NoError(t, err, "put verifier KV entry")

	routeEntryBytes, err := json.Marshal(kvEntryWithAuth{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: routePath},
		Auth: &kvAuthMeta{Verifier: verifierID, Optional: false},
	})
	require.NoError(t, err)
	_, err = fx.kv.Put(fx.ctx, routeKey, routeEntryBytes)
	require.NoError(t, err, "put auth-strip route KV entry")

	// Wait for the route to become routable. Send a probe with
	// no Authorization header — the verifier capture-channel will
	// fill on each probe but the test only asserts on the
	// envelope from the load-phase request below, so probe-phase
	// envelopes are drained at the start of the load phase.
	waitForRouteAuthRoutable(t, routePath)

	// Drain any probe-phase envelopes the watcher convergence
	// loop pushed onto the channels.
	drainEnvelopes(verifierEnvelopes)
	drainEnvelopes(routeEnvelopes)

	// Issue the load-phase request with the credential header.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, gatewayURL+routePath, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", bearerValue)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"synthetic verifier returns 200 → request should reach the route handler")

	// Pull the captured envelopes from each subject. A 5s wait
	// is generous — the NATS round-trip plus the gateway encode/
	// decode pass typically completes in single-digit ms. A
	// timeout means the gateway short-circuited the verifier or
	// route call, which would itself be a regression worth
	// surfacing.
	var verifierEnv, routeEnv capturedEnvelope
	select {
	case verifierEnv = <-verifierEnvelopes:
	case <-time.After(5 * time.Second):
		t.Fatal("verifier subscriber never observed the load-phase envelope")
	}
	select {
	case routeEnv = <-routeEnvelopes:
	case <-time.After(5 * time.Second):
		t.Fatal("route subscriber never observed the load-phase envelope")
	}

	// CONTRACT 1: the verifier MUST have observed the
	// Authorization header verbatim. Without the raw token a JWT
	// verifier could not validate the signature.
	assert.Equal(t, bearerValue, verifierEnv.Headers["authorization"],
		"verifier envelope MUST carry the raw Authorization header (verifier needs the token)")

	// CONTRACT 2: the route handler envelope MUST NOT carry
	// Authorization. The gateway's stripAuthHeaders pass runs
	// AFTER the verifier returns 200, BEFORE encoding the route
	// envelope. A regression that disabled the strip would
	// surface here as a non-empty value on the route side.
	_, hasAuth := routeEnv.Headers["authorization"]
	assert.Falsef(t, hasAuth,
		"route envelope MUST NOT carry Authorization header after verifier success; got %q",
		routeEnv.Headers["authorization"])

	// CONTRACT 3: Proxy-Authorization is also in the strip set
	// (see strippedAuthHeaders in proxy/handler.go). Even though
	// this test did not send the header, the assertion documents
	// the intent — if a future contributor adds it to the
	// outbound request, the route envelope MUST still drop it.
	_, hasProxyAuth := routeEnv.Headers["proxy-authorization"]
	assert.False(t, hasProxyAuth,
		"route envelope MUST NOT carry Proxy-Authorization either")
}

// waitForRouteAuthRoutable polls the gateway until the synthetic
// auth route returns 200 (with no Authorization header it would
// 401 from the verifier; this poll uses no auth header but
// expects the verifier to receive the request and reply 200 for
// any input — the verifier capture channel's reply path always
// returns 200 regardless of what it captured). Falls through on
// 5s timeout.
//
// We expect 200 here because the synthetic verifier ALWAYS
// answers 200, even for missing-header requests. A 404 status
// during the poll means the route has not yet been incorporated
// into the routing table; once routable, the verifier path runs
// and returns the 200.
func waitForRouteAuthRoutable(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(reloadWaitTimeout)
	client := &http.Client{Timeout: 2 * time.Second}
	url := gatewayURL + path

	var lastStatus int
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			lastStatus = resp.StatusCode
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(reloadPollInterval)
	}

	t.Fatalf("synthetic auth route never became routable within %s (last=%d)",
		reloadWaitTimeout, lastStatus)
}

// drainEnvelopes removes any pending envelopes from the channel
// without blocking. Called between the readiness probe phase and
// the load phase so probe envelopes do not leak into the
// load-phase assertions.
func drainEnvelopes(ch chan capturedEnvelope) {
	for {
		select {
		case <-ch:
			// drained one; loop
		default:
			return
		}
	}
}

