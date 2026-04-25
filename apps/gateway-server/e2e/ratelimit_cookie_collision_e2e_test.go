//go:build e2e

// Package e2e — cookie-collision rate-limit fall-through scenario.
// Pins the security contract for the cookie keyBy strategy: when
// the inbound Cookie header carries two cookies with the same name
// (RFC 6265 permits this but it is a strong injection signal), the
// rate-limit resolver treats the cookie strategy as unresolvable
// and falls through to the next keyBy candidate.
//
// Sibling of reload_test.go (synthetic-handler/KV-Put pattern reused
// here) and contract_test.go (rate-limit assertions on the wire).
// The route is registered via direct KV Put because example-app
// does not host any cookie-keyed rate-limited route — the demo
// surface focuses on header keyBy. Building a local synthetic
// route keeps the production code unchanged while exercising the
// cookie-collision branch end-to-end.
//
// Wire shape:
//   - Cookie: session=victim_id; session=attacker_id (RFC 6265 §5.4
//     allows multiple cookies in one header; the gateway parser
//     observes both, sees the duplicate name, and bumps
//     CookieCollisionCount).
//   - keyBy: ['cookie:session', 'ip'] — cookie strategy returns
//     (collided=true), resolver continues to 'ip', resolves to the
//     loopback peer (127.0.0.1) under the default trusted-proxy
//     profile.
//   - rps:2, burst:2 — the first two requests pass; the third
//     observes 429 with the X-RateLimit-* triplet on the wire.
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

// kvRateLimitMeta mirrors registry.RateLimitMeta for the KV Put
// path used by this test. Declared locally so the e2e package
// stays decoupled from registry internals — the wire contract is
// the JSON shape, not the Go struct identity.
type kvRateLimitMeta struct {
	RPS   int      `json:"rps"`
	Burst int      `json:"burst,omitempty"`
	KeyBy []string `json:"keyBy,omitempty"`
	Store string   `json:"store,omitempty"`
}

// kvEntryWithRateLimit extends the reload_test.go kvEntry with
// the rateLimit field. Reload tests do not write rateLimit and
// therefore do not need the field on their local type; this file
// declares its own to avoid mutating the reload test's shape.
type kvEntryWithRateLimit struct {
	HTTP      *kvHTTPMeta      `json:"http,omitempty"`
	RateLimit *kvRateLimitMeta `json:"rateLimit,omitempty"`
}

// TestE2E_RateLimit_CookieCollisionFallsThroughToIP pins the
// cookie-collision fall-through contract. Setup:
//
//   - Synthetic route /rl/cookie-collide registered via direct KV
//     Put with rateLimit: { rps: 2, burst: 2, keyBy:
//     ['cookie:session', 'ip'] }.
//   - Synthetic NATS subscriber answers 200 for any request that
//     reaches the handler. Requests rejected by the rate-limit
//     gate never reach this subscriber.
//   - Five sequential requests, each carrying
//     `Cookie: session=victim_id; session=attacker_id`.
//
// Expected behaviour: the cookie strategy collides on every
// request (duplicate `session` cookie), so the resolver falls
// through to 'ip' for every request. All five requests therefore
// share the loopback peer's bucket. With rps:2, burst:2 the first
// two requests succeed; the remaining three are rejected with 429
// and the X-RateLimit-* triplet on the wire.
//
// Pinning intent:
//   - Negative-space security: an attacker who CAN inject a
//     duplicate Cookie header (e.g., via response splitting)
//     MUST NOT be able to bypass per-cookie rate limiting by
//     sandwiching their session next to the victim's. The
//     fall-through preserves the default IP-keyed budget.
//   - Header observability: rejected requests carry the
//     X-RateLimit-* triplet so retry tooling sees the same shape
//     it does on a normal 429 — the gate fired, no special-case
//     skipping of header emission on the collision branch.
func TestE2E_RateLimit_CookieCollisionFallsThroughToIP(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-cookie-collide"
		pattern = "cookie.collide.probe"
		path    = "/rl/cookie-collide"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	t.Cleanup(func() { _ = fx.kv.Delete(fx.ctx, key) })

	// Synthetic handler — answers any request that reaches it
	// with a trivial 200. Any 429 in the assertions below was
	// emitted by the gateway's rate-limit gate, not by the
	// handler.
	sub, err := fx.nc.Subscribe(subject, func(msg *nats.Msg) {
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

	// Register the synthetic route with cookie-keyed rate-limit.
	entry := kvEntryWithRateLimit{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		RateLimit: &kvRateLimitMeta{
			RPS:   2,
			Burst: 2,
			KeyBy: []string{"cookie:session", "ip"},
		},
	}
	entryBytes, err := json.Marshal(entry)
	require.NoError(t, err)

	_, err = fx.kv.Put(fx.ctx, key, entryBytes)
	require.NoError(t, err, "put cookie-collide KV entry")

	// Wait for the watcher to incorporate the new route. The
	// reload_test.go pattern proves this converges within a
	// single watcher tick (≤ 100ms typically); 5s is the same
	// safety margin used elsewhere.
	waitForRouteStatusWithCookie(t, path, http.StatusOK)

	// Drain residual bucket state from any previous run on the
	// loopback peer — rps=2 means a 1.1s gap is enough.
	time.Sleep(1100 * time.Millisecond)

	// Fire 5 sequential requests with a duplicate-name cookie
	// header. Each request bumps the cookie-collision counter on
	// the gateway side and falls through to the IP bucket; all
	// 5 share one bucket on 127.0.0.1.
	statuses, lastRejectedHeaders := burstWithDuplicateCookies(t, gatewayURL+path, 5)
	t.Logf("cookie-collision burst: statuses=%v", statuses)

	// First two requests inside the burst MUST succeed.
	assert.GreaterOrEqual(t, statuses[http.StatusOK], 2,
		"rps:2 burst:2 — first two requests in the IP bucket must succeed")

	// Remaining three MUST be rejected with 429.
	assert.GreaterOrEqual(t, statuses[http.StatusTooManyRequests], 3,
		"after the burst is exhausted on the IP fallback, remaining requests must 429; statuses=%v", statuses)

	// A rejected response MUST carry the X-RateLimit-* triplet.
	// The cookie-collision branch shares the same header path as
	// any other rate-limit reject — a regression that swallowed
	// the headers on the fall-through path would land here.
	require.NotNil(t, lastRejectedHeaders, "must observe at least one 429 to assert headers")
	assert.NotEmpty(t, lastRejectedHeaders.Get("X-RateLimit-Limit"),
		"rejected response must carry X-RateLimit-Limit even when the cookie strategy collided")
	assert.Equal(t, "0", lastRejectedHeaders.Get("X-RateLimit-Remaining"),
		"X-RateLimit-Remaining MUST be 0 on a real 429")
	assert.NotEmpty(t, lastRejectedHeaders.Get("Retry-After"),
		"Retry-After must reach the wire on the cookie-collision fall-through 429")
}

// burstWithDuplicateCookies fires `count` sequential requests
// against url, each carrying a Cookie header with two cookies
// sharing the name `session`. Returns the per-status counts plus
// the headers observed on the last rejected (429) response — the
// caller asserts the rate-limit triplet on that last seen
// rejection rather than every individual one, because the headers
// are identical across all rejections within the same window.
func burstWithDuplicateCookies(t *testing.T, url string, count int) (map[int]int, http.Header) {
	t.Helper()

	statuses := make(map[int]int, 4)
	var lastRejectedHeaders http.Header

	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < count; i++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		require.NoError(t, err)

		// RFC 6265 §5.4: a single Cookie header may carry multiple
		// `name=value` pairs separated by `; `. Two pairs with the
		// same name (`session`) is the duplicate-cookie shape the
		// gateway parses as a collision.
		req.Header.Set("Cookie", "session=victim_id; session=attacker_id")

		resp, err := client.Do(req)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		statuses[resp.StatusCode]++
		if resp.StatusCode == http.StatusTooManyRequests {
			lastRejectedHeaders = resp.Header.Clone()
		}
	}

	return statuses, lastRejectedHeaders
}

// waitForRouteStatusWithCookie polls the gateway until GET path
// (with the duplicate-cookie header) returns `want` or
// reloadWaitTimeout elapses. Mirrors waitForRouteStatus from
// reload_test.go but sends the cookie so the readiness probe
// behaves identically to the load-phase requests — a route that
// becomes routable but rejects the cookie-bearing probe with 429
// pre-warm would otherwise land us in a confusing state.
func waitForRouteStatusWithCookie(t *testing.T, path string, want int) {
	t.Helper()

	deadline := time.Now().Add(reloadWaitTimeout)
	client := &http.Client{Timeout: 2 * time.Second}
	url := gatewayURL + path

	var lastStatus int
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		require.NoError(t, err)
		req.Header.Set("Cookie", "session=victim_id; session=attacker_id")

		resp, err := client.Do(req)
		if err == nil {
			lastStatus = resp.StatusCode
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == want {
				// Drain the bucket so the load phase starts from
				// a clean state. The probe call we just made
				// counted against the burst.
				time.Sleep(1100 * time.Millisecond)
				return
			}
		}
		time.Sleep(reloadPollInterval)
	}

	t.Fatalf("gateway never observed status %d on %s within %s (last=%d)",
		want, path, reloadWaitTimeout, lastStatus)
}
