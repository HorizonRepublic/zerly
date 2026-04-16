/**
 * CORS policy for a gateway-exposed endpoint.
 * Written to the `handler_registry` KV bucket as the `cors` field.
 * Go gateway reads this and handles OPTIONS preflight + response
 * headers without a NATS round-trip.
 */
export interface IGatewayCorsConfig {
  /** Allowed origins. Wildcard `'*'` matches all. */
  readonly origins: readonly string[];

  /**
   * Allowed HTTP methods for preflight.
   * Default: the route's own method + all other methods registered
   * on the same path (resolved by Go at table build time).
   */
  readonly methods?: readonly string[];

  /**
   * Allowed request headers for preflight.
   * Default: `['Content-Type', 'Authorization', 'X-Request-Id']`.
   */
  readonly headers?: readonly string[];

  /** Whether the browser should send credentials (cookies, auth headers). */
  readonly credentials?: boolean;

  /** How long (seconds) the browser caches a preflight response. */
  readonly maxAge?: number;
}
