/**
 * Context passed to an `IErrorReporter` so external APM backends
 * (Sentry, Datadog, etc.) can attach request-side breadcrumbs to
 * the uploaded exception.
 * @remarks
 * `AllExceptionsFilter` only handles HTTP — RPC is delegated to
 * `@zerly/gateway-sdk` and vanilla Nest — so the `type` discriminator
 * is fixed to `'http'`. Kept as a field rather than elided so the
 * shape stays a stable superset if a future filter starts reusing
 * the same reporter from a non-HTTP transport.
 */
export interface IErrorContext {
  type: 'http';
  requestId?: string;
  method?: string;
  url?: string;
}
