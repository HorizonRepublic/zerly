/**
 * Envelope returned by a Nest handler back to `zerly-gateway-server` over
 * Core NATS request/reply.
 * @template TBody - Type of the response body. Defaults to `unknown`.
 * @remarks
 * Application code never constructs this directly. The SDK's
 * `GatewayResponseInterceptor` wraps successful handler returns into this
 * shape, and the `GatewayExceptionFilter` wraps thrown errors. A single
 * wire format serves both success and error paths — the gateway does not
 * distinguish between them, it simply forwards `status`, `headers`, and
 * `body` verbatim to the HTTP client.
 */
export interface IGatewayReply<TBody = unknown> {
  /**
   * HTTP status code to return.
   * @remarks
   * Always present. The gateway writes this value verbatim to the HTTP
   * response status line without further interpretation. On the success path
   * the SDK's `GatewayResponseInterceptor` fills it from
   * `IApiGatewayOptions.statusCode` (or the null-return default); on the
   * error path the `GatewayExceptionFilter` fills it from
   * `DomainException.status` or the generic `500` fallback. Downstream
   * `@HttpCode()` decorators are not consulted — this field is authoritative.
   */
  readonly status: number;

  /**
   * HTTP headers to include in the response. Always present; may be empty.
   * @remarks
   * The gateway merges these over its own defaults (`Content-Type`,
   * `X-Request-Id`). Headers set here override gateway defaults, except
   * `X-Request-Id` which the gateway always owns.
   */
  readonly headers: Readonly<Record<string, string>>;

  /** Response body. `null` for void/204 responses. JSON-serializable. */
  readonly body: TBody | null;
}
