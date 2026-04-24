package ratelimit

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryStore is an in-process GCRA rate limiter. Each bucket's
// state is a single atomic int64 holding TAT (Theoretical Arrival
// Time) in Unix nanoseconds.
//
// Semantically identical to NATSKVStore — switching between them
// produces the same decisions for the same (key, rps, burst).
//
// Trade-off vs NATSKVStore: no cross-replica sharing. Each pod
// tracks its own buckets. Use for single-instance deployments or
// hot-path routes where network-store latency is unacceptable.
type MemoryStore struct {
	entries sync.Map // map[string]*memoryEntry
	ttl     time.Duration
	stop    chan struct{}
	// closeOnce serializes Close so concurrent shutdown callers cannot
	// race the select/close sequence and panic on a double close. The
	// sync.Once token is consumed on the first call regardless of
	// success, which matches the documented "idempotent" contract.
	closeOnce sync.Once

	counters struct {
		allowed  atomic.Int64
		rejected atomic.Int64
	}
}

type memoryEntry struct {
	tat      atomic.Int64 // Unix nanoseconds
	lastSeen atomic.Int64 // Unix nanoseconds
}

// NewMemoryStore constructs a MemoryStore with the given idle-key
// TTL. A background sweeper removes entries whose lastSeen is older
// than ttl every ttl/10. Close() stops the sweeper.
//
// TTL semantics (Memory): the TTL is an idle-entry sweep interval.
// An entry is reaped only after no Allow call has touched it for
// ttl. A continuously-active key persists indefinitely; the GCRA
// state stays valid for as long as the key is in use. This differs
// from NATSKVStore, which interprets the same configuration value as
// a hard MaxAge: every key is reaped after ttl regardless of activity.
// Operators wiring RATELIMIT_KEY_TTL must understand the divergence
// when comparing per-bucket lifetime across a backend swap.
//
// Example:
//
//	store := NewMemoryStore(24 * time.Hour)
//	defer store.Close()
//	decision, err := store.Allow(ctx, "user:1234", 100, 10)
func NewMemoryStore(ttl time.Duration) *MemoryStore {
	s := &MemoryStore{ttl: ttl, stop: make(chan struct{})}
	go s.sweep()
	return s
}

// Allow implements Store by running GCRA against an in-memory TAT.
//
// The atomic CAS loop is the hot path: LoadOrStore the entry, load
// the current TAT, compute the decision via Check, and either
// return (rejection path) or CAS the new TAT into place. A lost
// CAS means another goroutine advanced the TAT for the same key —
// retry so the late arrival sees the updated state.
//
// ctx is consulted at the top of the loop so a cancelled or
// deadline-exceeded request surfaces ctx.Err() rather than producing
// a side-effecting decision. The check is a single atomic load
// against ctx.Done(), negligible against the rest of the loop, and
// keeps Memory and NATS-KV aligned for callers wiring shared upstream
// timeout chains.
func (s *MemoryStore) Allow(ctx context.Context, key string, rps, burst int) (Decision, error) {
	now := time.Now()
	v, _ := s.entries.LoadOrStore(key, &memoryEntry{})
	e := v.(*memoryEntry)
	e.lastSeen.Store(now.UnixNano())

	for {
		if err := ctx.Err(); err != nil {
			return Decision{}, err
		}

		currentNs := e.tat.Load()
		currentTAT := time.Unix(0, currentNs)
		decision, newTAT := Check(currentTAT, now, rps, burst)

		if !decision.Allowed {
			s.counters.rejected.Add(1)
			return decision, nil
		}
		if e.tat.CompareAndSwap(currentNs, newTAT.UnixNano()) {
			s.counters.allowed.Add(1)
			return decision, nil
		}
		// CAS failed (another goroutine won); retry.
	}
}

// FlushPrefix removes all entries whose key begins with prefix.
func (s *MemoryStore) FlushPrefix(_ context.Context, prefix string) error {
	s.entries.Range(func(k, _ any) bool {
		if ks, ok := k.(string); ok && strings.HasPrefix(ks, prefix) {
			s.entries.Delete(k)
		}
		return true
	})
	return nil
}

// Close is idempotent and safe to call from multiple goroutines.
// sync.Once guards the channel close so a concurrent second invocation
// observes the consumed token and returns without re-closing the
// channel — racing two close(stop) calls would panic the runtime.
func (s *MemoryStore) Close() error {
	s.closeOnce.Do(func() {
		close(s.stop)
	})
	return nil
}

// Counters returns a snapshot of internal counters for OpenTelemetry
// plumbing. Each value is read atomically so callers see a consistent
// point-in-time view.
//
// MemoryStore has no remote dependencies, so backend_errors is
// hard-wired to 0. The key still ships in the snapshot so the
// minimum schema declared on Store.Counters is satisfied — dashboards
// graphing backend_errors across the deployment do not go dark on a
// memory-only pod.
func (s *MemoryStore) Counters() map[string]int64 {
	return map[string]int64{
		"ratelimit_memory_decisions_allowed":  s.counters.allowed.Load(),
		"ratelimit_memory_decisions_rejected": s.counters.rejected.Load(),
		"ratelimit_memory_backend_errors":     0,
	}
}

func (s *MemoryStore) sweep() {
	interval := s.ttl / 10
	if interval < time.Second {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-t.C:
			cutoff := now.Add(-s.ttl).UnixNano()
			s.entries.Range(func(k, v any) bool {
				if e, ok := v.(*memoryEntry); ok && e.lastSeen.Load() < cutoff {
					s.entries.Delete(k)
				}
				return true
			})
		}
	}
}
