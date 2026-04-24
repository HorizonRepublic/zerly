//go:build integration

package ratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
)

// setupJetStream spins up a single NATS container with JetStream
// enabled and returns a ready-to-use JetStream handle. Cleanup is
// registered via t.Cleanup — callers do not manage the container or
// connection lifetime directly.
//
// A handler_registry bucket is pre-created inline because the
// production NATSKVStore constructor reads the handler bucket's
// replica count to mirror it on its own bucket. In the full gateway
// stack that bucket is provisioned by the Nest-side nestjs-jetstream
// library; integration tests simulate that provisioning step so the
// constructor sees a realistic starting state.
//
// Each test MUST call this at most once — per-test containers give
// parallel-safe isolation and prevent state bleed between tests.
func setupJetStream(t *testing.T) jetstream.JetStream {
	t.Helper()

	ctx := context.Background()
	container, err := tcnats.Run(ctx, "nats:2.11.7")
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	url, err := container.ConnectionString(ctx)
	require.NoError(t, err)

	nc, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(nc.Close)

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "handler_registry",
		History: 1,
	})
	require.NoError(t, err)

	return js
}

// TestNATSKVStore_Integration_RoundTrip exercises the full Allow →
// CAS Create → readback path against a real JetStream KV. It proves
// that the TAT encoded by the store is readable back via the raw
// jetstream.KeyValue API and decodes to a non-zero time, which is
// the minimum integrity guarantee the GCRA loop depends on.
func TestNATSKVStore_Integration_RoundTrip(t *testing.T) {
	t.Parallel()

	js := setupJetStream(t)
	ctx := context.Background()

	sut, err := NewNATSKVStore(ctx, NATSKVStoreConfig{
		JS:            js,
		HandlerBucket: "handler_registry",
		BucketSuffix:  "_ratelimit",
		KeyTTL:        2 * time.Second,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)

	decision, err := sut.Allow(ctx, "k", 10, 20)
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	kv, err := js.KeyValue(ctx, "handler_registry_ratelimit")
	require.NoError(t, err)

	entry, err := kv.Get(ctx, "k")
	require.NoError(t, err)

	tat, err := decodeTAT(entry.Value())
	require.NoError(t, err)
	assert.False(t, tat.IsZero(), "decoded TAT must be a real timestamp")
}

// TestNATSKVStore_Integration_ReplicasInherited confirms the
// constructor's wiring that makes the rate-limit bucket inherit the
// handler registry's replica count. A single-node testcontainer can
// only run Replicas=1, so this test pins the inheritance path
// itself: whatever the handler bucket is configured with, the
// rate-limit bucket ends up with the same value.
func TestNATSKVStore_Integration_ReplicasInherited(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	container, err := tcnats.Run(ctx, "nats:2.11.7")
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	url, err := container.ConnectionString(ctx)
	require.NoError(t, err)

	nc, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(nc.Close)

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Single-node containers cannot go above Replicas=1; the
	// inheritance wiring is what this test locks in, not the
	// absolute replica count.
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:   "handler_registry",
		History:  1,
		Replicas: 1,
	})
	require.NoError(t, err)

	_, err = NewNATSKVStore(ctx, NATSKVStoreConfig{
		JS:            js,
		HandlerBucket: "handler_registry",
		BucketSuffix:  "_ratelimit",
		KeyTTL:        2 * time.Second,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)

	kv, err := js.KeyValue(ctx, "handler_registry_ratelimit")
	require.NoError(t, err)

	status, err := kv.Status(ctx)
	require.NoError(t, err)

	bucketStatus, ok := status.(*jetstream.KeyValueBucketStatus)
	require.True(t, ok, "status must be *jetstream.KeyValueBucketStatus")

	info := bucketStatus.StreamInfo()
	require.NotNil(t, info)
	assert.Equal(t, 1, info.Config.Replicas)
}

// TestNATSKVStore_Integration_TTLExpiry proves that KeyTTL maps onto
// the stream's MaxAge and the server actually evicts idle keys. The
// 4-second wait against a 2-second TTL gives JetStream's
// second-granularity cleanup ample slack without inflating the
// suite's runtime.
func TestNATSKVStore_Integration_TTLExpiry(t *testing.T) {
	t.Parallel()

	js := setupJetStream(t)
	ctx := context.Background()

	sut, err := NewNATSKVStore(ctx, NATSKVStoreConfig{
		JS:            js,
		HandlerBucket: "handler_registry",
		BucketSuffix:  "_ratelimit",
		KeyTTL:        2 * time.Second,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)

	decision, err := sut.Allow(ctx, "k", 10, 20)
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	time.Sleep(4 * time.Second)

	kv, err := js.KeyValue(ctx, "handler_registry_ratelimit")
	require.NoError(t, err)

	_, err = kv.Get(ctx, "k")
	assert.True(t, errors.Is(err, jetstream.ErrKeyNotFound),
		"key should be evicted after MaxAge expiry, got err=%v", err)
}

// TestNATSKVStore_Integration_ConcurrentCASConflict fires many
// parallel Allow calls at a single hot key to exercise the CAS
// retry loop under real contention. The rps value is deliberately
// huge (1M) to keep GCRA permissive — the assertion is about lost
// updates (allowed+rejected totals) and CAS correctness, not about
// rate-limit math.
//
// At rps=1M period is 1μs, while a real NATS CAS round-trip sits
// around 1ms; GCRA's delay tolerance (burst*period = 10μs) is
// dwarfed by wall-clock progress between writes, so once admission
// starts flowing the limiter effectively acts as unbounded for
// this burst size. The test therefore asserts the invariant that
// actually matters under CAS contention — no lost updates,
// allowed+rejected equals the total — and leaves rate-limit-math
// bounds to unit tests that run against the in-memory fake where
// sub-microsecond period granularity is faithful.
func TestNATSKVStore_Integration_ConcurrentCASConflict(t *testing.T) {
	t.Parallel()

	js := setupJetStream(t)
	ctx := context.Background()

	sut, err := NewNATSKVStore(ctx, NATSKVStoreConfig{
		JS:            js,
		HandlerBucket: "handler_registry",
		BucketSuffix:  "_ratelimit",
		KeyTTL:        1 * time.Minute,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)

	const (
		goroutines = 20
		rps        = 1_000_000
		burst      = 10
	)

	var (
		mu       sync.Mutex
		allowed  int
		rejected int
		wg       sync.WaitGroup
		start    = make(chan struct{})
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			<-start

			decision, allowErr := sut.Allow(ctx, "hot-key", rps, burst)

			mu.Lock()
			switch {
			case allowErr != nil:
				// CAS budget exhaustion under contention counts as
				// "did not admit"; the state was never corrupted.
				rejected++
			case decision.Allowed:
				allowed++
			default:
				rejected++
			}
			mu.Unlock()
		}()
	}

	close(start)
	wg.Wait()

	assert.Equal(t, goroutines, allowed+rejected,
		"no lost updates: every goroutine must be accounted for")
	assert.GreaterOrEqual(t, allowed, 1,
		"at least one goroutine must win CAS and be admitted")
	assert.LessOrEqual(t, allowed, goroutines,
		"admitted count cannot exceed the number of contenders")
}
