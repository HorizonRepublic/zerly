import { Controller, NotFoundException } from '@nestjs/common';

import {
  GatewayRoute,
  GatewayBody,
  GatewayParam,
  GatewayQuery,
  GatewayRequestId,
} from '@zerly/gateway-sdk';

import { GatewayDemoService, type IDemoUser } from './gateway-demo.service';

/**
 * Request body accepted by `POST /demo/users`. Declared locally because the
 * shape is specific to this demo endpoint.
 */
interface ICreateDemoUserDto {
  readonly name: string;
}

/**
 * Success response for `POST /demo/users`. Extends the base user with the
 * gateway-generated request ID for correlation in logs — demonstrates how
 * handlers can weave observability data from `@GatewayRequestId` into
 * domain responses.
 */
interface IDemoUserWithRequestId extends IDemoUser {
  readonly requestId: string;
}

/**
 * Demonstrates the three most common `@GatewayRoute` patterns.
 * @remarks
 * 1. `GET /demo/users/:id` — path param extraction via `@GatewayParam`,
 *    throws a plain `Error` on miss so the `DefaultErrorBodyFactory`
 *    serializes a generic 500 (the DomainException path is covered by the
 *    gateway-sdk integration test, not here).
 *
 * 2. `POST /demo/users` — body extraction via `@GatewayBody`, explicit
 *    `statusCode: 201`, and request-ID echo demonstrating the
 *    `@GatewayRequestId` sugar decorator.
 *
 * 3. `DELETE /demo/users/:id` — void return, exercising the 204 default
 *    from `DefaultStatusResolver`.
 *
 * None of these handlers are connected to a live NATS transport by the
 * example-app bootstrap — that is deferred until the E2E milestone. At
 * compile time they prove that `@GatewayRoute` composes correctly with a
 * real NestJS DI-managed controller, and their metadata is retrievable
 * via `Reflector.get` (as verified end-to-end in the `gateway-sdk`
 * integration test).
 */
@Controller()
export class GatewayDemoController {
  public constructor(private readonly service: GatewayDemoService) {}

  /**
   * Fetch a single user by ID.
   * @param id - Opaque user identifier taken from the `:id` path segment.
   * @returns The matching user record.
   * @throws Error When no user exists for the supplied ID.
   */
  @GatewayRoute({
    pattern: 'users.get',
    method: 'GET',
    path: '/users/:id',
  })
  public getUser(@GatewayQuery() query: unknown, @GatewayParam('id') id: string): IDemoUser {
    console.log('QUERY', query);
    const user = this.service.findById(id);

    if (!user) {
      throw new NotFoundException(`User ${id} not found`);
    }

    return user;
  }

  /**
   * Create a new user and echo the originating gateway request ID.
   * @param dto - Body payload with the new user's display name.
   * @param requestId - Gateway-assigned request correlation ID.
   * @returns The newly created user, enriched with the request ID.
   */
  @GatewayRoute({
    pattern: 'users.create',
    method: 'POST',
    path: '/users',
    statusCode: 201,
  })
  public createUser(
    @GatewayBody() dto: ICreateDemoUserDto,
    @GatewayRequestId() requestId: string,
  ): IDemoUserWithRequestId {
    console.log('BODY', dto);
    return { ...this.service.create(dto.name), requestId };
  }

  /**
   * Delete a user. Idempotent — unknown IDs do not raise.
   * @param id - Opaque user identifier taken from the `:id` path segment.
   */
  @GatewayRoute({
    pattern: 'users.delete',
    method: 'DELETE',
    path: '/users/:id',
  })
  public deleteUser(@GatewayParam('id') id: string): void {
    this.service.delete(id);
  }
}
