// Package ratelimit provides a rate-limiting Store interface with an
// in-memory token-bucket implementation. The interface is designed
// for future Redis-compatible backends without changing consumer code.
package ratelimit

// Store is the rate-limiter backend interface.
type Store interface {
	// Allow checks whether a request identified by key should be
	// permitted under the given rate (rps) and burst limit.
	Allow(key string, rps int, burst int) bool
}
