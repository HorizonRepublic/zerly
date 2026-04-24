package http

import (
	"fmt"
	"math"

	"github.com/cloudwego/hertz/pkg/app/server"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/rs/zerolog"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/config"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/proxy"
)

// resolveMaxBodyBytes converts the operator-supplied
// HTTP_MAX_BODY_BYTES (a signed 64-bit value) into the int Hertz
// expects for its WithMaxRequestBodySize option.
//
// Negative inputs are a deliberate misconfiguration — there is no
// sensible interpretation of a "negative-byte" body cap, and silently
// treating it as zero would block every request. Returning an error
// fails startup loud so operators correct the value before traffic
// hits the pod.
//
// Inputs above math.MaxInt32 are clamped to math.MaxInt32 with a WARN
// log. On 32-bit platforms a plain int cast would overflow and Hertz
// would silently apply a tiny limit; clamping preserves the operator's
// intent ("accept very large bodies") without losing data integrity.
func resolveMaxBodyBytes(v int64, logger zerolog.Logger) (int, error) {
	if v < 0 {
		return 0, fmt.Errorf("HTTP_MAX_BODY_BYTES must be non-negative, got %d", v)
	}

	if v > math.MaxInt32 {
		logger.Warn().
			Int64("requested", v).
			Int64("clamped", int64(math.MaxInt32)).
			Msg("HTTP_MAX_BODY_BYTES exceeds int32 range; clamping to MaxInt32")

		return math.MaxInt32, nil
	}

	return int(v), nil
}

// withNoDefaultServerHeader disables Hertz's automatic
// `Server: hertz` response header. Hertz's public option API
// does not expose a helper for this flag even though the
// underlying `config.Options.NoDefaultServerHeader` field is
// public, so we build the option in-line against the documented
// struct shape (`config.Option` is a tiny `{F func(*Options)}`
// wrapper). Keeping the server name off the wire is both a
// fingerprinting-surface reduction and a consistency choice —
// the gateway should not leak its transport implementation into
// every response.
func withNoDefaultServerHeader() hertzconfig.Option {
	return hertzconfig.Option{
		F: func(o *hertzconfig.Options) {
			o.NoDefaultServerHeader = true
		},
	}
}

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
// The server is HTTP/1.1 with keep-alive. HTTP/2 (h2c) is not wired
// today because Hertz h2c requires additional imports that have not
// been brought in. Until that lands, the config surface carries no
// HTTP/2 knob — accepting an operator toggle that has no effect is
// worse than requiring a code change to enable it.
//
// Returns an error when cfg.MaxBodyBytes is negative — see
// resolveMaxBodyBytes for the rationale. Callers MUST treat the
// error as fatal because partial server construction would race the
// rest of the bootstrap.
func NewServer(cfg *config.Config, handler *proxy.Handler, logger zerolog.Logger) (*server.Hertz, error) {
	maxBody, err := resolveMaxBodyBytes(cfg.MaxBodyBytes, logger)
	if err != nil {
		return nil, fmt.Errorf("http server: %w", err)
	}

	h := server.Default(
		server.WithHostPorts(cfg.HTTPAddr),
		server.WithMaxRequestBodySize(maxBody),
		server.WithMaxHeaderBytes(cfg.MaxHeaderBytes),
		server.WithReadTimeout(cfg.ReadTimeout),
		server.WithWriteTimeout(cfg.WriteTimeout),
		server.WithIdleTimeout(cfg.IdleTimeout),
		server.WithExitWaitTime(cfg.ShutdownTimeout),
		server.WithKeepAlive(true),
		withNoDefaultServerHeader(),
	)

	h.Use(newTrustedProxyMiddleware(cfg.TrustedProxies))
	h.Any("/*path", NewHertzAdapter(handler))

	return h, nil
}
