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

	counters struct {
		allowed  atomic.Int64
		rejected atomic.Int64
	}
}

type memoryEntry struct {
	tat      atomic.Int64 // Unix nanoseconds
	lastSeen atomic.Int64 // Unix nanoseconds
}

// NewMemoryStore constructs a MemoryStore with the given stale-key
// TTL. A background sweeper removes entries whose lastSeen is
// older than ttl every ttl/10. Close() stops the sweeper.
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
// retry so the late arrival sees the updated state. ctx is
// accepted for interface symmetry with NATSKVStore but not
// consulted: every branch is pure CPU and returns in microseconds.
func (s *MemoryStore) Allow(ctx context.Context, key string, rps, burst int) (Decision, error) {
	_ = ctx
	now := time.Now()
	v, _ := s.entries.LoadOrStore(key, &memoryEntry{})
	e := v.(*memoryEntry)
	e.lastSeen.Store(now.UnixNano())

	for {
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

// Close is idempotent.
func (s *MemoryStore) Close() error {
	select {
	case <-s.stop:
		// already closed
	default:
		close(s.stop)
	}
	return nil
}

// Counters returns a snapshot of internal counters for future
// OpenTelemetry plumbing. Each value is read atomically so callers
// see a consistent point-in-time view.
func (s *MemoryStore) Counters() map[string]int64 {
	return map[string]int64{
		"ratelimit_memory_decisions_allowed":  s.counters.allowed.Load(),
		"ratelimit_memory_decisions_rejected": s.counters.rejected.Load(),
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
