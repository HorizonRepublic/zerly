//go:build e2e

// Package e2e — trusted-proxy resolution scenarios on the default
// gateway profile (TRUSTED_PROXIES=private). Sibling of e2e_test.go
// and contract_test.go; reuses `waitForGateway`.
//
// Companion file `trustedproxy_empty_test.go` covers the
// empty-trust profile and pins the security proof. This file
// covers the honest-operator happy path: peer is loopback ∈ private
// → XFF is honoured → resolver returns the declared client IP.
package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trustedProxyGatewayURL hits the gateway via the IPv4 loopback
// explicitly (rather than `localhost`) so the peer IP observed by
// the gateway is deterministically 127.0.0.1 regardless of the
// host's resolver preference order. The shared gatewayURL const
// uses `localhost`, which on dual-stack hosts resolves to ::1 and
// would make assertions on the peer IP non-portable.
const trustedProxyGatewayURL = "http://127.0.0.1:8080"

// whoamiResponse mirrors the /whoami endpoint body shape in
// apps/example-app/src/app/gateway-demo/gateway-contract-demo.controller.ts.
type whoamiResponse struct {
	IP string `json:"ip"`
}

// callWhoami issues a GET /whoami with optional X-Forwarded-For and
// returns the decoded body. Tests that only care about the resolved
// IP stay terse.
func callWhoami(t *testing.T, xff string) whoamiResponse {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, trustedProxyGatewayURL+"/whoami", nil)
	require.NoError(t, err)
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body whoamiResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	return body
}

// TestE2E_TrustedProxy_DirectRequest_NoXFF_ReturnsLoopbackPeer pins
// the no-XFF baseline: a direct request with no X-Forwarded-For
// resolves to the TCP peer, which for an IPv4 loopback run is
// 127.0.0.1.
func TestE2E_TrustedProxy_DirectRequest_NoXFF_ReturnsLoopbackPeer(t *testing.T) {
	waitForGateway(t)

	got := callWhoami(t, "")
	assert.Equal(t, "127.0.0.1", got.IP,
		"no XFF → peer IP is the loopback the test dialed from")
}

// TestE2E_TrustedProxy_XFFSingleClient_TrustedPeer_ReturnsClientIP
// pins the common L7-forwarded case: a single-IP XFF from a
// loopback (∈ private) peer means the ingress controller vouches
// for that IP.
func TestE2E_TrustedProxy_XFFSingleClient_TrustedPeer_ReturnsClientIP(t *testing.T) {
	waitForGateway(t)

	got := callWhoami(t, "1.2.3.4")
	assert.Equal(t, "1.2.3.4", got.IP,
		"trusted loopback peer → XFF '1.2.3.4' honoured verbatim")
}

// TestE2E_TrustedProxy_XFFChain_SkipsTrusted_ReturnsRightmostUntrusted
// pins the rightmost-untrusted walk: the test sends a chain whose
// rightmost entry is a private IP (trusted) and whose leftmost
// entry is a public IP. The resolver must skip the trusted entry
// and return the public one.
func TestE2E_TrustedProxy_XFFChain_SkipsTrusted_ReturnsRightmostUntrusted(t *testing.T) {
	waitForGateway(t)

	got := callWhoami(t, "9.9.9.9, 10.0.0.1")
	assert.Equal(t, "9.9.9.9", got.IP,
		"10.0.0.1 is trusted (skipped); 9.9.9.9 is the rightmost untrusted IP")
}

// TestE2E_TrustedProxy_XFFAllTrusted_FallsBackToPeer pins the
// all-private chain: no untrusted IP means the chain exhausts and
// the peer IP is used.
func TestE2E_TrustedProxy_XFFAllTrusted_FallsBackToPeer(t *testing.T) {
	waitForGateway(t)

	got := callWhoami(t, "10.0.0.5, 172.16.0.1")
	assert.Equal(t, "127.0.0.1", got.IP,
		"all-trusted chain exhausts → resolver falls back to peer (127.0.0.1)")
}

// TestE2E_TrustedProxy_IPv6Chain_Resolves pins the IPv6 symmetrical
// path: a mixed IPv6 chain with loopback fd00::1 (∈ private) walks
// past the trusted entry and returns the public IPv6 client. Peer
// here is still IPv4 127.0.0.1 (IPv4 loopback base URL), so the
// test also proves that an IPv4 peer can vouch for an IPv6 XFF.
func TestE2E_TrustedProxy_IPv6Chain_Resolves(t *testing.T) {
	waitForGateway(t)

	got := callWhoami(t, "2001:db8::1, fd00::1")
	assert.Equal(t, "2001:db8::1", got.IP,
		"fd00::1 ∈ private → skipped; 2001:db8::1 is the returned client")
}
