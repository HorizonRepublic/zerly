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

// TestResolveKey_MapClaimIsDeterministic pins the fix for the
// fmt.Sprint nondeterminism: when a JWT claim is a map, the rendered
// rate-limit key MUST be stable across calls because Go map iteration
// order is randomized. Without deterministic encoding, the same user
// would land in different buckets across goroutines/pods and the
// configured rate would dilute or collide.
func TestResolveKey_MapClaimIsDeterministic(t *testing.T) {
	claims := map[string]any{
		"meta": map[string]any{
			"a": 1,
			"b": 2,
			"c": "three",
			"d": true,
		},
	}

	first := ratelimit.ResolveKey([]string{"user:meta"}, "1.2.3.4", noHeader, noCookie, claims)
	for i := 0; i < 100; i++ {
		got := ratelimit.ResolveKey([]string{"user:meta"}, "1.2.3.4", noHeader, noCookie, claims)
		assert.Equalf(t, first, got, "ResolveKey must be deterministic across runs (iteration %d)", i)
	}
}

// TestResolveKey_SliceClaimIsDeterministic mirrors the map case for
// array-shaped claims. JSON marshalling preserves slice order, but
// the assertion guards against a future regression that swaps in a
// formatter with non-deterministic output.
func TestResolveKey_SliceClaimIsDeterministic(t *testing.T) {
	claims := map[string]any{
		"roles": []any{"admin", "ops", "support"},
	}

	first := ratelimit.ResolveKey([]string{"user:roles"}, "1.2.3.4", noHeader, noCookie, claims)
	for i := 0; i < 50; i++ {
		got := ratelimit.ResolveKey([]string{"user:roles"}, "1.2.3.4", noHeader, noCookie, claims)
		assert.Equalf(t, first, got, "slice claim must render deterministically (iteration %d)", i)
	}
}

// TestResolveKey_ScalarClaimsRenderConsistently verifies the
// fast-path branches for primitive types still produce the wire shape
// callers expect — strings unchanged, numbers via fmt.Sprint.
func TestResolveKey_ScalarClaimsRenderConsistently(t *testing.T) {
	cases := []struct {
		name  string
		claim any
		want  string
	}{
		{"string", "user-123", "user-123"},
		{"int", 42, "42"},
		{"int64", int64(9000), "9000"},
		{"float", 3.5, "3.5"},
		{"bool", true, "true"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			claims := map[string]any{"id": c.claim}
			got := ratelimit.ResolveKey([]string{"user:id"}, "1.2.3.4", noHeader, noCookie, claims)
			assert.Equal(t, c.want, got)
		})
	}
}
