//go:build e2e

// Package e2e — GatewayRoute contract scenarios. Sibling of
// e2e_test.go / auth_test.go / response_test.go; reuses their
// `waitForGateway` helper and the shared `gatewayURL` constant.
// Spin the stack up per the README protocol in this directory
// before running `go test -tags=e2e`.
//
// These tests pin the observable wire behaviour of the
// GatewayRoute contract surfaces — CORS (preflight + actual),
// rate limiting (429 + keyBy isolation), per-route timeout,
// response headers (per-route override + forRoot default), and
// cookie defaults from `GatewayModule.forRoot`. Each scenario is
// end-to-end: client → gateway-server → NATS → example-app →
// NATS → gateway-server → client, with the full merge chain
// exercised on real bytes.
package e2e

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// corsOrigin is the single explicit origin the contract-demo
// controller allow-lists on `/cors/public` and `/cors/creds`.
// Keep the literal in sync with the SDK-side route metadata.
const corsOrigin = "https://example.test"

// TestE2E_CORS_PreflightMatchedOriginReturns204 pins the core
// preflight contract: OPTIONS /cors/public with a matching Origin
// and `Access-Control-Request-Method: GET` resolves to 204 with
// the full set of CORS response headers (origin echo, allowed
// methods/headers, max-age, Vary: Origin). The gateway handles
// this without a NATS round-trip — the CORS config lives in the
// handler_registry KV and is resolved inline at the edge.
func TestE2E_CORS_PreflightMatchedOriginReturns204(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodOptions, gatewayURL+"/cors/public", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", corsOrigin)
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, corsOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), "GET")
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Headers"), "Content-Type")
	assert.Equal(t, "600", resp.Header.Get("Access-Control-Max-Age"))
	assert.Equal(t, "Origin", resp.Header.Get("Vary"))
}

// TestE2E_CORS_PreflightUnknownOriginReturns404 pins the reject
// branch: a preflight with an Origin that is NOT in the allow-list
// short-circuits with 404. A 404 (rather than 403) mirrors the
// "we have nothing to say to this origin" posture and is
// indistinguishable from an unknown route — by design, so origin
// probing cannot enumerate the route surface.
func TestE2E_CORS_PreflightUnknownOriginReturns404(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodOptions, gatewayURL+"/cors/public", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

// TestE2E_CORS_PreflightMissingACRMReturns404 pins the malformed
// branch: a real CORS preflight always carries the
// Access-Control-Request-Method header (browsers emit it
// unconditionally). Without it, the gateway cannot look up the
// downstream route and returns 404 — rejecting raw OPTIONS probes
// with no CORS intent.
func TestE2E_CORS_PreflightMissingACRMReturns404(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodOptions, gatewayURL+"/cors/public", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", corsOrigin)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestE2E_CORS_ActualRequestStampsCORSHeaders pins the
// actual-request branch: a regular GET on a CORS-enabled route
// with a matching Origin carries the CORS response subset
// (origin echo, Vary) on a 200 body. Preflight-only attributes
// (methods, headers, max-age) MUST NOT appear here — they belong
// only on OPTIONS responses.
func TestE2E_CORS_ActualRequestStampsCORSHeaders(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodGet, gatewayURL+"/cors/public", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", corsOrigin)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, corsOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", resp.Header.Get("Vary"))
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Methods"),
		"preflight-only header must not leak onto actual responses")
	assert.Empty(t, resp.Header.Get("Access-Control-Max-Age"))
}

// TestE2E_CORS_CredentialsRouteEchoesAllowCredentials pins the
// credentials branch: a preflight for a CORS route with
// `credentials: true` emits `Access-Control-Allow-Credentials:
// true`. Without this header, browsers silently drop cookies on
// cross-origin requests — a regression here breaks session auth
// in the browser with no visible server-side symptom.
func TestE2E_CORS_CredentialsRouteEchoesAllowCredentials(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodOptions, gatewayURL+"/cors/creds", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", corsOrigin)
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "true", resp.Header.Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, corsOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
}

// TestE2E_RateLimit_BurstThenReject pins the token-bucket
// rejection branch. The `/rate-limit/basic` route is configured
// with `rps: 1, burst: 1` so the second back-to-back request
// MUST be rejected with 429. The gateway emits `Retry-After: 1`
// so well-behaved clients know when to retry.
//
// Because the memory store keys buckets by method+path+clientIP
// and test suites all hit the gateway from the same loopback IP,
// this test intentionally runs first among rate-limit scenarios
// and relies on a cold bucket. A follow-up run within the same
// second would re-observe the 429 — that is an accepted trade-off
// for deterministic assertion.
func TestE2E_RateLimit_BurstThenReject(t *testing.T) {
	waitForGateway(t)

	first, err := http.Get(gatewayURL + "/rate-limit/basic")
	require.NoError(t, err)
	_ = first.Body.Close()
	assert.Equal(t, http.StatusOK, first.StatusCode, "first request within burst must succeed")

	second, err := http.Get(gatewayURL + "/rate-limit/basic")
	require.NoError(t, err)
	defer func() { _ = second.Body.Close() }()

	assert.Equal(t, http.StatusTooManyRequests, second.StatusCode,
		"second request within the same second must be rate-limited")
	assert.Equal(t, "1", second.Header.Get("Retry-After"),
		"429 response must carry a Retry-After directive")
}

// TestE2E_RateLimit_KeyByHeaderIsolatesBuckets pins the keyBy
// chain isolation contract. Two distinct `X-API-Key` values on
// the same route MUST land in separate token buckets — one
// tenant's spike cannot exhaust another tenant's budget. Waits
// ~1.1s first so any lingering bucket state from a prior test
// run (the memory store is not cleared between tests) has a
// chance to refill.
func TestE2E_RateLimit_KeyByHeaderIsolatesBuckets(t *testing.T) {
	waitForGateway(t)

	// Let any neighbouring bucket refill so the first hit of
	// each key lands in a clean state. rps=1 means a 1-second
	// gap is enough; 1.1s gives a small jitter budget.
	time.Sleep(1100 * time.Millisecond)

	doReq := func(key string) *http.Response {
		req, err := http.NewRequest(http.MethodGet, gatewayURL+"/rate-limit/by-header", nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", key)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		return resp
	}

	aliceFirst := doReq("tenant-alice")
	_ = aliceFirst.Body.Close()
	assert.Equal(t, http.StatusOK, aliceFirst.StatusCode)

	bobFirst := doReq("tenant-bob")
	_ = bobFirst.Body.Close()
	assert.Equal(t, http.StatusOK, bobFirst.StatusCode,
		"a second tenant must not inherit the first tenant's bucket state")

	aliceSecond := doReq("tenant-alice")
	_ = aliceSecond.Body.Close()
	assert.Equal(t, http.StatusTooManyRequests, aliceSecond.StatusCode,
		"the original tenant's bucket must remain exhausted regardless of other tenants")
}

// TestE2E_Timeout_PerRouteDeadlineReturns504 pins the per-route
// timeout contract. The `/timeout/slow` route declares a 200ms
// budget; the handler sleeps 500ms, so the NATS request exceeds
// the deadline and the gateway surfaces 504 Gateway Timeout. The
// whole round trip MUST return well under the handler's 500ms
// sleep — a regression where the gateway waited for the full
// handler response would make this obvious via the elapsed time.
func TestE2E_Timeout_PerRouteDeadlineReturns504(t *testing.T) {
	waitForGateway(t)

	started := time.Now()
	resp, err := http.Get(gatewayURL + "/timeout/slow?delayMs=500")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	elapsed := time.Since(started)

	assert.Equal(t, http.StatusGatewayTimeout, resp.StatusCode)
	// The route timeout is 200ms; with NATS + scheduling overhead a
	// loose 450ms bound still catches a regression where the gateway
	// waits the full 500ms of handler sleep.
	assert.Less(t, elapsed, 450*time.Millisecond,
		"gateway must return 504 on the per-route deadline, not wait for the handler")
}

// TestE2E_Headers_RouteOverrideAndForRootDefaultBothFlow pins the
// deep-merge contract on response headers. The `/headers/route`
// handler declares a per-route `x-custom: route-value`; the
// module-level `forRoot({ defaults: { headers: { 'x-frame-options':
// 'DENY' } } })` contributes a global header. Both MUST appear on
// the response — per-route additions and module-level defaults
// compose via per-key deep merge.
func TestE2E_Headers_RouteOverrideAndForRootDefaultBothFlow(t *testing.T) {
	waitForGateway(t)

	resp, err := http.Get(gatewayURL + "/headers/route")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "route-value", resp.Header.Get("X-Custom"),
		"per-route static header must land on the wire")
	assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"),
		"forRoot header default must survive per-route merge")
}

// TestE2E_Headers_ForRootDefaultFlowsToUndecoratedRoutes pins the
// global reach of module-level header defaults: any route that
// does NOT declare its own `headers` still inherits the forRoot
// defaults. The demo `/bench/hello` route declares nothing, so
// `x-frame-options: DENY` MUST still appear.
func TestE2E_Headers_ForRootDefaultFlowsToUndecoratedRoutes(t *testing.T) {
	waitForGateway(t)

	resp, err := http.Get(gatewayURL + "/bench/hello")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"),
		"forRoot header default must apply to routes with no per-route override")
}

// TestE2E_CookieDefaults_AreMergedIntoSetCookie pins the cookie
// defaults merge contract. The `/cookies/set` handler calls
// `res.cookie('sid', 'contract-probe')` with no options — the SDK
// cookie serializer MUST merge the `forRoot({ defaults: { cookies }
// })` block into the emitted `Set-Cookie` header, so the wire
// cookie carries `HttpOnly`, `SameSite=Lax`, `Path=/`, and
// `Max-Age=7200` even though the handler never declared them.
func TestE2E_CookieDefaults_AreMergedIntoSetCookie(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/cookies/set", strings.NewReader("{}"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "drain body so the log output isn't truncated on failure")
	_ = body

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var sid *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "sid" {
			sid = c

			break
		}
	}

	require.NotNil(t, sid, "cookie set via bare res.cookie must still reach the wire")
	assert.Equal(t, "contract-probe", sid.Value)
	assert.True(t, sid.HttpOnly, "forRoot cookie default HttpOnly must merge in")
	assert.Equal(t, http.SameSiteLaxMode, sid.SameSite, "forRoot cookie default SameSite=lax must merge in")
	assert.Equal(t, "/", sid.Path, "forRoot cookie default Path=/ must merge in")
	assert.Equal(t, 7200, sid.MaxAge, "forRoot cookie default MaxAge must merge in")
	assert.False(t, sid.Secure, "forRoot default Secure=false must apply for local dev")
}

// TestE2E_RateLimit_KeyByIpIsolatesBuckets pins the honest-operator
// per-IP isolation path under the default TRUSTED_PROXIES=private
// profile. The `/rate-limit/basic` route has `rateLimit: { rps: 1,
// burst: 1 }` and no explicit keyBy, so §13 falls back to ['ip'].
// With the private sentinel in effect, loopback peer (127.0.0.1) ∈
// private → XFF is honoured, so three DIFFERENT X-Forwarded-For
// values resolve to three DIFFERENT client IPs, each landing in
// its own bucket.
//
// This is the mirror of
// TestE2E_TrustedProxyEmpty_SpoofingBypassBlocked: same endpoint,
// same three XFF values, opposite profile. The empty-trust profile
// ignores XFF and all three bucket to peer; the default profile
// honours XFF and all three get independent buckets. Together they
// pin the full trust-resolution contract for §13 per-IP RL.
//
// The request targets the gateway via 127.0.0.1 (not localhost) to
// guarantee the peer observed by the gateway is IPv4 loopback and
// therefore trusted by the default profile's `private` sentinel —
// on dual-stack hosts `localhost` resolves to ::1, which is also
// private but makes the test host-dependent.
func TestE2E_RateLimit_KeyByIpIsolatesBuckets(t *testing.T) {
	waitForGateway(t)

	// Let any lingering bucket from a previous run refill.
	time.Sleep(1100 * time.Millisecond)

	issue := func(xff string) int {
		req, err := http.NewRequest(
			http.MethodGet,
			"http://127.0.0.1:8080/rate-limit/basic",
			nil,
		)
		require.NoError(t, err)
		req.Header.Set("X-Forwarded-For", xff)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		return resp.StatusCode
	}

	assert.Equal(t, http.StatusOK, issue("1.1.1.1"),
		"tenant A (1.1.1.1) first request passes")
	assert.Equal(t, http.StatusOK, issue("2.2.2.2"),
		"tenant B (2.2.2.2) first request passes — separate bucket from A")
	assert.Equal(t, http.StatusOK, issue("3.3.3.3"),
		"tenant C (3.3.3.3) first request passes — separate bucket from A and B")
}
