// Package main is the entry point for zerly-gateway-server.
//
// The binary is a thin bootstrap: it loads configuration, constructs
// every internal component in dependency order, starts the HTTP
// server in a background goroutine, and blocks on SIGTERM. All
// non-bootstrap logic lives in the internal packages so this file
// stays auditable at a glance.
//
// Failure-path discipline: anything that happens before the zerolog
// logger is built writes to stderr and exits with code 1, because
// emitting structured JSON through a non-existent logger is
// impossible. Everything after goes through logger.Fatal() so
// operators get the same JSON shape for startup failures as for
// runtime errors. logger.Fatal() already calls os.Exit(1) internally,
// so no explicit exit follows it.
package main

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/auth"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/config"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/lifecycle"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/observability"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/proxy"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/ratelimit"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/routing"
	httptransport "github.com/HorizonRepublic/zerly/apps/gateway-server/internal/transport/http"
	natstransport "github.com/HorizonRepublic/zerly/apps/gateway-server/internal/transport/nats"
)

// main wires the gateway end-to-end. The body is intentionally a flat
// sequence of helper calls so the control flow — config, logger,
// NATS, KV, registry, routing, requester, handler, HTTP server,
// block-on-signal, drain — is readable in under 30 lines.
func main() {
	cfg := loadConfigOrDie()
	logger := buildLoggerOrDie(cfg)
	logger.Info().
		Str("http_addr", cfg.HTTPAddr).
		Strs("nats_urls", cfg.NATSUrls).
		Str("kv_bucket", cfg.KVBucket).
		Msg("starting zerly-gateway-server")

	ctx := context.Background()

	nc := connectNATSOrDie(cfg, logger)
	js, kv := openKVOrDie(ctx, nc, cfg, logger)
	_ = js // consumed by the ratelimit Router bootstrap in the next wiring step

	store := registry.NewStore()
	watcher := registry.NewWatcher(kv, store, logger)
	currentTable := installRoutingRebuild(store, watcher, logger)

	if err := watcher.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("registry watcher start failed")
	}

	rlRouter := ratelimit.NewRouter(ratelimit.FailPolicyOpen.Resolve(), logger)
	if err := rlRouter.EnsureBackend("memory", func() (ratelimit.Store, error) {
		return ratelimit.NewMemoryStore(10 * time.Minute), nil
	}); err != nil {
		logger.Fatal().Err(err).Msg("ratelimit memory backend init failed")
	}
	defer func() { _ = rlRouter.Close() }()

	requester := buildRequesterOrDie(nc, logger)
	handler := buildProxyHandler(cfg, currentTable, requester, rlRouter, logger)
	httpServer := httptransport.NewServer(cfg, handler)

	// Run the Hertz server directly instead of Spin() so that its
	// built-in SIGTERM/SIGINT handler does not race with our own
	// lifecycle.WaitForSignal. Spin() always registers its own
	// signal waiter, and when two goroutines listen for the same
	// signal the one that wakes first tears down the engine — if
	// Hertz wins, lifecycle.Drain's httpServer.Shutdown sees
	// "engine is not running" and in-flight requests are dropped
	// instead of drained. Run() blocks until Shutdown is called
	// externally, which is exactly the handoff our lifecycle
	// package expects.
	go func() {
		if err := httpServer.Run(); err != nil {
			logger.Error().Err(err).Msg("hertz server exited unexpectedly")
		}
	}()
	logger.Info().Str("addr", cfg.HTTPAddr).Msg("http server started")

	sig := lifecycle.WaitForSignal()
	logger.Info().Str("signal", sig.String()).Msg("shutdown signal received")
	lifecycle.Drain(lifecycle.Options{
		HTTP:    httpServer,
		Watcher: watcher,
		NATS:    nc,
		Timeout: cfg.ShutdownTimeout,
		Logger:  logger,
	})
}

// loadConfigOrDie loads the operator-facing config and terminates the
// process via stderr + os.Exit if it is missing or malformed. The
// logger is not yet available at this point, so the error path
// bypasses zerolog entirely and writes to stderr in plain text —
// this is the ONE place in the bootstrap where structured logging
// cannot be used because its dependency has not been built yet.
func loadConfigOrDie() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway: config load failed: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

// buildLoggerOrDie constructs the zerolog logger from cfg. Any failure
// is terminal — without a working logger the rest of the bootstrap
// would emit to /dev/null and operators would have no diagnostic
// surface to debug why the pod is in CrashLoopBackOff. Like
// loadConfigOrDie, the error path writes to stderr because the
// logger that WOULD carry the error is the very thing that just
// failed to construct.
func buildLoggerOrDie(cfg *config.Config) zerolog.Logger {
	logger, err := observability.NewLogger(cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway: logger init failed: %v\n", err)
		os.Exit(1)
	}
	return logger
}

// connectNATSOrDie dials the NATS cluster and returns a live
// connection. Failure is fatal because the gateway has no reason to
// run without a NATS link — every request path ends in a Core NATS
// request/reply, and a gateway that cannot reach NATS is strictly
// worse than no gateway (it would 503 every request with zero
// useful diagnostic signal).
func connectNATSOrDie(cfg *config.Config, logger zerolog.Logger) *natsgo.Conn {
	nc, err := natstransport.Connect(cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("nats connect failed")
	}
	return nc
}

// openKVOrDie initializes the JetStream client and opens the
// handler_registry KV bucket that holds the routing metadata.
// It returns both the JetStream context and the KeyValue handle:
// the JetStream context is needed by downstream components that
// create additional KV buckets at startup (e.g., the ratelimit
// Router). Failure is fatal because a gateway with no routing table
// cannot forward a single request — refusing to start is strictly
// better than starting in a state where every request 404s.
func openKVOrDie(
	ctx context.Context,
	nc *natsgo.Conn,
	cfg *config.Config,
	logger zerolog.Logger,
) (jetstream.JetStream, jetstream.KeyValue) {
	js, err := jetstream.New(nc)
	if err != nil {
		logger.Fatal().Err(err).Msg("jetstream init failed")
	}
	kv, err := js.KeyValue(ctx, cfg.KVBucket)
	if err != nil {
		logger.Fatal().Err(err).Str("bucket", cfg.KVBucket).Msg("open kv bucket failed")
	}
	return js, kv
}

// installRoutingRebuild wires the routing table rebuild callback into
// the watcher. The first rebuild runs synchronously against whatever
// snapshot the store currently holds so a nil table is never observed
// by the proxy handler. Subsequent rebuilds fire on every KV change
// because the watcher invokes registered callbacks in registration
// order after every successful Store.Replace.
//
// The closure captured below tracks prevRoutes and firstLoad across
// rebuilds to emit lifecycle log entries: a single INFO "initial
// route set published" on the first rebuild, then an INFO or DEBUG
// "table rebuilt" on every subsequent rebuild depending on whether
// the delta is empty. The closure is touched by exactly one
// goroutine at a time — the watcher invokes OnChange callbacks
// serially on its single watch goroutine (see registry.Watcher
// godoc) and the initial synchronous call happens on the main
// goroutine before OnChange is registered and before Start is
// called, so the two phases never overlap. A future refactor that
// parallelises callbacks MUST add explicit synchronisation here.
//
// The returned *atomic.Value stores the current routing.Table; the
// proxy handler's TableProvider closure calls Load().(routing.Table)
// for a lock-free, always-consistent snapshot. atomic.Value is used
// rather than atomic.Pointer[routing.Table] because routing.Table is
// an interface — atomic.Pointer[Interface] would store a pointer-to-
// interface, introducing a second indirection on every request
// lookup for no benefit.
func installRoutingRebuild(
	store *registry.Store,
	watcher *registry.Watcher,
	logger zerolog.Logger,
) *atomic.Value {
	var (
		current    atomic.Value
		prevRoutes []routing.Route
		firstLoad  = true
	)

	rebuild := func() {
		snapshot := store.Get()
		verifiers := auth.BuildVerifierRegistry(snapshot, logger)
		nextRoutes := routing.CollectRoutes(snapshot, verifiers, logger)

		if firstLoad {
			routing.LogInitialLoad(nextRoutes, logger)
			firstLoad = false
		} else {
			delta := routing.ComputeDelta(prevRoutes, nextRoutes)
			routing.LogDelta(delta, nextRoutes, logger)
		}

		// The routing builder pre-resolves every verifier id into the
		// corresponding Route.Auth.VerifierSubject at build time, so
		// the proxy handler never needs live access to the registry —
		// it reads the subject directly off the matched route. The
		// VerifierRegistry itself is short-lived: only the routing
		// builder consumes it, and it is discarded at the end of each
		// rebuild. Future verifier-result caching can reintroduce a
		// long-lived registry handle if id-keyed lookups become
		// necessary at request time.
		current.Store(routing.BuildTableFromRoutes(nextRoutes))
		prevRoutes = nextRoutes
	}

	rebuild()
	watcher.OnChange(rebuild)

	return &current
}

// buildRequesterOrDie constructs the NATS Requester pool. By default
// the pool holds a single connection; increasing the pool size is a
// tuning knob justified only by benchmark evidence of contention on
// the single-socket send path. Raising it speculatively would add
// reconnect complexity and connection-limit pressure on the NATS
// cluster with no demonstrable throughput benefit.
func buildRequesterOrDie(nc *natsgo.Conn, logger zerolog.Logger) *natstransport.Requester {
	requester, err := natstransport.NewRequester([]*natsgo.Conn{nc})
	if err != nil {
		logger.Fatal().Err(err).Msg("nats requester init failed")
	}
	return requester
}

// buildProxyHandler assembles the HTTP->NATS orchestration handler
// with its dependencies. The Table provider closure captures the
// *atomic.Value returned by installRoutingRebuild so every request
// sees the latest routing snapshot without any coordination between
// the request path and the watcher goroutine.
func buildProxyHandler(
	cfg *config.Config,
	currentTable *atomic.Value,
	requester *natstransport.Requester,
	rlRouter *ratelimit.Router,
	logger zerolog.Logger,
) *proxy.Handler {
	return proxy.NewHandler(proxy.HandlerConfig{
		Table: func() routing.Table {
			return currentTable.Load().(routing.Table)
		},
		Nats:        requester,
		Encoder:     proxy.NewDefaultEncoder(),
		Decoder:     proxy.NewDefaultDecoder(),
		Timeout:     cfg.RequestTimeout,
		Logger:      logger,
		RateLimiter: rlRouter,
	})
}
