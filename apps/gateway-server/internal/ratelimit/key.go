package ratelimit

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cespare/xxhash/v2"
)

// base32Alphabet is the lowercase, unpadded, NATS-KV-safe base32
// alphabet used by encodeBase32. Chosen over stdlib encoding/base32
// because stdlib's alphabet mixes case and appends '=' padding, both
// of which would collide with NATS KV key charset restrictions.
const base32Alphabet = "abcdefghijklmnopqrstuvwxyz234567"

// hashKey returns a fixed-length 13-character lowercase base32 digest
// of input, backed by xxHash64.
//
// The hash is non-cryptographic and used purely to compress arbitrary
// user-supplied identifiers (path templates, IPs, header values,
// cookie values, JWT claim fragments) into a uniform, NATS-KV-safe
// token. xxHash64 is collision-safe at the cardinalities expected
// for rate-limit buckets (~10k-100k active keys; 64-bit birthday
// bound is ~4 billion keys) and ~5x faster than SHA-256 on the hot
// path — cryptographic strength is irrelevant because the output
// never travels to a trust boundary.
func hashKey(input string) string {
	return encodeBase32(xxhash.Sum64String(input))
}

// encodeBase32 renders a uint64 as exactly 13 lowercase-base32
// characters without padding (ceil(64/5) = 13). Fills the buffer
// right-to-left because each iteration captures the low 5 bits of h
// before shifting.
func encodeBase32(h uint64) string {
	var buf [13]byte
	for i := 12; i >= 0; i-- {
		buf[i] = base32Alphabet[h&0x1f]
		h >>= 5
	}

	return string(buf[:])
}

// BuildBucketKey composes a NATS-KV-safe rate-limit bucket key using
// the canonical schema shared by every Store backend:
//
//	{method}.{hashKey(pathTemplate)}.{hashKey(resolvedKey)}
//
// Both MemoryStore and NATSKVStore use this identical schema so that
// switching the store backend preserves bucket identity across a
// migration — a user rate-limited on one backend remains rate-limited
// after a hot-swap without losing their TAT. The charset is the
// lowercase base32 alphabet plus '.' as separator — all NATS KV key
// constraints (no ':', no whitespace, no wildcard characters) are
// satisfied by construction.
//
// method is emitted verbatim (e.g. "GET", "POST") — HTTP method names
// are already NATS-KV-safe. Both pathTemplate and resolvedKey are
// hashed so arbitrary user-supplied characters (':', '/', ' ', '>',
// '*', etc.) never reach the key.
func BuildBucketKey(method, pathTemplate, resolvedKey string) string {
	return method + "." + hashKey(pathTemplate) + "." + hashKey(resolvedKey)
}

// ResolveKey walks the keyBy chain and returns the first resolved value.
// Falls back to clientIP if nothing resolves.
//
// headerFn and cookieFn are injected to decouple from any HTTP framework.
func ResolveKey(
	keyBy []string,
	clientIP string,
	headerFn func(name string) string,
	cookieFn func(name string) string,
	claims map[string]any,
) string {
	for _, key := range keyBy {
		switch {
		case key == "ip":
			return clientIP

		case strings.HasPrefix(key, "header:"):
			if v := headerFn(key[7:]); v != "" {
				return v
			}

		case strings.HasPrefix(key, "cookie:"):
			if v := cookieFn(key[7:]); v != "" {
				return v
			}

		case strings.HasPrefix(key, "user:"):
			if claims != nil {
				field := key[5:]
				if v, ok := claims[field]; ok {
					return stringifyClaim(v)
				}
			}
		}
	}

	return clientIP
}

// stringifyClaim renders a JWT claim value into a deterministic
// rate-limit key fragment.
//
// fmt.Sprint on a map or slice walks the runtime's randomized map
// iteration order, so the same claim payload would otherwise produce
// different rate-limit keys across goroutines and pods — buckets
// would dilute or collide instead of converging on the configured
// rate. JSON marshalling sorts map keys lexicographically (per
// encoding/json since Go 1.12), so json.Marshal is the canonical way
// to derive a stable representation for object/array claims.
//
// Scalar primitives go through fmt.Sprint directly because their wire
// form is already deterministic. The json.Marshal failure branch
// falls back to fmt.Sprintf so a malformed claim still produces some
// key — the worst case is non-determinism on a value that already
// failed to encode, which is no worse than the prior behavior.
func stringifyClaim(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(val)
	}

	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}

	return string(encoded)
}
