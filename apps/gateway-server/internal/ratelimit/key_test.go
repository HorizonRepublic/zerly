package ratelimit_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/ratelimit"
)

func TestBuildBucketKey_Schema(t *testing.T) {
	k := ratelimit.BuildBucketKey("GET", "/users/:id", "192.0.2.1")
	parts := strings.Split(k, ".")
	assert.Len(t, parts, 3)
	assert.Equal(t, "GET", parts[0])
	assert.Regexp(t, `^[a-z2-7]{13}$`, parts[1])
	assert.Regexp(t, `^[a-z2-7]{13}$`, parts[2])
}

func TestBuildBucketKey_NATSKVSafe(t *testing.T) {
	// Input contains characters NATS KV would reject directly.
	k := ratelimit.BuildBucketKey("POST", "/auth:login/v1", "header:x-api-key=abc 123")
	for _, forbidden := range []byte{':', ' ', '>', '*', '/'} {
		assert.NotContains(t, k, string(forbidden))
	}
}

func noHeader(_ string) string { return "" }
func noCookie(_ string) string { return "" }

func TestResolveKey_IPAlwaysResolves(t *testing.T) {
	key := ratelimit.ResolveKey([]string{"ip"}, "1.2.3.4", noHeader, noCookie, nil)

	assert.Equal(t, "1.2.3.4", key)
}

func TestResolveKey_HeaderResolvesWhenPresent(t *testing.T) {
	headerFn := func(name string) string {
		if name == "x-api-key" {
			return "my-key"
		}

		return ""
	}

	key := ratelimit.ResolveKey([]string{"header:x-api-key", "ip"}, "1.2.3.4", headerFn, noCookie, nil)

	assert.Equal(t, "my-key", key)
}

func TestResolveKey_CookieResolvesWhenPresent(t *testing.T) {
	cookieFn := func(name string) string {
		if name == "session" {
			return "sess-abc"
		}

		return ""
	}

	key := ratelimit.ResolveKey([]string{"cookie:session", "ip"}, "1.2.3.4", noHeader, cookieFn, nil)

	assert.Equal(t, "sess-abc", key)
}

func TestResolveKey_UserFieldResolvesFromClaims(t *testing.T) {
	claims := map[string]any{"id": "user-123"}

	key := ratelimit.ResolveKey([]string{"user:id", "ip"}, "1.2.3.4", noHeader, noCookie, claims)

	assert.Equal(t, "user-123", key)
}

func TestResolveKey_FallsBackToIPWhenNothingResolves(t *testing.T) {
	key := ratelimit.ResolveKey([]string{"user:id", "header:x-api-key"}, "1.2.3.4", noHeader, noCookie, nil)

	assert.Equal(t, "1.2.3.4", key)
}

func TestResolveKey_PriorityChainStopsAtFirstMatch(t *testing.T) {
	headerFn := func(name string) string {
		if name == "x-api-key" {
			return "api-key-val"
		}

		return ""
	}
	claims := map[string]any{"id": "user-123"}

	// user:id should win because it comes first in the chain
	key := ratelimit.ResolveKey([]string{"user:id", "header:x-api-key", "ip"}, "1.2.3.4", headerFn, noCookie, claims)

	assert.Equal(t, "user-123", key)
}
