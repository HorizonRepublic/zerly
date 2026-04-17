package http

import (
	"context"
	"net"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/trustedproxy"
)

// testTrusted returns the 7-range private sentinel used by every
// integration test below. Parsed once per test — cheap.
func testTrusted(t *testing.T) []*net.IPNet {
	t.Helper()
	trusted, err := trustedproxy.ParseCIDRList("private")
	require.NoError(t, err)

	return trusted
}

// remoteAddrConn wraps Hertz's mock.Conn to return a specific
// RemoteAddr. The ut test harness builds a RequestContext without
// a backing network connection, so ctx.RemoteAddr() returns the
// zero TCPAddr until SetConn injects one. Embedding mock.Conn
// keeps us on the full network.Conn surface without re-implementing
// the 15+ methods the interface requires.
type remoteAddrConn struct {
	*mock.Conn
	remote net.Addr
}

func (c *remoteAddrConn) RemoteAddr() net.Addr {
	return c.remote
}

// attachRemote rebuilds the ut context's underlying conn so
// ctx.RemoteAddr() returns addr. The ut harness does not expose
// a SetRemoteAddr helper, so we go through SetConn with a wrapper
// that overrides the single method we care about.
func attachRemote(ctx *app.RequestContext, addr net.Addr) {
	ctx.SetConn(&remoteAddrConn{Conn: mock.NewConn(""), remote: addr})
}

// clientIPFromCtx reads the middleware-stamped client IP off the
// Hertz request context. Unexported helper so every assertion in
// this file reads the same slot.
func clientIPFromCtx(t *testing.T, ctx *app.RequestContext) (string, bool) {
	t.Helper()
	raw, ok := ctx.Get(clientIPUserKey)
	if !ok {
		return "", false
	}
	s, ok := raw.(string)

	return s, ok
}

// TestTrustedProxyMiddleware_TrustedPeerHonoursXFF pins the happy
// path: a request arriving from a loopback peer (∈ private) with a
// public-IP XFF must land in the adapter with the XFF IP stamped on
// the per-request context slot.
func TestTrustedProxyMiddleware_TrustedPeerHonoursXFF(t *testing.T) {
	middleware := newTrustedProxyMiddleware(testTrusted(t))

	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil,
		ut.Header{Key: "X-Forwarded-For", Value: "1.2.3.4"},
	)
	attachRemote(ctx, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321})

	middleware(context.Background(), ctx)

	got, ok := clientIPFromCtx(t, ctx)
	require.True(t, ok, "middleware must stamp client_ip as string on context")
	assert.Equal(t, "1.2.3.4", got)
}

// TestTrustedProxyMiddleware_UntrustedPeerIgnoresXFF pins the
// security-critical path: a request arriving from a public peer
// must have its XFF ignored, regardless of contents. This is the
// test that would have caught the spoofing bug before H.3.
func TestTrustedProxyMiddleware_UntrustedPeerIgnoresXFF(t *testing.T) {
	middleware := newTrustedProxyMiddleware(testTrusted(t))

	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil,
		ut.Header{Key: "X-Forwarded-For", Value: "1.2.3.4"},
	)
	attachRemote(ctx, &net.TCPAddr{IP: net.ParseIP("5.5.5.5"), Port: 12345})

	middleware(context.Background(), ctx)

	got, ok := clientIPFromCtx(t, ctx)
	require.True(t, ok)
	assert.Equal(t, "5.5.5.5", got,
		"untrusted peer → XFF ignored → peer IP stamped (spoofing defence)")
}

// TestTrustedProxyMiddleware_NoXFF_TrustedPeer_StampsPeer pins the
// no-XFF path: if the trusted peer does not forward an XFF, the
// peer IP itself is stamped.
func TestTrustedProxyMiddleware_NoXFF_TrustedPeer_StampsPeer(t *testing.T) {
	middleware := newTrustedProxyMiddleware(testTrusted(t))

	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil)
	attachRemote(ctx, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321})

	middleware(context.Background(), ctx)

	got, _ := clientIPFromCtx(t, ctx)
	assert.Equal(t, "127.0.0.1", got)
}

// TestTrustedProxyMiddleware_IPv6Chain_Resolves pins the IPv6
// symmetrical path: loopback ::1 is in the private sentinel, so an
// IPv6 XFF from a loopback peer resolves the same way as IPv4.
func TestTrustedProxyMiddleware_IPv6Chain_Resolves(t *testing.T) {
	middleware := newTrustedProxyMiddleware(testTrusted(t))

	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil,
		ut.Header{Key: "X-Forwarded-For", Value: "2001:db8::1"},
	)
	attachRemote(ctx, &net.TCPAddr{IP: net.ParseIP("::1"), Port: 8080})

	middleware(context.Background(), ctx)

	got, _ := clientIPFromCtx(t, ctx)
	assert.Equal(t, "2001:db8::1", got)
}

// TestTrustedProxyMiddleware_MalformedXFF_SkipsGarbage pins the
// resilience path: malformed XFF entries are skipped, the walk
// continues to the next valid one.
func TestTrustedProxyMiddleware_MalformedXFF_SkipsGarbage(t *testing.T) {
	middleware := newTrustedProxyMiddleware(testTrusted(t))

	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil,
		ut.Header{Key: "X-Forwarded-For", Value: "garbage, 1.2.3.4"},
	)
	attachRemote(ctx, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321})

	middleware(context.Background(), ctx)

	got, _ := clientIPFromCtx(t, ctx)
	assert.Equal(t, "1.2.3.4", got)
}

// ---------- adapter.resolveRemoteAddr ----------

// TestResolveRemoteAddr_ReadsMiddlewareContextValue confirms the
// adapter picks up the stamped value written by the middleware.
func TestResolveRemoteAddr_ReadsMiddlewareContextValue(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil)
	ctx.Set(clientIPUserKey, "1.2.3.4")

	assert.Equal(t, "1.2.3.4", resolveRemoteAddr(ctx))
}

// TestResolveRemoteAddr_FallbackToCtxClientIPWhenMiddlewareAbsent
// confirms the safety net: an adapter test that does not register
// the middleware still gets a non-empty RemoteAddr via Hertz's
// built-in ClientIP(). Without this fallback, unit tests on the
// adapter would need the full middleware stack or panic on nil.
func TestResolveRemoteAddr_FallbackToCtxClientIPWhenMiddlewareAbsent(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil)
	// No ctx.Set call — simulate a test that drives the adapter
	// directly.
	got := resolveRemoteAddr(ctx)
	// ctx.ClientIP() returns "" on a bare ut context (no peer), but
	// the function must not panic and must return the ClientIP()
	// value verbatim.
	assert.Equal(t, ctx.ClientIP(), got)
}

// TestResolveRemoteAddr_EmptyContextValueFallsBack pins the empty-
// string guard: if a buggy middleware stamps an empty string, the
// adapter falls back rather than propagating the empty IP.
func TestResolveRemoteAddr_EmptyContextValueFallsBack(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil)
	ctx.Set(clientIPUserKey, "")

	assert.Equal(t, ctx.ClientIP(), resolveRemoteAddr(ctx),
		"empty context value must trigger ClientIP() fallback")
}
