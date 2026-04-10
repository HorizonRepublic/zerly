/**
 * Standard JSON shape returned inside `IGatewayReply.body` when a handler
 * throws. Modeled after RFC 7807 (`application/problem+json`), simplified.
 * @remarks
 * Built by `IErrorBodyFactory` implementations — the default impl recognizes
 * the `isDomainException: true` marker from `@zerly/errors` and extracts
 * structured fields. Generic throws fall back to a 500 shape with minimal
 * information (full error logged server-side only).
 */
export interface IGatewayErrorBody {
  /**
   * Stable machine-readable error code, e.g. `USER_NOT_FOUND`.
   * @remarks
   * Sourced from `DomainException.code` when available. For unknown throws
   * falls back to `INTERNAL_SERVER_ERROR`.
   */
  readonly error: string;

  /**
   * Human-readable error description.
   * @remarks
   * Sourced from `DomainException.message` when available; otherwise a
   * sanitized generic string ("An unexpected error occurred"). Safe to
   * render directly in a client UI — unlike `stack`, it never leaks
   * implementation details. Clients may use `error` as an i18n key and fall
   * back to this field when no localized message exists.
   */
  readonly message: string;

  /**
   * Echo of `IGatewayRequest.meta.requestId` for log correlation by the client.
   * @remarks
   * Clients should include this value when reporting bugs — it is the key
   * that lets operators find the exact request path across all services.
   */
  readonly requestId: string;

  /** Optional structured context from `DomainException.details`. Domain-specific. */
  readonly details?: Readonly<Record<string, unknown>>;

  /**
   * Stack trace. Only populated in non-production environments.
   * @remarks
   * Controlled by `GatewayModuleOptions.isProduction` passed to `forRoot`.
   * Never expose stack traces in production HTTP responses.
   */
  readonly stack?: string;
}
