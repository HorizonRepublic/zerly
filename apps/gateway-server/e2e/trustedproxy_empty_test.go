//go:build e2e_empty_trust

// Package e2e — trusted-proxy empty-trust profile scenarios.
//
// This file uses a DIFFERENT build tag (`e2e_empty_trust`) so the
// nx runner can pick it up exclusively when the gateway has been
// restarted with TRUSTED_PROXIES="". Running this file against a
// default-profile gateway would falsely fail because XFF would be
// honoured.
//
// The file is the security proof for the H.3 hardening bundle:
// scenario 2 demonstrates that spoofing-bypass of the §13 per-IP
// rate limit is no longer possible when the operator has declared
// no trusted proxies.
package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trustedProxyEmptyGatewayURL hits the gateway via the IPv4 loopback
// explicitly so the peer IP observed by the gateway is
// deterministically 127.0.0.1. A dual-stack localhost would resolve
// to ::1 on some hosts and make peer-IP assertions non-portable.
const trustedProxyEmptyGatewayURL = "http://127.0.0.1:8080"

// trustedProxyEmptyReadyTimeout bounds how long we poll for the
// gateway's readiness on the IPv4 loopback path. The shared
// waitForGateway helper dials the package-level gatewayURL
// (localhost), so this file rolls its own readiness probe against
// 127.0.0.1 to guarantee consistency with the test URL.
const trustedProxyEmptyReadyTimeout = 10 * time.Second

// waitForEmptyTrustGateway polls the gateway until it responds on
// the IPv4 loopback. Any response (including 404) counts as ready.
// The shared waitForGateway uses `localhost`; this file pins IPv4
// to match the assertions below.
func waitForEmptyTrustGateway(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(trustedProxyEmptyReadyTimeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for {
		req, err := http.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			trustedProxyEmptyGatewayURL+"/__probe__",
			nil,
		)
		require.NoError(t, err)

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("empty-trust gateway not reachable within %s: %v",
				trustedProxyEmptyReadyTimeout, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// whoamiResponseEmpty mirrors the /whoami endpoint body shape.
// Duplicated from trustedproxy_test.go because that file is
// compiled under a different build tag (plain `e2e`) and the two
// files never appear in the same build.
type whoamiResponseEmpty struct {
	IP string `json:"ip"`
}

// TestE2E_TrustedProxyEmpty_XFFIgnored_ReturnsPeer pins the
// front-door invariant: with TRUSTED_PROXIES="", loopback peer is
// NOT trusted, XFF is ignored, the returned IP is the peer.
func TestE2E_TrustedProxyEmpty_XFFIgnored_ReturnsPeer(t *testing.T) {
	waitForEmptyTrustGateway(t)

	req, err := http.NewRequest(
		http.MethodGet,
		trustedProxyEmptyGatewayURL+"/whoami",
		nil,
	)
	require.NoError(t, err)
	req.Header.Set("X-Forwarded-For", "1.1.1.1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body whoamiResponseEmpty
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "127.0.0.1", body.IP,
		"empty trusted list → peer is never trusted → XFF ignored")
}

// TestE2E_TrustedProxyEmpty_SpoofingBypassBlocked is the security
// proof for this whole bundle. The test sends two requests against
// /rate-limit/basic (`rps: 1, burst: 1, keyBy: ['ip']` in
// gateway-contract-demo.controller.ts; keyBy defaults to ['ip']
// through §13's implicit fallback) with DIFFERENT X-Forwarded-For
// values. Before H.3 these would have landed in two separate
// buckets (one per XFF value) and both would succeed; after H.3
// with TRUSTED_PROXIES="" both bucket on the peer IP (127.0.0.1)
// and the second request is rejected with 429.
func TestE2E_TrustedProxyEmpty_SpoofingBypassBlocked(t *testing.T) {
	waitForEmptyTrustGateway(t)

	// Let any lingering bucket from a previous run refill. rps=1
	// means a 1-second gap is enough; 1.1s gives jitter headroom.
	time.Sleep(1100 * time.Millisecond)

	issue := func(xff string) int {
		req, err := http.NewRequest(
			http.MethodGet,
			trustedProxyEmptyGatewayURL+"/rate-limit/basic",
			nil,
		)
		require.NoError(t, err)
		req.Header.Set("X-Forwarded-For", xff)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()

		return resp.StatusCode
	}

	first := issue("1.1.1.1")
	assert.Equal(t, http.StatusOK, first,
		"first request within burst must succeed")

	second := issue("2.2.2.2")
	assert.Equal(t, http.StatusTooManyRequests, second,
		"different XFF values MUST NOT bypass per-IP RL — both bucket to peer 127.0.0.1")
}
