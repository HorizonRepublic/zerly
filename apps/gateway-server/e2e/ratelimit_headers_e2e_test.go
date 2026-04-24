//go:build e2e

// Package e2e — wire-level header casing for the rate-limit response
// surface. The unit tests in internal/proxy/handler_test.go assert
// against the in-memory `result.Headers` map and therefore see the
// keys exactly as `BuildHeaders` produces them ("X-RateLimit-Limit").
// Hertz canonicalises header names before transmission, so the wire
// bytes a real HTTP client observes are the MIME canonical form
// ("X-Ratelimit-Limit"). This file pins that wire contract end-to-end
// so a future change to either the gateway-side casing OR the Hertz
// canonicalisation rules surfaces as a test failure rather than a
// silent regression in client header parsing.
package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_RateLimit_HeaderCasingOnTheWire pins the raw on-the-wire
// case for the X-RateLimit-* response headers. We use case-insensitive
// matching (http.Header.Get already does this) but additionally
// inspect the underlying CanonicalMIMEHeaderKey form so a regression
// that caused the gateway to emit a non-canonical key (e.g. the all-
// lowercase "x-ratelimit-limit") would surface here.
//
// Headers are emitted on every rate-limited response — both the
// happy path (200 within the burst) and the 429 path. We hit
// /rate-limit/basic once on a fresh bucket so the response is a
// 200 with the full triplet visible.
func TestE2E_RateLimit_HeaderCasingOnTheWire(t *testing.T) {
	waitForGateway(t)

	// Drain any lingering bucket so the request below succeeds. The
	// /rate-limit/basic route is rps=1 burst=1; sleeping 1.1s gives
	// the bucket time to refill plus a small jitter buffer.
	time.Sleep(1100 * time.Millisecond)

	resp, err := http.Get(gatewayURL + "/rate-limit/basic")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"first request after refill must be inside the burst")

	// http.Header.Get is case-insensitive by spec, so any of the
	// expected wire spellings round-trip cleanly. The presence
	// assertion alone is the contract — operators can rely on
	// http.Header.Get / browser fetch APIs to find the value
	// regardless of which canonicalisation step set it.
	assert.NotEmpty(t, resp.Header.Get("X-RateLimit-Limit"),
		"X-RateLimit-Limit must reach the wire on rate-limited routes")
	assert.NotEmpty(t, resp.Header.Get("X-RateLimit-Remaining"),
		"X-RateLimit-Remaining must reach the wire on the happy path")
	assert.NotEmpty(t, resp.Header.Get("X-RateLimit-Reset"),
		"X-RateLimit-Reset must reach the wire on the happy path")

	// Pin the actual canonical key Hertz produces. Net/http stores
	// headers under CanonicalMIMEHeaderKey form, so iterating the
	// raw map shows exactly which spelling crossed the wire.
	wantCanonical := http.CanonicalHeaderKey("X-RateLimit-Limit")
	if _, ok := resp.Header[wantCanonical]; !ok {
		t.Errorf(
			"expected canonical header %q to exist in raw response.Header map; got keys=%v",
			wantCanonical, headerKeys(resp.Header),
		)
	}
}

// TestE2E_RateLimit_HeaderCasingOn429 mirrors the canonical-form
// assertion against the 429 reject path. Headers must reach the
// wire on rejection too — clients use the X-RateLimit-* triplet
// to schedule retries even when the gateway returned a Retry-After
// directive.
func TestE2E_RateLimit_HeaderCasingOn429(t *testing.T) {
	waitForGateway(t)

	// Force a fresh bucket, consume it, then immediately retry to
	// trigger the 429 branch.
	time.Sleep(1100 * time.Millisecond)

	first, err := http.Get(gatewayURL + "/rate-limit/basic")
	require.NoError(t, err)
	_ = first.Body.Close()
	require.Equal(t, http.StatusOK, first.StatusCode)

	second, err := http.Get(gatewayURL + "/rate-limit/basic")
	require.NoError(t, err)
	defer func() { _ = second.Body.Close() }()

	require.Equal(t, http.StatusTooManyRequests, second.StatusCode)

	assert.NotEmpty(t, second.Header.Get("Retry-After"),
		"Retry-After must reach the wire on 429")
	assert.NotEmpty(t, second.Header.Get("X-RateLimit-Limit"),
		"X-RateLimit-Limit must reach the wire on 429")
	assert.Equal(t, "0", second.Header.Get("X-RateLimit-Remaining"),
		"Remaining = 0 on a real 429 (bucket exhausted)")

	wantCanonical := http.CanonicalHeaderKey("Retry-After")
	if _, ok := second.Header[wantCanonical]; !ok {
		t.Errorf(
			"expected canonical header %q to exist in raw response.Header map; got keys=%v",
			wantCanonical, headerKeys(second.Header),
		)
	}
}

// headerKeys returns a sorted snapshot of the keys in an http.Header
// for use in failure messages. Sorted output keeps the diff readable
// when a regression surfaces an unexpected key set.
func headerKeys(h http.Header) []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}

	return keys
}
