//go:build e2e

// Package e2e — KV-bypass sanitization scenarios. Pins the
// security contract of the gateway's routing-builder layer: a
// malicious or malformed KV entry written outside the SDK MUST
// NOT make it onto the wire. The SDK's typia validator already
// rejects these shapes at module init, so every entry tested
// here is a hand-crafted operator-bypass payload — exactly the
// threat model the routing builder's sanitizers (sanitizeHeaders,
// sanitizeCORS, sanitizeRateLimit) defend against.
//
// Sibling of reload_test.go (synthetic-handler/KV-Put pattern
// reused here) and ratelimit_cookie_collision_e2e_test.go
// (KV-Put + waitForRoute* polling reused here). The route is
// registered via direct KV Put because example-app does not host
// any deliberately-malformed routes — the demo surface validates
// inputs before they reach KV. Building local synthetic routes
// keeps the production code unchanged while exercising the
// sanitizer branches end-to-end.
//
// Scenarios:
//
//   - CRLF / NUL byte in route.headers map values is dropped per
//     entry; the surrounding response otherwise serves normally
//     (no Set-Cookie injection, no X-Foo header on the wire).
//   - CRLF / NUL byte anywhere in cors.origins / cors.methods /
//     cors.headers / cors.exposeHeaders fail-closes the entire
//     CORS block: no Access-Control-Allow-Origin echo, OPTIONS
//     preflight returns 404 (route exists but CORS is gone).
//   - rateLimit.rps <= 0 or rateLimit.burst < 0 drops the entire
//     rate-limit block: a burst of requests serves 200 throughout
//     because no limiter is ever attached to the route.
//   - Sanity: a clean headers map propagates intact, proving the
//     sanitizer is not over-zealous.
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kvCORSMeta mirrors registry.CORSMeta for the KV-Put path used by
// these tests. Declared locally so the e2e package stays decoupled
// from registry internals — the wire contract is the JSON shape,
// not the Go struct identity.
type kvCORSMeta struct {
	Origins       []string `json:"origins"`
	Methods       []string `json:"methods,omitempty"`
	Headers       []string `json:"headers,omitempty"`
	Credentials   bool     `json:"credentials,omitempty"`
	MaxAge        int      `json:"maxAge,omitempty"`
	ExposeHeaders []string `json:"exposeHeaders,omitempty"`
}

// kvSanitizationEntry is the on-the-wire JSON shape for the
// synthetic KV entries these tests write. Mirrors the subset of
// registry.HandlerEntry fields exercised by the sanitizer paths
// — http (route shape), headers (per-entry CRLF check), cors
// (block-level CRLF check), rateLimit (rps/burst sanity).
type kvSanitizationEntry struct {
	HTTP      *kvHTTPMeta       `json:"http,omitempty"`
	CORS      *kvCORSMeta       `json:"cors,omitempty"`
	RateLimit *kvRateLimitMeta  `json:"rateLimit,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// putSanitizationEntry serializes entry, writes it to KV under
// key, registers a t.Cleanup that deletes the key (so subsequent
// runs of the same test are idempotent against the shared bucket),
// and waits for the watcher to make path return `wantStatus` on a
// plain GET. The wait step proves the entry was ingested before
// the test issues its assertion request.
func putSanitizationEntry(t *testing.T, fx *natsFixture, key string, entry *kvSanitizationEntry, path string, wantStatus int) {
	t.Helper()

	t.Cleanup(func() { _ = fx.kv.Delete(fx.ctx, key) })

	bytes, err := json.Marshal(entry)
	require.NoError(t, err, "marshal synthetic KV entry")

	_, err = fx.kv.Put(fx.ctx, key, bytes)
	require.NoError(t, err, "put synthetic KV entry")

	waitForRouteStatus(t, path, wantStatus)
}

// TestE2E_KVSanitize_HeaderCRLFInValueDropped pins the per-entry
// CRLF drop on route.headers. The malicious value would split the
// response header line into two on the wire, smuggling a
// Set-Cookie that hijacks the session. Sanitizer drops the entry;
// the route otherwise serves normally.
func TestE2E_KVSanitize_HeaderCRLFInValueDropped(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-kvsan-hdrcrlf"
		pattern = "kvsan.hdrcrlf.probe"
		path    = "/kvsan/hdr-crlf"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	serveFakeHandler(t, fx.nc, subject, map[string]any{"ok": true})

	entry := &kvSanitizationEntry{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		Headers: map[string]string{
			"X-Foo": "value\r\nSet-Cookie: stolen=1",
		},
	}

	putSanitizationEntry(t, fx, key, entry, path, http.StatusOK)

	resp, err := http.Get(gatewayURL + path)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"route still serves; only the malformed header entry is dropped")

	// The malicious smuggled Set-Cookie MUST NOT reach the wire.
	// Either as a folded second line on X-Foo (which Go's HTTP
	// server already rejects at write time) or as a synthesized
	// Set-Cookie header on the response.
	assert.Empty(t, resp.Header.Values("Set-Cookie"),
		"CRLF-smuggled Set-Cookie must not reach the wire")

	// The X-Foo header itself MUST be absent — the entire entry
	// (name+value) was dropped by the sanitizer, not partially
	// stripped.
	assert.Empty(t, resp.Header.Get("X-Foo"),
		"sanitized header entry must be dropped, not emitted with the cleaned value")
}

// TestE2E_KVSanitize_HeaderNULDropped pins the same per-entry drop
// for a NUL byte. Some HTTP intermediaries fold NUL into a line
// break, and trailing-NUL truncation in C-string consumers makes
// any embedded NUL a header-injection primitive — even if Go's
// own server already rejects the value, the sanitizer is the
// boundary that keeps malformed values out of the routing table
// entirely.
func TestE2E_KVSanitize_HeaderNULDropped(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-kvsan-hdrnul"
		pattern = "kvsan.hdrnul.probe"
		path    = "/kvsan/hdr-nul"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	serveFakeHandler(t, fx.nc, subject, map[string]any{"ok": true})

	entry := &kvSanitizationEntry{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		Headers: map[string]string{
			"X-Bar": "value\x00truncated",
		},
	}

	putSanitizationEntry(t, fx, key, entry, path, http.StatusOK)

	resp, err := http.Get(gatewayURL + path)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("X-Bar"),
		"NUL-bearing header entry must be dropped")
}

// TestE2E_KVSanitize_CORSOriginCRLFDropsEntireCORS pins the
// fail-closed CORS contract: any CRLF in cors.origins drops the
// ENTIRE CORS block (not just the offending origin), so partial
// CORS — strictly worse than no CORS, since it surfaces the
// contract while keeping the malformed string as an injection
// primitive — never reaches the wire.
//
// Two assertions:
//
//   - An actual GET with Origin: evil.com receives no
//     Access-Control-Allow-Origin echo (CORS dropped, route
//     serves bare).
//   - An OPTIONS preflight on the same path returns 404 — with
//     no CORS block on the route, the gateway has nothing to say
//     to the preflight, mirroring the unknown-origin posture.
func TestE2E_KVSanitize_CORSOriginCRLFDropsEntireCORS(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-kvsan-corscrlf"
		pattern = "kvsan.corscrlf.probe"
		path    = "/kvsan/cors-crlf"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	serveFakeHandler(t, fx.nc, subject, map[string]any{"ok": true})

	entry := &kvSanitizationEntry{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		CORS: &kvCORSMeta{
			Origins: []string{"evil.com\r\nSet-Cookie: x=y"},
			Methods: []string{http.MethodGet},
		},
	}

	putSanitizationEntry(t, fx, key, entry, path, http.StatusOK)

	// Actual request with the matching Origin string. With the
	// CORS block dropped the route still serves, but no
	// Access-Control-Allow-Origin reaches the wire.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, gatewayURL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "evil.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"),
		"CRLF in cors.origins MUST drop the entire CORS block")
	assert.Empty(t, resp.Header.Values("Set-Cookie"),
		"CRLF-smuggled Set-Cookie must not reach the wire via CORS")

	// Preflight check: with the CORS block dropped, OPTIONS has
	// nothing to negotiate, so the gateway returns 404.
	preflight, err := http.NewRequestWithContext(context.Background(), http.MethodOptions, gatewayURL+path, nil)
	require.NoError(t, err)
	preflight.Header.Set("Origin", "evil.com")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodGet)

	preResp, err := http.DefaultClient.Do(preflight)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, preResp.Body)
	_ = preResp.Body.Close()

	assert.Equal(t, http.StatusNotFound, preResp.StatusCode,
		"OPTIONS on a route whose CORS block was sanitized away MUST return 404")
	assert.Empty(t, preResp.Header.Get("Access-Control-Allow-Origin"),
		"preflight reject must not echo any CORS headers")
}

// TestE2E_KVSanitize_CORSExposeHeadersCRLFDropsEntireCORS pins
// that the fail-closed CORS contract covers ExposeHeaders, not
// only Origins. ExposeHeaders is the per-route override for
// Access-Control-Expose-Headers — a CRLF here would inject
// arbitrary response headers on every CORS request without ever
// going near the cors.origins string.
func TestE2E_KVSanitize_CORSExposeHeadersCRLFDropsEntireCORS(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-kvsan-corsexp"
		pattern = "kvsan.corsexp.probe"
		path    = "/kvsan/cors-exp"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	serveFakeHandler(t, fx.nc, subject, map[string]any{"ok": true})

	entry := &kvSanitizationEntry{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		CORS: &kvCORSMeta{
			Origins:       []string{"https://app.example.test"},
			Methods:       []string{http.MethodGet},
			ExposeHeaders: []string{"X-Trace-Id\r\nX-Injected: 1"},
		},
	}

	putSanitizationEntry(t, fx, key, entry, path, http.StatusOK)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, gatewayURL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://app.example.test")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"),
		"CRLF in cors.exposeHeaders MUST drop the entire CORS block, not just the malformed field")
	assert.Empty(t, resp.Header.Get("Access-Control-Expose-Headers"),
		"sanitized exposeHeaders must not reach the wire")
	assert.Empty(t, resp.Header.Get("X-Injected"),
		"CRLF-smuggled X-Injected must not reach the wire")
}

// rateLimitBurstSize bounds how many requests each rate-limit
// sanitization scenario fires at the synthetic route. 100 is
// enough to overshoot any plausible rps:burst combination by an
// order of magnitude — if the limiter were attached, at least
// one rejection would land in this window. With the malformed
// block sanitized away no limiter exists, so every request
// resolves to the fake handler's 200.
const rateLimitBurstSize = 100

// burstUnauthenticated fires count sequential GETs against url
// and returns the per-status counts. Used by the rate-limit
// sanitization scenarios to assert that no 429 ever lands when
// the malformed rate-limit block was dropped.
func burstUnauthenticated(t *testing.T, url string, count int) map[int]int {
	t.Helper()

	statuses := make(map[int]int, 4)
	client := &http.Client{Timeout: 5 * time.Second}

	for i := 0; i < count; i++ {
		resp, err := client.Get(url)
		require.NoError(t, err, "request %d", i)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		statuses[resp.StatusCode]++
	}

	return statuses
}

// TestE2E_KVSanitize_RateLimitRPSZeroDropsLimiter pins that a
// rate-limit block with rps:0 is dropped wholesale at routing
// build time, so no limiter is ever attached to the route. A
// burst of 100 requests resolves entirely to 200; the
// handler-level fail-safe ("rps <= 0 → skip limiter") is now
// surfaced by an explicit drop + WARN log instead of a silent
// pass-through.
func TestE2E_KVSanitize_RateLimitRPSZeroDropsLimiter(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-kvsan-rlzero"
		pattern = "kvsan.rlzero.probe"
		path    = "/kvsan/rl-zero"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	serveFakeHandler(t, fx.nc, subject, map[string]any{"ok": true})

	entry := &kvSanitizationEntry{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		RateLimit: &kvRateLimitMeta{
			RPS:   0,
			Burst: 10,
		},
	}

	putSanitizationEntry(t, fx, key, entry, path, http.StatusOK)

	statuses := burstUnauthenticated(t, gatewayURL+path, rateLimitBurstSize)
	t.Logf("rps:0 burst: statuses=%v", statuses)

	assert.Equal(t, rateLimitBurstSize, statuses[http.StatusOK],
		"rps:0 sanitized to nil — every request in the burst must succeed; statuses=%v", statuses)
	assert.Zero(t, statuses[http.StatusTooManyRequests],
		"sanitized-away limiter must NEVER produce 429; statuses=%v", statuses)
}

// TestE2E_KVSanitize_RateLimitNegativeBurstDropsLimiter pins the
// same drop on negative burst. GCRA.Check on a negative burst
// would either deny-all or allow-all depending on currentTAT —
// either outcome is a footgun. The sanitizer drops the whole
// block, the route serves without rate-limiting, and the
// operator sees a WARN at next reload instead of unexplained
// behaviour at runtime.
func TestE2E_KVSanitize_RateLimitNegativeBurstDropsLimiter(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-kvsan-rlneg"
		pattern = "kvsan.rlneg.probe"
		path    = "/kvsan/rl-neg"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	serveFakeHandler(t, fx.nc, subject, map[string]any{"ok": true})

	entry := &kvSanitizationEntry{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		RateLimit: &kvRateLimitMeta{
			RPS:   10,
			Burst: -5,
		},
	}

	putSanitizationEntry(t, fx, key, entry, path, http.StatusOK)

	statuses := burstUnauthenticated(t, gatewayURL+path, rateLimitBurstSize)
	t.Logf("burst:-5 burst: statuses=%v", statuses)

	assert.Equal(t, rateLimitBurstSize, statuses[http.StatusOK],
		"negative burst sanitized to nil — every request in the burst must succeed; statuses=%v", statuses)
	assert.Zero(t, statuses[http.StatusTooManyRequests],
		"sanitized-away limiter must NEVER produce 429; statuses=%v", statuses)
}

// TestE2E_KVSanitize_HeaderCleanValuesSurvive is the
// over-zealousness sanity test: a clean Headers map (no CRLF, no
// NUL) MUST propagate to the response intact. Without this
// assertion the suite could pass with a sanitizer that drops
// every entry unconditionally.
func TestE2E_KVSanitize_HeaderCleanValuesSurvive(t *testing.T) {
	waitForGateway(t)

	fx := newNATSFixture(t)

	const (
		service = "e2e-kvsan-clean"
		pattern = "kvsan.clean.probe"
		path    = "/kvsan/clean"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	serveFakeHandler(t, fx.nc, subject, map[string]any{"ok": true})

	entry := &kvSanitizationEntry{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		Headers: map[string]string{
			"X-Frame-Options": "DENY",
		},
	}

	putSanitizationEntry(t, fx, key, entry, path, http.StatusOK)

	resp, err := http.Get(gatewayURL + path)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"),
		"clean header value MUST propagate to the response unchanged")
}
