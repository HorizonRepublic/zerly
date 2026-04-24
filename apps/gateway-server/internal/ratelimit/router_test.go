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
