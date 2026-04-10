import type { HttpMethod } from './http-method.type';

/**
 * Options accepted by the `@ApiGateway` decorator.
 * @remarks
 * These options are composed into two separate metadata targets at decoration
 * time: the `pattern` is forwarded to `@MessagePattern`, while `method`,
 * `path`, and `statusCode` are written under `extras.meta.http` so that
 * `nestjs-jetstream` persists them to the `handler_registry` KV bucket.
 */
export interface IApiGatewayOptions {
  /**
   * The message pattern — same semantics as `@MessagePattern(pattern)`.
   * @remarks
   * Determines the NATS subject the handler subscribes to, using the
   * `nestjs-jetstream` convention: `{service}__microservice.cmd.{pattern}`.
   *
   * Consumers may still invoke this handler directly as an RPC from another
   * Nest service using this same pattern — the HTTP exposure is additive.
   */
  readonly pattern: string;

  /** HTTP method the gateway should accept. */
  readonly method: HttpMethod;

  /** URL path template with `:param` placeholders, e.g. `/users/:id`. */
  readonly path: string;

  /**
   * HTTP status returned on successful responses.
   * @remarks
   * When omitted, the gateway applies the default rules: `200` for non-null
   * returns, `204` for `null`/`undefined`/`void` returns. Provide an explicit
   * value (e.g. `201` for `POST /users`) when the default is wrong.
   */
  readonly statusCode?: number;
}
