import type { IGatewayRequestMeta } from './gateway-request-meta.interface';
import type { IGatewayRouteContext } from './gateway-route-context.interface';

/**
 * Envelope payload sent by `zerly-gateway-server` to a Nest handler over
 * Core NATS request/reply.
 * @template TBody - Type of the parsed HTTP body. Defaults to `unknown` for
 *                   safety; narrow via the handler signature or param decorators.
 * @remarks
 * Application code typically does not touch this type directly — param
 * decorators (`@GatewayBody`, `@GatewayParam`, etc.) extract the fields
 * individually. Use `@Payload()` with this type when you need the raw envelope.
 */
export interface IGatewayRequest<TBody = unknown> {
  /** Routing context describing which registered route matched this request. */
  readonly route: IGatewayRouteContext;

  /** Path parameters extracted from `:placeholders` in the route template. */
  readonly params: Readonly<Record<string, string>>;

  /**
   * Parsed query string. Repeated keys arrive as `string[]`; single-value
   * keys arrive as `string`. Handlers should `Array.isArray` to discriminate.
   */
  readonly query: Readonly<Record<string, string | readonly string[]>>;

  /**
   * Request headers, lowercased. Multi-value headers are joined into a single
   * string in MVP; `headersRaw` will be added as a non-breaking extension when
   * multi-value support lands.
   */
  readonly headers: Readonly<Record<string, string>>;

  /** Parsed JSON body, or `null` for empty requests. */
  readonly body: TBody;

  /** Gateway-generated metadata: request id, trace context, deadlines. */
  readonly meta: IGatewayRequestMeta;
}
