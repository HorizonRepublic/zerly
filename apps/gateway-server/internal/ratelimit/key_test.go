package ratelimit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/ratelimit"
)

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
