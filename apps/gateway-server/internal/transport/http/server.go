package http

import (
	"github.com/cloudwego/hertz/pkg/app/server"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/config"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/proxy"
)

// NewServer constructs a Hertz server bound to handler via a single
// catch-all route "/*path". Dynamic HTTP-to-RPC routing is performed
// INSIDE proxy.Handler via the routing.Table, so Hertz itself sees
// only one registered route and incurs no per-request routing cost.
//
// The returned *server.Hertz is NOT started — callers are expected
// to call h.Run() in a goroutine once all dependencies have been
// assembled, and then block on the lifecycle package for signal-
// driven shutdown. Returning the unstarted server lets tests
// construct it against an ephemeral port without touching the
// network.
//
// ExitWaitTimeout is aligned with cfg.ShutdownTimeout so there is a
// single operator-facing knob ("SHUTDOWN_TIMEOUT") for the total
// drain budget. Hertz internally bounds its Shutdown context by
// ExitWaitTimeout — without this override it would cap at Hertz's
// default 5s and the lifecycle package's longer budget would be
// silently ignored.
//
// HTTP/2 (h2c) support is NOT wired even though cfg.EnableHTTP2
// exists — Hertz h2c requires additional imports that have not yet
// been brought in. The current server is HTTP/1.1 with keep-alive,
// which is sufficient for unit tests, benchmarks, and local smoke
// tests.
func NewServer(cfg *config.Config, handler *proxy.Handler) *server.Hertz {
	h := server.Default(
		server.WithHostPorts(cfg.HTTPAddr),
		server.WithMaxRequestBodySize(int(cfg.MaxBodyBytes)),
		server.WithReadTimeout(cfg.ReadTimeout),
		server.WithWriteTimeout(cfg.WriteTimeout),
		server.WithIdleTimeout(cfg.IdleTimeout),
		server.WithExitWaitTime(cfg.ShutdownTimeout),
		server.WithKeepAlive(true),
	)

	h.Any("/*path", NewHertzAdapter(handler))

	return h
}
