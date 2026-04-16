package ratelimit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/ratelimit"
)

func TestMemoryStore_Allow(t *testing.T) {
	store := ratelimit.NewMemoryStore()
	defer store.Stop()

	assert.True(t, store.Allow("key-a", 1, 1), "first request should pass")
	assert.False(t, store.Allow("key-a", 1, 1), "second immediate request should be rejected")
	assert.True(t, store.Allow("key-b", 1, 1), "different key should be independent")
}

func TestMemoryStore_BurstAllowsSpike(t *testing.T) {
	store := ratelimit.NewMemoryStore()
	defer store.Stop()

	for i := 0; i < 5; i++ {
		assert.True(t, store.Allow("burst", 1, 5), "request %d in burst window should pass", i)
	}

	assert.False(t, store.Allow("burst", 1, 5), "request after burst exhausted should be rejected")
}

func TestMemoryStore_FlushPrefix(t *testing.T) {
	store := ratelimit.NewMemoryStore()
	defer store.Stop()

	store.Allow("POST:/users:1.2.3.4", 1, 1)
	store.Allow("POST:/users:5.6.7.8", 1, 1)
	store.Allow("GET:/other:1.2.3.4", 1, 1)

	store.FlushPrefix("POST:/users:")

	// Flushed keys should be like new — allow again
	assert.True(t, store.Allow("POST:/users:1.2.3.4", 1, 1))
	assert.True(t, store.Allow("POST:/users:5.6.7.8", 1, 1))

	// Unflushed key still rate-limited
	assert.False(t, store.Allow("GET:/other:1.2.3.4", 1, 1))
}
