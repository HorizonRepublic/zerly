/**
 * Key source for rate-limit bucket resolution.
 *
 * Go gateway walks the `keyBy` array in order and uses the first
 * value that resolves. Implicit fallback to client IP if nothing
 * in the chain matches.
 *
 * - `'ip'` — trusted-proxy-resolved client IP (always resolves).
 * - `'header:<name>'` — request header value.
 * - `'cookie:<name>'` — request cookie value.
 * - `'user:<field>'` — field from auth claims (requires auth to have succeeded).
 */
export type RateLimitKey = 'ip' | `header:${string}` | `cookie:${string}` | `user:${string}`;

/**
 * Per-route rate limiting policy.
 * Written to the `handler_registry` KV bucket as the `rateLimit` field.
 * Go gateway enforces via a token-bucket algorithm.
 */
export interface IGatewayRateLimitConfig {
  /** Maximum sustained requests per second. */
  readonly rps: number;

  /**
   * Token bucket burst size — how many requests are allowed in a
   * short spike before the sustained rate kicks in.
   * Default: `rps * 2`.
   */
  readonly burst?: number;

  /**
   * Priority chain for resolving the rate-limit bucket key.
   * Go walks the array top-to-bottom; first value that resolves wins.
   * If nothing resolves, falls back to client IP.
   * Default: `['ip']`.
   */
  readonly keyBy?: readonly RateLimitKey[];
}
