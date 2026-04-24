package ratelimit

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/routing"
)

type stubStore struct {
	name string
}

func (s *stubStore) Allow(context.Context, string, int, int) (Decision, error) {
	return Decision{Allowed: true}, nil
}

func (*stubStore) FlushPrefix(context.Context, string) error {
	return nil
}

func (*stubStore) Close() error {
	return nil
}

func TestRouter_DispatchByStoreField(t *testing.T) {
	mem := NewMemoryStore(time.Minute)
	defer func() { _ = mem.Close() }()
	kv := &stubStore{name: "nats-kv"}

	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())
	require.NoError(t, r.EnsureBackend("memory", func() (Store, error) { return mem, nil }))
	require.NoError(t, r.EnsureBackend("nats-kv", func() (Store, error) { return kv, nil }))

	assert.Same(t, mem, r.StoreFor(routing.Route{RateLimit: &registry.RateLimitMeta{Store: "memory"}}))
	assert.Same(t, kv, r.StoreFor(routing.Route{RateLimit: &registry.RateLimitMeta{Store: "nats-kv"}}))
}

func TestRouter_EmptyStoreDefaultsToMemory(t *testing.T) {
	mem := NewMemoryStore(time.Minute)
	defer func() { _ = mem.Close() }()

	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())
	require.NoError(t, r.EnsureBackend("memory", func() (Store, error) { return mem, nil }))

	assert.Same(t, mem, r.StoreFor(routing.Route{RateLimit: &registry.RateLimitMeta{Store: ""}}))
}

func TestRouter_NilRateLimitReturnsMemory(t *testing.T) {
	mem := NewMemoryStore(time.Minute)
	defer func() { _ = mem.Close() }()

	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())
	require.NoError(t, r.EnsureBackend("memory", func() (Store, error) { return mem, nil }))

	assert.Same(t, mem, r.StoreFor(routing.Route{}))
}

func TestRouter_UnknownStoreFallsBackToMemory(t *testing.T) {
	mem := NewMemoryStore(time.Minute)
	defer func() { _ = mem.Close() }()

	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())
	require.NoError(t, r.EnsureBackend("memory", func() (Store, error) { return mem, nil }))

	got := r.StoreFor(routing.Route{RateLimit: &registry.RateLimitMeta{Store: "redis"}})
	assert.Same(t, mem, got)
	assert.Equal(t, int64(1), r.Counters()["ratelimit_store_fallback"],
		"each fallback to memory bumps the observability counter")
}

func TestRouter_EnsureBackendIdempotent(t *testing.T) {
	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())
	var calls atomic.Int32
	factory := func() (Store, error) {
		calls.Add(1)
		mem := NewMemoryStore(time.Minute)
		t.Cleanup(func() { _ = mem.Close() })
		return mem, nil
	}
	require.NoError(t, r.EnsureBackend("memory", factory))
	require.NoError(t, r.EnsureBackend("memory", factory))
	assert.Equal(t, int32(1), calls.Load(), "factory must run once")
}

func TestRouter_EnsureBackendFactoryError(t *testing.T) {
	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())
	err := r.EnsureBackend("nats-kv", func() (Store, error) { return nil, errors.New("boom") })
	assert.Error(t, err)
}

// TestRouter_StoreForAfterCloseReturnsClosedSentinel guards the race
// where a request is dispatched through the router while shutdown is
// in flight. Before the fix, Close() closed every store but left the
// map populated, so StoreFor returned a closed store whose Allow
// could panic or misbehave. The router now flips into a terminal
// closed state and StoreFor returns a sentinel whose Allow surfaces
// ErrStoreClosed, which the handler's FailPolicy maps to a
// well-defined decision.
func TestRouter_StoreForAfterCloseReturnsClosedSentinel(t *testing.T) {
	mem := NewMemoryStore(time.Minute)

	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())
	require.NoError(t, r.EnsureBackend("memory", func() (Store, error) { return mem, nil }))

	require.NoError(t, r.Close())

	store := r.StoreFor(routing.Route{})
	require.NotNil(t, store, "StoreFor must never return nil; the handler dereferences it on the hot path")

	// Allow on the sentinel must surface ErrStoreClosed — never a
	// panic — so the FailPolicy path picks up the decision.
	_, err := store.Allow(context.Background(), "k", 10, 5)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStoreClosed)

	// FlushPrefix and Close on the sentinel are idempotent no-ops.
	assert.NoError(t, store.FlushPrefix(context.Background(), "k"))
	assert.NoError(t, store.Close())
}

func TestRouter_CloseIsIdempotent(t *testing.T) {
	mem := NewMemoryStore(time.Minute)

	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())
	require.NoError(t, r.EnsureBackend("memory", func() (Store, error) { return mem, nil }))

	require.NoError(t, r.Close())
	require.NoError(t, r.Close(),
		"second Close must be a no-op rather than double-closing the underlying stores")
}

// TestRouter_StoreForReturnsClosedSentinelWhenMemoryMissing pins
// the defensive behaviour at the bottom of the resolution chain: if
// bootstrap forgets to register the memory backend, StoreFor MUST
// return a non-nil Store whose Allow surfaces ErrStoreClosed. The
// hot-path handler dereferences the result without nil checks, so a
// nil return here would be a panic.
func TestRouter_StoreForReturnsClosedSentinelWhenMemoryMissing(t *testing.T) {
	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())

	store := r.StoreFor(routing.Route{Method: "GET", PathTemplate: "/x"})
	require.NotNil(t, store, "StoreFor must never return nil; the handler dereferences it on the hot path")

	_, err := store.Allow(context.Background(), "k", 10, 5)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStoreClosed)

	// The fallback counter must have bumped so operators can surface
	// the misconfiguration through metrics — otherwise a missing
	// backend hides forever behind the nil-safe FailPolicy decision.
	assert.GreaterOrEqual(t, r.Counters()["ratelimit_store_fallback"], int64(1),
		"missing memory backend must bump the fallback counter")
}

// TestRouter_StoreForUnknownStoreWithoutMemoryReturnsClosedSentinel
// covers the combined path: declared backend missing, memory backend
// also missing. Both fallbacks log and bump the counter, but the
// final return must still be a non-nil Store.
func TestRouter_StoreForUnknownStoreWithoutMemoryReturnsClosedSentinel(t *testing.T) {
	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())

	store := r.StoreFor(routing.Route{
		Method:       "POST",
		PathTemplate: "/y",
		RateLimit:    &registry.RateLimitMeta{Store: "redis"},
	})
	require.NotNil(t, store, "StoreFor must never return nil even when both declared and fallback backends are missing")

	_, err := store.Allow(context.Background(), "k", 10, 5)
	require.ErrorIs(t, err, ErrStoreClosed)
}

func TestRouter_EnsureBackendAfterCloseRefuses(t *testing.T) {
	r := NewRouter(FailPolicyOpen.Resolve(), zerolog.Nop())
	require.NoError(t, r.Close())

	// Factory must not even be invoked after close — that would leak
	// resources the router cannot track through its shutdown path.
	var called atomic.Bool
	err := r.EnsureBackend("memory", func() (Store, error) {
		called.Store(true)
		mem := NewMemoryStore(time.Minute)
		t.Cleanup(func() { _ = mem.Close() })

		return mem, nil
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStoreClosed)
	assert.False(t, called.Load(),
		"factory must not run after Close; the sentinel prevents registration")
}
