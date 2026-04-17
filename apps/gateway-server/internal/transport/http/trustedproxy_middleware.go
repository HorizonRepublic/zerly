package http

import (
	"context"
	"net"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/trustedproxy"
)

// clientIPUserKey is the per-request context slot the trusted-proxy
// middleware writes and the adapter reads. Unexported constant so the
// middleware and adapter share the literal without any other code in
// the package (or outside) introducing a parallel key.
const clientIPUserKey = "client_ip"

// xForwardedForHeader is the single HTTP header the resolver reads.
// The MVP deliberately does NOT consume X-Real-IP or vendor-specific
// headers (CF-Connecting-IP, True-Client-IP) — exotic deployments
// opt in via a follow-up extension of the resolver.
const xForwardedForHeader = "X-Forwarded-For"

// newTrustedProxyMiddleware returns a Hertz handler that resolves
// the client IP for every incoming request and stamps it on the
// request context via ctx.Set(clientIPUserKey, ip). The adapter
// reads the stamped value in buildServeInput via resolveRemoteAddr.
//
// The middleware is small by design: all trust logic lives in the
// pure trustedproxy package. This wrapper extracts framework inputs
// (peer TCP address, XFF header), calls ResolveClientIP, and writes
// the result back onto Hertz's context. The resolver's nil-safe
// behaviour means a non-TCP peer (exotic test setup, Unix socket)
// degrades gracefully — the resolver returns the empty string, the
// adapter's fallback path invokes ctx.ClientIP(), and the request
// still serves.
func newTrustedProxyMiddleware(trusted []*net.IPNet) app.HandlerFunc {
	return func(_ context.Context, ctx *app.RequestContext) {
		peerIP := extractPeerIP(ctx.RemoteAddr())
		xff := string(ctx.Request.Header.Peek(xForwardedForHeader))
		resolved := trustedproxy.ResolveClientIP(peerIP, xff, trusted)
		ctx.Set(clientIPUserKey, resolved)
	}
}

// extractPeerIP pulls the IP portion out of a net.Addr. For TCP
// connections (the only production transport) this is a simple
// type assertion. For anything else we return nil and the resolver
// handles it as an untrusted peer.
func extractPeerIP(addr net.Addr) net.IP {
	if tcp, ok := addr.(*net.TCPAddr); ok {
		return tcp.IP
	}

	return nil
}
