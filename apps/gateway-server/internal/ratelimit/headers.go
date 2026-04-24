package ratelimit

import (
	"math"
	"strconv"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

// BuildHeaders returns the X-RateLimit-* response headers for a given
// decision, plus Retry-After on rejection.
//
// Always emits (on both allow and reject):
//   - X-RateLimit-Limit     = configured rps
//   - X-RateLimit-Remaining = decision.Remaining
//   - X-RateLimit-Reset     = decision.ResetAt as Unix seconds
//
// On rejection (decision.Allowed == false) additionally emits:
//   - Retry-After = ceil(decision.RetryAfter seconds), clamped to a
//     minimum of 1. Retry-After: 0 is misleading to clients because
//     many client libraries treat it as "retry immediately"; a
//     fractional sub-second wait always rounds up to a full second.
//
// Keys use the canonical casing clients (GitHub, Stripe, etc.)
// expect. The returned map is fresh and safe to mutate / merge into
// any http.Header-compatible collection.
func BuildHeaders(rl *registry.RateLimitMeta, d Decision) map[string]string {
	h := map[string]string{
		"X-RateLimit-Limit":     strconv.Itoa(rl.RPS),
		"X-RateLimit-Remaining": strconv.Itoa(d.Remaining),
		"X-RateLimit-Reset":     strconv.FormatInt(d.ResetAt.Unix(), 10),
	}
	if !d.Allowed {
		secs := int64(math.Ceil(d.RetryAfter.Seconds()))
		if secs < 1 {
			secs = 1
		}
		h["Retry-After"] = strconv.FormatInt(secs, 10)
	}
	return h
}
