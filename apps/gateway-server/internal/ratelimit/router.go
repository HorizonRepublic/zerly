package ratelimit

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/routing"
)

// Router dispatches Allow calls to the appropriate Store backend
// based on a route's declared store id ("memory", "nats-kv",
// "redis"). Both gateway startup and the registry watcher's
// hot-reload path register backends through the single EnsureBackend
// method so the wiring stays uniform across cold boot and live
// reconfiguration.
//
// StoreFor falls back to "memory" when a route has no RateLimit
// config, declares an empty store id, or declares a backend the
// gateway has not registered (e.g. "redis" before a Redis adapter
// lands). The fallback is safe — memory is always registered at
// startup and produces correct rate-limit decisions, though per-pod
// rather than multi-replica. Every fallback bumps a counter so
// operators can surface misconfiguration through metrics once
// observability is wired.
type Router struct {
	mu         sync.RWMutex
	stores     map[string]Store
	failPolicy Policy
	logger     zerolog.Logger
	counters   struct {
		fallback atomic.Int64
	}
}

// NewRouter creates an empty Router bound to the given fail-policy
// and logger. Callers MUST register at least the "memory" backend
// via EnsureBackend before any Allow call reaches the router; the
// gateway bootstrap is responsible for this invariant.
//
// Example:
//
//	policy := FailPolicyOpen.Resolve()
//	router := NewRouter(policy, log)
//	// Register backends during startup.
//	router.EnsureBackend("memory", func() (Store, error) {
//		return NewMemoryStore(24 * time.Hour), nil
//	})
//	// Routes automatically select their store; hot-reload re-calls EnsureBackend.
func NewRouter(failPolicy Policy, logger zerolog.Logger) *Router {
	return &Router{
		stores:     make(map[string]Store),
		failPolicy: failPolicy,
		logger:     logger,
	}
}

// EnsureBackend registers a Store for the given id if one is not
// already present. factory runs exactly once per id; subsequent
// calls with the same id are no-ops so the watcher hot-reload path
// can re-scan the registry on every delta without duplicate
// instantiation or side effects. A factory error is propagated
// verbatim and leaves the router untouched.
//
// Safe for concurrent use alongside StoreFor.
func (r *Router) EnsureBackend(id string, factory func() (Store, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.stores[id]; ok {
		return nil
	}

	s, err := factory()
	if err != nil {
		return fmt.Errorf("ratelimit: register backend %q: %w", id, err)
	}

	r.stores[id] = s
	return nil
}

// StoreFor returns the Store that should service the given route.
// Resolution order:
//
//  1. `route.RateLimit == nil` or `route.RateLimit.Store == ""` → memory.
//  2. Declared id matches a registered backend → that backend.
//  3. Declared id has no matching backend → memory + fallback warn log
//     + fallback counter bump.
//
// The hot-path memory backend lookup uses RLock; it is safe to call
// concurrently with EnsureBackend.
func (r *Router) StoreFor(route routing.Route) Store {
	declared := "memory"
	if route.RateLimit != nil && route.RateLimit.Store != "" {
		declared = route.RateLimit.Store
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if s, ok := r.stores[declared]; ok {
		return s
	}

	if declared != "memory" {
		r.logger.Warn().
			Str("event", "ratelimit.store.fallback").
			Str("route", route.Method+":"+route.PathTemplate).
			Str("declared", declared).
			Str("fallback", "memory").
			Msg("declared rate-limit backend is not registered; falling back to memory")
		r.counters.fallback.Add(1)
	}

	return r.stores["memory"]
}

// FailPolicy returns the resolved Policy. Request handlers invoke
// Apply on it when Store.Allow returns a non-nil error so the
// open/closed/etc. decision is made consistently across all
// backends served by this router.
func (r *Router) FailPolicy() Policy { return r.failPolicy }

// Close closes every registered Store. The first error encountered
// is returned; the remaining stores are still closed on a
// best-effort basis so a failing backend does not leak the others.
func (r *Router) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for id, s := range r.stores {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close %s: %w", id, err)
		}
	}

	return firstErr
}

// Counters returns a snapshot of router-level observability
// counters. The keys are namespaced for future OpenTelemetry
// plumbing.
func (r *Router) Counters() map[string]int64 {
	return map[string]int64{
		"ratelimit_store_fallback": r.counters.fallback.Load(),
	}
}
