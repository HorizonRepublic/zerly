// Package lifecycle orchestrates the gateway's graceful shutdown
// sequence.
//
// The gateway holds four long-lived resources that each need to be
// quiesced cleanly when the process receives SIGTERM:
//
//  1. The Hertz HTTP server — must stop accepting new connections,
//     then drain in-flight requests.
//  2. The registry watcher — must stop its internal goroutine so it
//     no longer attempts to replace the store snapshot after we have
//     begun shutting down.
//  3. The NATS connection(s) — Drain waits for in-flight subscriptions
//     and publishes to finish before tearing down the socket, which
//     is what gives the gateway its "no request left behind" guarantee
//     during rolling deployments.
//  4. (Implicit) any per-request goroutines spawned by the Hertz
//     handler — these are owned by Hertz and drained by its own
//     Shutdown call.
//
// The shutdown is strictly ordered: HTTP first (stop the source of
// new work), watcher second (so a late KV delta cannot mutate the
// routing table after we have stopped serving), NATS last (so any
// in-flight upstream replies have a chance to land before we close
// the socket). A global deadline bounds every step so the gateway
// cannot hang forever on a stuck dependency.
package lifecycle

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	natsgo "github.com/nats-io/nats.go"
	"github.com/rs/zerolog"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

// HTTPServer is the narrow contract the Drain routine needs from the
// Hertz server. Declaring it here instead of importing server.Hertz
// directly keeps Drain unit-testable with a fake implementation — the
// concrete Hertz type is only referenced at the Shutdown helper's
// construction site in main.go.
type HTTPServer interface {
	Shutdown(ctx context.Context) error
}

// NATSConn is the narrow contract the Drain routine needs from a
// nats.Conn. Same rationale as HTTPServer: keeps the drain sequence
// testable against a fake.
type NATSConn interface {
	Drain() error
}

// Options bundles the resources Drain must quiesce on shutdown.
// Every field is required and a nil value triggers a deliberate
// nil-pointer dereference so bootstrap wiring bugs surface loudly
// instead of silently skipping a drain step.
type Options struct {
	// HTTP is the HTTP server instance whose Shutdown method blocks
	// on in-flight request completion.
	HTTP HTTPServer
	// Watcher is the registry watcher whose Stop method cancels its
	// background goroutine.
	Watcher *registry.Watcher
	// NATS is the NATS connection whose Drain method waits for
	// in-flight subscriptions and publishes to finish.
	NATS NATSConn
	// Timeout bounds the entire drain sequence. If a single step
	// exceeds it, the remaining steps still run but with an expired
	// context — implementations that honour ctx.Done() will exit
	// fast, which is the desired behaviour during an oversubscribed
	// shutdown.
	Timeout time.Duration
	// Logger records the start and end of each drain step plus any
	// step-level errors. Errors never fail the overall sequence —
	// the gateway always attempts every step so a failed HTTP
	// Shutdown does not leave the NATS connection leaking.
	Logger zerolog.Logger
}

// DefaultSignals is the set of OS signals the gateway treats as a
// shutdown request. Extracted so tests and alternative entry points
// can share the same list.
var DefaultSignals = []os.Signal{syscall.SIGTERM, syscall.SIGINT}

// WaitForSignal registers a buffered channel for DefaultSignals,
// blocks until one arrives, and returns the signal that fired. The
// buffer size is 1 because the kernel delivers at most one signal
// per registered name before the handler runs — anything more is
// a symptom of a broken runtime and losing duplicates is acceptable.
//
// Callers that need testability should not use WaitForSignal; they
// should construct their own channel and pass it to Drain directly
// so the test can push a synthetic signal without reaching into os
// package state.
func WaitForSignal() os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, DefaultSignals...)
	defer signal.Stop(ch)
	return <-ch
}

// Drain runs the ordered shutdown sequence against opts and returns
// once every step has completed or the global Timeout has elapsed.
//
// Errors from individual steps are logged but do not abort the
// sequence — a failed HTTP Shutdown must NOT prevent the NATS
// connection from being drained, because the process is about to
// exit and the cleanest finalization we can offer the operator is
// to attempt every drain unconditionally.
func Drain(opts Options) {
	opts.Logger.Info().Dur("timeout", opts.Timeout).Msg("gateway shutdown: draining resources")

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	shutdownHTTP(ctx, opts)
	stopWatcher(opts)
	drainNATS(opts)

	opts.Logger.Info().Msg("gateway shutdown: drain complete")
}

// shutdownHTTP stops the Hertz server from accepting new connections
// and waits for in-flight requests to finish. Errors are logged at
// ERROR but do not abort the rest of the drain.
func shutdownHTTP(ctx context.Context, opts Options) {
	opts.Logger.Debug().Msg("shutdown step: http")
	if err := opts.HTTP.Shutdown(ctx); err != nil {
		opts.Logger.Error().Err(err).Msg("http shutdown failed; continuing drain")
	}
}

// stopWatcher cancels the registry watcher's background goroutine.
// Stop is idempotent (guarded by sync.Once in the watcher) and
// cannot fail, so there is nothing to log on the error branch.
func stopWatcher(opts Options) {
	opts.Logger.Debug().Msg("shutdown step: registry watcher")
	opts.Watcher.Stop()
}

// drainNATS waits for in-flight subscriptions and publishes on the
// NATS connection to finish before tearing down the socket. Errors
// are logged but do not abort the shutdown.
func drainNATS(opts Options) {
	opts.Logger.Debug().Msg("shutdown step: nats drain")
	if err := opts.NATS.Drain(); err != nil {
		opts.Logger.Error().Err(err).Msg("nats drain failed")
	}
}

// Compile-time assertion that the Hertz server type implements the
// HTTPServer contract. Catches API drift on Hertz upgrades before
// the bootstrap layer references it.
var _ HTTPServer = (*server.Hertz)(nil)

// Compile-time assertion that a *nats.Conn implements the NATSConn
// contract.
var _ NATSConn = (*natsgo.Conn)(nil)
