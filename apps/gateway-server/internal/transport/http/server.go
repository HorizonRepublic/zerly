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
// to call h.Spin() (or a custom start method in Milestone 21) once
// all dependencies have been assembled. Returning the unstarted
// server lets tests construct it against an ephemeral port without
// touching the network.
//
// HTTP/2 (h2c) support is NOT wired in Milestone 18 even though
// cfg.EnableHTTP2 exists — Hertz h2c requires additional imports
// that belong with the bootstrap wiring milestone (M21). The
// current server is HTTP/1.1 with keep-alive, which is sufficient
// for unit tests, benchmarks, and local smoke tests.
func NewServer(cfg *config.Config, handler *proxy.Handler) *server.Hertz {
	h := server.Default(
		server.WithHostPorts(cfg.HTTPAddr),
		server.WithMaxRequestBodySize(int(cfg.MaxBodyBytes)),
		server.WithReadTimeout(cfg.ReadTimeout),
		server.WithWriteTimeout(cfg.WriteTimeout),
		server.WithIdleTimeout(cfg.IdleTimeout),
		server.WithKeepAlive(true),
	)

	h.Any("/*path", NewHertzAdapter(handler))

	return h
}
