//go:build e2e

// Package e2e — trusted-proxy resolution scenarios for the
// non-XFF single-value header variants:
//   - X-Real-IP        (nginx-ingress, ALB, classic L7 reverse proxies)
//   - CF-Connecting-IP (Cloudflare)
//   - True-Client-IP   (Akamai, Cloudflare Enterprise)
//
// Sibling of trustedproxy_test.go (XFF chain walk on the default
// profile) and trustedproxy_empty_test.go (empty-trust security
// proof). This file spawns a SECONDARY gateway per scenario on a
// dedicated port with TRUSTED_PROXY_HEADER overridden, so the
// scenarios can exercise each header variant without disturbing
// the primary :8080 gateway used by the rest of the e2e suite.
//
// Trust profile rationale:
//   - Honored-path scenarios use TRUSTED_PROXIES=private so the
//     loopback peer (127.0.0.1) is in the trusted list and the
//     header is honoured verbatim. Mirrors trustedproxy_test.go.
//   - The spoofing-rejected scenario uses TRUSTED_PROXIES=10.0.0.0/8
//     so the loopback peer is NOT trusted and the resolver falls
//     back to the peer's TCP address — the single-value header
//     attached by an untrusted peer MUST be ignored.
//
// All single-value variants share the contract:
//   - peer ∈ trusted, header parses as IP → header value verbatim
//   - peer ∈ trusted, header empty / malformed → peer IP
//   - peer ∉ trusted → peer IP, header ignored (spoofing defence)
//
// These contracts are unit-tested in
// internal/trustedproxy/resolver_test.go but had no e2e coverage
// for the non-XFF variants; production deployments behind
// nginx-ingress, Cloudflare, or Akamai were therefore not pinned
// end-to-end against the running gateway binary.
package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// altHeaderReadyTimeout bounds how long the harness waits for a
// spawned alt-header secondary gateway to bind its port. 10s is
// generous for a prebuilt binary against an already-running NATS;
// a cold boot finishes well under 2s.
const altHeaderReadyTimeout = 10 * time.Second

// altHeaderRequestTimeout caps each /whoami request fired by the
// scenarios in this file. The handler hop is single-digit ms in
// practice; 2s leaves headroom for a loaded laptop.
const altHeaderRequestTimeout = 2 * time.Second

// startAltHeaderGateway spawns a secondary gateway process bound
// to httpAddr (e.g. ":8082") configured with the given trusted-
// proxy header and CIDR list. The process inherits the ambient
// env and then explicitly sets every knob the scenario depends
// on, so the test is deterministic regardless of what the
// developer has in `.env`.
//
// trustedProxies is passed verbatim to TRUSTED_PROXIES; the
// caller picks "private" for honoured-path scenarios and a CIDR
// that excludes loopback (e.g. "10.0.0.0/8") for the spoofing-
// rejected scenario.
//
// Cleanup is registered with t.Cleanup BEFORE the readiness
// probe. If waitForGatewayAt fails the test, the spawned process
// is still torn down so the next test run finds the port free.
//
// Returns the base URL (e.g. "http://127.0.0.1:8082") so the
// caller can compose request URLs without re-deriving the host.
// IPv4 loopback is hardcoded so the peer IP observed by the
// gateway is deterministically 127.0.0.1; a dual-stack
// `localhost` would resolve to ::1 on some hosts and break the
// peer-IP assertions.
func startAltHeaderGateway(t *testing.T, httpAddr, header, trustedProxies string) string {
	t.Helper()

	binary := gatewayBinaryPath(t)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(),
		"NATS_URLS=nats://localhost:4222",
		"HTTP_ADDR="+httpAddr,
		"KV_BUCKET=handler_registry",
		"LOG_FORMAT=console",
		"LOG_LEVEL=info",
		"ENVIRONMENT=development",
		"TRUSTED_PROXY_HEADER="+header,
		"TRUSTED_PROXIES="+trustedProxies,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	require.NoError(t, cmd.Start(), "failed to start alt-header gateway binary")

	var once sync.Once
	t.Cleanup(func() {
		once.Do(func() {
			cancel()
			_ = cmd.Wait()
		})
	})

	// "127.0.0.1" + httpAddr (which begins with ":") yields a clean
	// IPv4 base URL. Hardcoding the loopback host keeps peer-IP
	// assertions portable across dual-stack hosts.
	baseURL := "http://127.0.0.1" + httpAddr
	waitForGatewayAt(t, baseURL, altHeaderReadyTimeout)

	return baseURL
}

// callWhoamiWithHeader issues GET /whoami against baseURL,
// optionally setting headerName to headerValue, and returns the
// resolved client IP from the JSON body. Empty headerName means
// no header is sent — used by the empty-header fallback scenario.
func callWhoamiWithHeader(t *testing.T, baseURL, headerName, headerValue string) string {
	t.Helper()

	client := &http.Client{Timeout: altHeaderRequestTimeout}
	req, err := http.NewRequest(http.MethodGet, baseURL+"/whoami", nil)
	require.NoError(t, err)
	if headerName != "" {
		req.Header.Set(headerName, headerValue)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"alt-header /whoami must return 200 — non-200 indicates a transport or routing failure unrelated to trust resolution")

	var body whoamiResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	return body.IP
}

// TestE2E_TrustedProxy_XRealIPFromTrustedPeer_Honored pins the
// nginx-ingress / classic-L7 happy path: with TRUSTED_PROXIES=
// private, the loopback peer is trusted, and an X-Real-IP value
// attached by that peer is honoured verbatim.
func TestE2E_TrustedProxy_XRealIPFromTrustedPeer_Honored(t *testing.T) {
	baseURL := startAltHeaderGateway(t, ":8082", "X-Real-IP", "private")

	got := callWhoamiWithHeader(t, baseURL, "X-Real-IP", "203.0.113.50")
	assert.Equal(t, "203.0.113.50", got,
		"trusted loopback peer → X-Real-IP '203.0.113.50' honoured verbatim")
}

// TestE2E_TrustedProxy_XRealIPFromUntrustedPeer_Spoofing_Rejected
// is the security proof for the single-value resolver. With
// TRUSTED_PROXIES=10.0.0.0/8, the loopback peer (127.0.0.1) is
// NOT trusted; an X-Real-IP header attached by that peer is a
// spoofing attempt and MUST be ignored. The resolved IP is the
// peer's TCP address, not the spoofed value.
func TestE2E_TrustedProxy_XRealIPFromUntrustedPeer_Spoofing_Rejected(t *testing.T) {
	baseURL := startAltHeaderGateway(t, ":8083", "X-Real-IP", "10.0.0.0/8")

	got := callWhoamiWithHeader(t, baseURL, "X-Real-IP", "203.0.113.50")
	assert.Equal(t, "127.0.0.1", got,
		"untrusted loopback peer → X-Real-IP ignored, resolved IP is the peer's TCP address")
	assert.NotEqual(t, "203.0.113.50", got,
		"spoofed X-Real-IP from an untrusted peer MUST NOT leak through to the resolved client IP")
}

// TestE2E_TrustedProxy_CFConnectingIP_Honored pins the Cloudflare
// edge happy path: with the gateway behind Cloudflare, peer ∈
// trusted, CF-Connecting-IP from the edge is honoured verbatim.
func TestE2E_TrustedProxy_CFConnectingIP_Honored(t *testing.T) {
	baseURL := startAltHeaderGateway(t, ":8084", "CF-Connecting-IP", "private")

	got := callWhoamiWithHeader(t, baseURL, "CF-Connecting-IP", "198.51.100.42")
	assert.Equal(t, "198.51.100.42", got,
		"trusted loopback peer → CF-Connecting-IP '198.51.100.42' honoured verbatim")
}

// TestE2E_TrustedProxy_TrueClientIP_Honored pins the Akamai /
// Cloudflare-Enterprise edge happy path: with the gateway behind
// such an edge, peer ∈ trusted, True-Client-IP is honoured.
func TestE2E_TrustedProxy_TrueClientIP_Honored(t *testing.T) {
	baseURL := startAltHeaderGateway(t, ":8085", "True-Client-IP", "private")

	got := callWhoamiWithHeader(t, baseURL, "True-Client-IP", "192.0.2.77")
	assert.Equal(t, "192.0.2.77", got,
		"trusted loopback peer → True-Client-IP '192.0.2.77' honoured verbatim")
}

// TestE2E_TrustedProxy_AltHeader_FallsBackToPeerWhenHeaderEmpty
// pins the empty-header fallback contract: with a non-XFF header
// configured, a request that omits the header resolves to the
// peer IP. Picking X-Real-IP for this scenario is arbitrary —
// the contract is identical for every single-value variant.
func TestE2E_TrustedProxy_AltHeader_FallsBackToPeerWhenHeaderEmpty(t *testing.T) {
	baseURL := startAltHeaderGateway(t, ":8086", "X-Real-IP", "private")

	got := callWhoamiWithHeader(t, baseURL, "", "")
	assert.Equal(t, "127.0.0.1", got,
		"trusted peer + missing single-value header → resolver falls back to peer IP")
}

// TestE2E_TrustedProxy_AltHeader_DoesNotWalkChain pins the
// non-XFF variants' single-value semantics: a malformed multi-IP
// value (legal for XFF, illegal for X-Real-IP) is NOT walked as
// a chain. ResolveClientIPSingle parses the trimmed value as an
// IP and falls back to the peer IP on parse failure — it does
// not split on commas. The asserted contract is "peer IP",
// matching the resolver's malformed-value branch.
//
// If a future contract change introduces chain walking on a
// single-value variant (e.g. tolerating "1.1.1.1, 2.2.2.2" for
// X-Real-IP), this test must be updated in lockstep — that
// behaviour shift is observable to operators and needs an
// explicit migration note.
func TestE2E_TrustedProxy_AltHeader_DoesNotWalkChain(t *testing.T) {
	baseURL := startAltHeaderGateway(t, ":8087", "X-Real-IP", "private")

	got := callWhoamiWithHeader(t, baseURL, "X-Real-IP", "1.1.1.1, 2.2.2.2")
	assert.Equal(t, "127.0.0.1", got,
		"single-value X-Real-IP with comma-separated value is malformed → resolver falls back to peer IP, never walks the 'chain'")
	assert.NotEqual(t, "2.2.2.2", got,
		"single-value resolver MUST NOT pick the rightmost entry — that is XFF semantics")
	assert.NotEqual(t, "1.1.1.1", got,
		"single-value resolver MUST NOT pick the leftmost entry either")
}

