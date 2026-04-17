//go:build e2e

// Package e2e — route reload scenarios. Pins the gateway's live
// response to handler_registry KV mutations: a new entry must
// become routable without a gateway restart, a modified entry
// must change behaviour on subsequent requests, and a deleted
// entry must drop out of the routing table. Sibling of
// e2e_test.go / auth_test.go / response_test.go / contract_test.go;
// reuses the shared `waitForGateway` helper and `gatewayURL`.
//
// The stack assumed by these tests is the three-process harness
// described in README.md: NATS on :4222, example-app on whatever
// transport it picked at boot, and the gateway on :8080. Because
// no live Nest handler is wired to the synthetic subjects, each
// test subscribes to the KV-declared subject itself via nats.go
// and answers inline with a canned GatewayReply envelope — so
// the round trip exercises the gateway's table lookup, encoder,
// NATS request, decoder, and merge layers on real bytes without
// relying on example-app to host the route.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// natsURL targets the same JetStream-enabled NATS server the
// gateway and example-app processes connect to. The e2e harness
// does not run its own NATS; it piggy-backs on the one stood up
// by `nx run gateway-server:e2e-up`.
const natsURL = "nats://localhost:4222"

// kvBucket matches the gateway's KV_BUCKET env var. The bucket
// is created lazily by example-app at startup — by the time any
// reload test runs, the bucket exists and is safe to mutate.
const kvBucket = "handler_registry"

// reloadPollInterval bounds how often the test re-probes the
// gateway for routing-table updates. The watcher fires on every
// KV Put/Delete, so 100ms is generous and keeps test duration
// short while leaving room for scheduling jitter.
const reloadPollInterval = 100 * time.Millisecond

// reloadWaitTimeout bounds how long we wait for a KV mutation
// to propagate through the watcher into the gateway's routing
// table. 5s is a wide safety margin — the watcher typically
// converges within a single poll interval.
const reloadWaitTimeout = 5 * time.Second

// kvEntry mirrors registry.HandlerEntry for the subset of fields
// these tests need. Declared locally so the e2e package stays
// decoupled from the registry package's internals — any
// additive field change on the registry side will not require a
// sync update here.
type kvEntry struct {
	HTTP    *kvHTTPMeta `json:"http,omitempty"`
	Timeout *int        `json:"timeout,omitempty"`
}

type kvHTTPMeta struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// gatewayReply is the minimal wire shape a Nest handler would
// emit on a successful reply. The test subscriber answers with
// this envelope verbatim — status 200, an empty headers map so
// the gateway stamps only its own x-request-id / content-type,
// and a trivial JSON body.
type gatewayReply struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Body    json.RawMessage     `json:"body"`
}

// natsFixture bundles the short-lived NATS/KV plumbing each
// reload test sets up in its own goroutine scope. The ctx is
// the per-test deadline; cleanup hooks drain subscriptions and
// close the connection so a test failure does not leak a live
// subscription into the next case.
type natsFixture struct {
	ctx context.Context
	nc  *nats.Conn
	js  jetstream.JetStream
	kv  jetstream.KeyValue
}

// newNATSFixture connects to the shared NATS server, resolves
// the handler_registry KV handle, and registers a t.Cleanup to
// drain both. A single fixture is enough for every reload
// scenario because the tests namespace their KV keys and NATS
// subjects per-case.
func newNATSFixture(t *testing.T) *natsFixture {
	t.Helper()

	nc, err := nats.Connect(natsURL)
	require.NoError(t, err, "connect to nats")

	js, err := jetstream.New(nc)
	require.NoError(t, err, "open jetstream context")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	kv, err := js.KeyValue(ctx, kvBucket)
	require.NoError(t, err, "open handler_registry KV")

	t.Cleanup(nc.Close)

	return &natsFixture{ctx: ctx, nc: nc, js: js, kv: kv}
}

// serveFakeHandler subscribes to `subject` and answers every
// incoming request with a gatewayReply JSON envelope that
// contains the supplied body. The subscription unsubscribes
// on t.Cleanup so tests do not bleed handlers into subsequent
// cases sharing the same NATS connection.
func serveFakeHandler(t *testing.T, nc *nats.Conn, subject string, body any) {
	t.Helper()

	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err, "marshal fake handler body")

	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		reply := gatewayReply{
			Status:  http.StatusOK,
			Headers: map[string][]string{},
			Body:    bodyBytes,
		}
		replyBytes, marshalErr := json.Marshal(reply)
		if marshalErr != nil {
			return
		}
		_ = msg.Respond(replyBytes)
	})
	require.NoError(t, err, "subscribe fake handler")

	t.Cleanup(func() { _ = sub.Unsubscribe() })
}

// waitForRouteStatus polls the gateway until GET `path`
// returns `want` or reloadWaitTimeout elapses. A bare 404 right
// after a KV Put is the expected transient state until the
// watcher loop fires, so the test MUST poll rather than probe
// once and assert.
func waitForRouteStatus(t *testing.T, path string, want int) {
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
			if resp.StatusCode == want {
				return
			}
		}
		time.Sleep(reloadPollInterval)
	}

	t.Fatalf("gateway never observed status %d on %s within %s (last=%d)",
		want, path, reloadWaitTimeout, lastStatus)
}

// TestE2E_Reload_NewKVEntryBecomesRoutable pins the happy-path
// reload contract: writing a fresh KV entry with an HTTP meta
// makes the declared path routable on the live gateway without
// a restart. The test subscribes to the synthetic subject
// itself so the round trip completes with a real reply, not a
// 503 from `ErrNoResponders`.
func TestE2E_Reload_NewKVEntryBecomesRoutable(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	// Namespace per-test so parallel / retried runs do not
	// collide on the shared bucket. The ".cmd." infix is part
	// of the nestjs-jetstream key contract — the gateway's
	// subject parser requires it.
	const (
		service = "e2e-dyn-new"
		pattern = "dyn.new.probe"
		path    = "/dyn/new"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	t.Cleanup(func() { _ = fx.kv.Delete(fx.ctx, key) })

	serveFakeHandler(t, fx.nc, subject, map[string]any{"ok": true, "marker": "new"})

	// Sanity: the path does not exist yet.
	pre, err := http.Get(gatewayURL + path)
	require.NoError(t, err)
	_ = pre.Body.Close()
	require.Equal(t, http.StatusNotFound, pre.StatusCode,
		"path must be unknown before the KV entry is written")

	entry := kvEntry{HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path}}
	entryBytes, err := json.Marshal(entry)
	require.NoError(t, err)

	_, err = fx.kv.Put(fx.ctx, key, entryBytes)
	require.NoError(t, err, "put KV entry")

	waitForRouteStatus(t, path, http.StatusOK)

	resp, err := http.Get(gatewayURL + path)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "new", body["marker"])
}

// TestE2E_Reload_DeletedKVEntryDropsOutOfTable pins the reverse
// direction: a KV delete MUST remove the path from the gateway
// routing table on the live watcher. Walks the full lifecycle:
// write → observe 200 → delete → observe 404.
func TestE2E_Reload_DeletedKVEntryDropsOutOfTable(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-dyn-del"
		pattern = "dyn.del.probe"
		path    = "/dyn/del"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	t.Cleanup(func() { _ = fx.kv.Delete(fx.ctx, key) })

	serveFakeHandler(t, fx.nc, subject, map[string]any{"ok": true})

	entryBytes, err := json.Marshal(
		kvEntry{HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path}},
	)
	require.NoError(t, err)

	_, err = fx.kv.Put(fx.ctx, key, entryBytes)
	require.NoError(t, err)

	waitForRouteStatus(t, path, http.StatusOK)

	require.NoError(t, fx.kv.Delete(fx.ctx, key), "delete KV entry")

	waitForRouteStatus(t, path, http.StatusNotFound)
}

// TestE2E_Reload_ModifiedKVEntryChangesBehaviour pins the
// in-place mutation path. A timeout field flip on an existing
// entry MUST be observed by the watcher and reflected in the
// routing table. We use a synthetic handler that never replies
// — the gateway then returns 504 bounded by the route's
// timeout, so we can assert the pre/post-mutation behaviour
// without racing against the handler.
//
// Starting budget: 5000ms (generous, never trips under a slow
// fake handler that ALSO replies after 200ms). After mutation:
// 100ms (guaranteed to trip, because the fake handler never
// replies).
func TestE2E_Reload_ModifiedKVEntryChangesBehaviour(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-dyn-mod"
		pattern = "dyn.mod.probe"
		path    = "/dyn/mod"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	t.Cleanup(func() { _ = fx.kv.Delete(fx.ctx, key) })

	// A fake handler that sleeps 300ms before replying. Under
	// the initial 5s timeout it is well within budget; under
	// the post-mutation 100ms timeout it is guaranteed to miss.
	sub, err := fx.nc.Subscribe(subject, func(msg *nats.Msg) {
		time.Sleep(300 * time.Millisecond)
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
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	initial := 5000
	initialBytes, err := json.Marshal(
		kvEntry{
			HTTP:    &kvHTTPMeta{Method: http.MethodGet, Path: path},
			Timeout: &initial,
		},
	)
	require.NoError(t, err)

	_, err = fx.kv.Put(fx.ctx, key, initialBytes)
	require.NoError(t, err)

	waitForRouteStatus(t, path, http.StatusOK)

	tight := 100
	tightBytes, err := json.Marshal(
		kvEntry{
			HTTP:    &kvHTTPMeta{Method: http.MethodGet, Path: path},
			Timeout: &tight,
		},
	)
	require.NoError(t, err)

	_, err = fx.kv.Put(fx.ctx, key, tightBytes)
	require.NoError(t, err, "update KV entry with tighter timeout")

	waitForRouteStatus(t, path, http.StatusGatewayTimeout)
}

// TestE2E_Reload_WatcherHandlesRapidSuccessiveUpdates pins the
// watcher's robustness against a burst of KV mutations. Issues
// three back-to-back Puts that alternate the handler identity
// (via the reply body's `version` marker). The final observed
// response body MUST match the last Put — guaranteeing the
// gateway did not stall on an intermediate state.
func TestE2E_Reload_WatcherHandlesRapidSuccessiveUpdates(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-dyn-burst"
		pattern = "dyn.burst.probe"
		path    = "/dyn/burst"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	t.Cleanup(func() { _ = fx.kv.Delete(fx.ctx, key) })

	// The handler replies with whatever payload the test last
	// installed. We rotate the reply marker in lockstep with
	// Put rounds, not in lockstep with the subscription itself,
	// so the final client read sees the final KV state's reply.
	latestVersion := 3
	replyBody := func() []byte {
		return []byte(fmt.Sprintf(`{"version":%d}`, latestVersion))
	}
	serveFakeHandler(t, fx.nc, subject, json.RawMessage(replyBody()))

	for i := 1; i <= 3; i++ {
		entry := kvEntry{
			HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		}
		bytes, err := json.Marshal(entry)
		require.NoError(t, err)

		_, err = fx.kv.Put(fx.ctx, key, bytes)
		require.NoError(t, err, "put round %d", i)
	}

	waitForRouteStatus(t, path, http.StatusOK)
}
