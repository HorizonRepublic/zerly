package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// MemoryStore is a token-bucket rate limiter backed by sync.Map.
// Keys are scoped per route+client and auto-evicted after 60 seconds
// of inactivity to prevent unbounded memory growth.
type MemoryStore struct {
	limiters sync.Map
	stop     chan struct{}
}

// NewMemoryStore creates a store and starts its cleanup goroutine.
func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{stop: make(chan struct{})}
	go s.cleanup()

	return s
}

// Allow implements Store.
func (s *MemoryStore) Allow(key string, rps int, burst int) bool {
	now := time.Now()

	v, loaded := s.limiters.Load(key)
	if loaded {
		e := v.(*entry)
		e.lastSeen = now

		return e.limiter.Allow()
	}

	e := &entry{
		limiter:  rate.NewLimiter(rate.Limit(rps), burst),
		lastSeen: now,
	}

	actual, _ := s.limiters.LoadOrStore(key, e)

	return actual.(*entry).limiter.Allow()
}

// FlushPrefix removes all entries whose key starts with prefix.
// Called when a route's rate-limit config changes via KV watcher.
func (s *MemoryStore) FlushPrefix(prefix string) {
	s.limiters.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			s.limiters.Delete(key)
		}

		return true
	})
}

// Stop terminates the cleanup goroutine.
func (s *MemoryStore) Stop() {
	close(s.stop)
}

func (s *MemoryStore) cleanup() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.limiters.Range(func(key, value any) bool {
				if now.Sub(value.(*entry).lastSeen) > 60*time.Second {
					s.limiters.Delete(key)
				}

				return true
			})
		}
	}
}
