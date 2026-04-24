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
 * Selects the rate-limit backend for a route.
 *
 * - `'memory'` (default): in-process GCRA rate limiter. Zero
 *   latency, zero infrastructure. Each gateway replica tracks its
 *   own counters → multi-replica deployments effectively allow N×
 *   the configured rate. Appropriate for single-instance
 *   deployments or hot-path routes (health checks) where
 *   network-store latency is unacceptable.
 *
 * - `'nats-kv'`: distributed GCRA state in a NATS JetStream KV
 *   bucket. All gateway replicas share counters → correct rate
 *   enforcement regardless of replica count. Reuses the existing
 *   NATS cluster (zero extra infrastructure). Documented ceiling:
 *   ~5k req/s across all rate-limited routes.
 *
 * - `'redis'`: **declared in the SDK contract; not yet implemented
 *   in the Go gateway.** Using `'redis'` today logs a warning on
 *   startup and falls back to `'memory'` for the affected route.
 *   Full Redis support is a planned future addition.
 * @remarks Store selection is applied per-route. A gateway process
 * can serve `'memory'`-backed and `'nats-kv'`-backed routes
 * simultaneously — each route's `store` field independently
 * selects its backend.
 * @default `'memory'`
 */
export type RateLimitStore = 'memory' | 'nats-kv' | 'redis';

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

  /**
   * Backend store selector. See {@link RateLimitStore} for the
   * semantics of each value and the per-route coexistence model.
   */
  readonly store?: RateLimitStore;
}
