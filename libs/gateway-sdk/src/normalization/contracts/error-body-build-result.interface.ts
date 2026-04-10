import type { IGatewayErrorBody } from '../../types/gateway-error-body.interface';

/**
 * Result of translating a thrown value into a gateway error payload.
 * @remarks
 * Separated into a named interface rather than an anonymous
 * `{ status, body }` tuple so it can be referenced in documentation,
 * exported as part of the public API, tested in isolation, and mocked
 * cleanly without re-declaring the shape at every call site.
 *
 * Both fields are `readonly` — once an `IErrorBodyFactory` has decided
 * on a status and body, downstream consumers (reply builder, exception
 * filter, diagnostics) must treat the result as an immutable snapshot.
 * Any adjustments require producing a new `IErrorBodyBuildResult`, which
 * keeps the audit trail clear and prevents accidental mutation between
 * the factory and the wire.
 */
export interface IErrorBodyBuildResult {
  /**
   * HTTP status code to attach to the outgoing reply envelope.
   * @remarks
   * Sourced from `DomainException.status` when the thrown value is a
   * recognized domain exception, or from the
   * `DEFAULT_STATUS_INTERNAL_ERROR` fallback for unknown throws.
   */
  readonly status: number;

  /**
   * Serialized error body that will travel inside `IGatewayReply.body`.
   * @remarks
   * Already shaped for wire transmission — no further transformation by
   * the reply builder. Sensitive fields (stack traces in production,
   * internal identifiers) must be stripped by the factory before the
   * body reaches this structure.
   */
  readonly body: IGatewayErrorBody;
}
