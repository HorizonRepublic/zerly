import type { IErrorBodyBuildResult } from './error-body-build-result.interface';
import type { IGatewayRequest } from '../../types/gateway-request.interface';

/**
 * Contract for translating an arbitrary thrown value into a structured
 * `IGatewayErrorBody` with an HTTP status.
 * @remarks
 * The default implementation (`DefaultErrorBodyFactory`) recognizes
 * the `@zerly/errors` `DomainException` marker
 * (`isDomainException: true`) via duck-typing and extracts `code`,
 * `message`, `details`, and optionally `stack` (dev only). For unknown
 * throws it falls back to a generic `500` shape with the real error
 * logged server-side only, so upstream clients never see internal
 * diagnostics.
 *
 * Custom implementations may integrate with non-Zerly error libraries
 * (e.g., `http-errors`, Problem Details RFC 7807), apply project-specific
 * error-code mapping, or redact sensitive fields from third-party errors
 * before they reach the wire. Override by binding a custom class to the
 * `GATEWAY_ERROR_BODY_FACTORY` token from
 * `../../tokens/gateway-tokens.constant`.
 *
 * This contract is deliberately decoupled from `@zerly/errors` itself —
 * implementations must **not** hard-import `DomainException` as a type
 * or class. Duck-typing on the marker field keeps `@zerly/gateway-sdk`
 * installable without `@zerly/errors` as a hard dependency, which is
 * important for consumers that roll their own error hierarchies.
 */
export interface IErrorBodyFactory {
  /**
   * Translate a thrown value into a `{ status, body }` pair.
   * @param error - The value thrown by the handler. Typed as `unknown`
   *                because JavaScript allows any value to be thrown —
   *                strings, numbers, plain objects, or `null` — and the
   *                factory is the layer responsible for normalizing all
   *                of them into a structured body.
   * @param request - The inbound request envelope, used primarily to
   *                  populate `IGatewayErrorBody.requestId` for log
   *                  correlation between gateway logs, handler logs, and
   *                  downstream observability tooling.
   */
  build(error: unknown, request: IGatewayRequest): IErrorBodyBuildResult;
}
