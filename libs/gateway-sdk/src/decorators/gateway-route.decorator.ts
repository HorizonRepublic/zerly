import { applyDecorators, UseFilters, UseInterceptors } from '@nestjs/common';
import { MessagePattern } from '@nestjs/microservices';

import { GatewayExceptionFilter } from '../filters/gateway-exception.filter';
import { GatewayResponseInterceptor } from '../interceptors/gateway-response.interceptor';

import type { IGatewayHttpMeta } from '../types/gateway-http-meta.interface';
import type { IGatewayRouteOptions } from '../types/gateway-route-options.interface';

/**
 * Exposes a NATS message handler as an HTTP endpoint via `zerly-gateway-server`.
 * @param options - Routing metadata for the handler.
 * @remarks
 * Composes three separate decorations in one call:
 *
 *   1. `@MessagePattern(pattern, { meta: { http } })` — registers the handler
 *      with `nestjs-jetstream` and writes HTTP routing metadata to the
 *      `handler_registry` NATS KV bucket via the existing `extras.meta`
 *      passthrough.
 *
 *   2. `@UseInterceptors(GatewayResponseInterceptor)` — locally attaches the
 *      success-path interceptor, so it fires only for gateway-exposed
 *      handlers with no runtime "is this a gateway handler?" check.
 *
 *   3. `@UseFilters(GatewayExceptionFilter)` — locally attaches the
 *      error-path filter. Because filters run after NestJS pipes and guards,
 *      validation errors from pipes are correctly serialized into structured
 *      HTTP responses rather than surfacing as raw 500s on the client side.
 *
 * The handler remains callable as a pure RPC from other Nest services via
 * the same pattern — the HTTP exposure is additive, not exclusive.
 *
 * The `statusCode` field is spread conditionally because the workspace
 * enables TypeScript's `exactOptionalPropertyTypes`: assigning a possibly
 * `undefined` value to an optional key is rejected, so the ternary
 * explicitly omits the key when no override was provided.
 * @example
 * ```ts
 * @Controller()
 * export class UsersController {
 *   @GatewayRoute({
 *     pattern: 'users.create',
 *     method: 'POST',
 *     path: '/users',
 *     statusCode: 201,
 *   })
 *   createUser(@GatewayBody() dto: CreateUserDto) {
 *     return this.usersService.create(dto);
 *   }
 * }
 * ```
 */
export const GatewayRoute = (options: IGatewayRouteOptions): MethodDecorator => {
  const http: IGatewayHttpMeta =
    options.statusCode === undefined
      ? { method: options.method, path: options.path }
      : {
          method: options.method,
          path: options.path,
          statusCode: options.statusCode,
        };

  return applyDecorators(
    MessagePattern(options.pattern, { meta: { http } }),
    UseInterceptors(GatewayResponseInterceptor),
    UseFilters(GatewayExceptionFilter),
  );
};
