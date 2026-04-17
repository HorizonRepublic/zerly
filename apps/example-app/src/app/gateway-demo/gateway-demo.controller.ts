import { Controller, ImATeapotException, NotFoundException } from '@nestjs/common';

import {
  GatewayRoute,
  GatewayBody,
  GatewayParam,
  GatewayQuery,
  GatewayRequestId,
  GatewayResponse,
  GatewayUser,
} from '@zerly/gateway-sdk';

import { type IDemoAuthUser } from './auth-verifier.controller';
import { GatewayDemoService, type IDemoUser } from './gateway-demo.service';

import type { IGatewayResponse } from '@zerly/gateway-sdk';

/**
 * Request body accepted by `POST /auth/login`. A realistic login
 * DTO would carry a password or a one-time-token; this demo keeps
 * it to just a name so tests do not need to wire up a password
 * store. Real consumers pair their own DTO with their own verifier.
 */
interface IDemoLoginDto {
  readonly name: string;
}

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
  public getUser(@GatewayQuery() _query: unknown, @GatewayParam('id') id: string): IDemoUser {
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

  /**
   * Echo the authenticated caller.
   * @remarks
   * Protected with `auth: true`, which routes through the default
   * `@GatewayAuthVerifier` registered by `AuthVerifierController`.
   * The verifier rejects missing / non-`demo-*` bearer tokens with a
   * 401, flags `demo-banned` as 403, and populates `user` for
   * everything else. The handler body simply returns whatever the
   * verifier produced — demonstrating the "identity-function on the
   * wire" contract for verifier claims.
   * @param user - Verifier-produced caller identity; non-nullable on
   *               required-auth routes.
   * @returns The verifier's claims verbatim.
   */
  @GatewayRoute({
    pattern: 'users.me',
    method: 'GET',
    path: '/me',
    auth: true,
  })
  public me(@GatewayUser() user: IDemoAuthUser): IDemoAuthUser {
    return user;
  }

  /**
   * Optional-auth endpoint demonstrating enriched-if-logged-in
   * content.
   * @remarks
   * Protected with `auth: { optional: true }`. Anonymous requests
   * (no bearer token or a rejected one) flow through with
   * `user === undefined`. Requests with a valid token surface the
   * caller identity under the `viewer` field so downstream consumers
   * can distinguish authenticated and anonymous responses without
   * running a second roundtrip.
   * @param id - Opaque article identifier taken from the `:id` path
   *             segment.
   * @param user - Verifier claims when present; undefined for
   *               anonymous callers.
   * @returns A payload that reveals the caller identity only when
   *          available.
   */
  @GatewayRoute({
    pattern: 'articles.get',
    method: 'GET',
    path: '/articles/:id',
    auth: { optional: true },
  })
  public getArticle(
    @GatewayParam('id') id: string,
    @GatewayUser() user: IDemoAuthUser | undefined,
  ): { readonly id: string; readonly viewer: IDemoAuthUser | null } {
    return { id, viewer: user ?? null };
  }

  /**
   * Demo login endpoint — demonstrates `@GatewayResponse()` for
   * setting an auth cookie plus overriding the default status
   * code to 201 from inside the handler body.
   * @remarks
   * Real consumers would hash a password, look the user up in a
   * store, issue a JWT or session id, and set the resulting cookie
   * with appropriate flags (`HttpOnly`, `Secure`, `SameSite`).
   * The demo skips the crypto and returns a synthetic user whose
   * `sid` cookie value is derived directly from the submitted
   * name — the e2e tests can assert on the exact cookie bytes.
   *
   * `secure: false` is used here because example-app runs on plain
   * HTTP in local dev. Production consumers MUST set `secure: true`
   * so the cookie only traverses TLS.
   * @param dto - Body with the display name to log in as.
   * @param res - Response builder for setting the session cookie
   *              and overriding status to 201.
   * @returns The authenticated user shape, mirroring what the
   *          verifier would have returned for `demo-<name>`.
   */
  @GatewayRoute({
    pattern: 'auth.login',
    method: 'POST',
    path: '/auth/login',
  })
  public login(
    @GatewayBody() dto: IDemoLoginDto,
    @GatewayResponse() res: IGatewayResponse,
  ): IDemoAuthUser {
    res
      .cookie('sid', `demo-${dto.name}`, {
        httpOnly: true,
        secure: false,
        sameSite: 'lax',
        path: '/',
        maxAge: 3600,
      })
      .status(201);

    const roles = dto.name === 'admin' ? (['admin', 'user'] as const) : (['user'] as const);

    return {
      id: `user-${dto.name}`,
      email: `${dto.name}@example.test`,
      roles,
    };
  }

  /**
   * Demo logout endpoint — demonstrates `@GatewayResponse().clearCookie()`
   * for client-side session removal.
   * @remarks
   * `clearCookie` emits a `Set-Cookie` header with `Max-Age=0` and
   * `Expires=Thu, 01 Jan 1970 00:00:00 GMT`, matching the domain
   * and path of the original cookie. The client MUST see matching
   * `path` / `domain` for the deletion to apply — this demo used
   * `path: '/'` when setting, so the clear uses the same.
   * @param res - Response builder for emitting the delete cookie.
   * @returns A trivial success envelope.
   */
  @GatewayRoute({
    pattern: 'auth.logout',
    method: 'POST',
    path: '/auth/logout',
  })
  public logout(@GatewayResponse() res: IGatewayResponse): { readonly ok: true } {
    res.clearCookie('sid', { path: '/' });

    return { ok: true };
  }

  /**
   * Demo OAuth2 redirect — demonstrates `@GatewayResponse().redirect()`
   * for the classic 302-to-external-provider start of an OAuth2
   * Authorization Code flow.
   * @remarks
   * Real consumers would read client id / redirect uri from
   * configuration, generate a PKCE verifier + state nonce, stash
   * the nonce in a server-side store, and then redirect to the
   * provider's authorize endpoint. The demo redirects to a
   * canned URL so e2e tests can assert the exact `Location`
   * header bytes without depending on a live provider.
   *
   * The handler `return null` is intentional — redirects carry
   * no body, and null return types combine with our 302 status
   * override to produce an empty HTTP body.
   * @param res - Response builder for the redirect.
   * @returns `null` — the status + Location header carry the
   *          entire response semantically.
   */
  @GatewayRoute({
    pattern: 'auth.google.start',
    method: 'GET',
    path: '/auth/google/start',
  })
  public googleStart(@GatewayResponse() res: IGatewayResponse): null {
    res.redirect(
      'https://accounts.google.com/o/oauth2/v2/auth?client_id=demo&response_type=code&scope=openid',
      302,
    );

    return null;
  }

  /**
   * Benchmark 200 hello path. Minimal static JSON body, no
   * validation, no DB, no auth. Kept as the canonical shape for
   * the Zerly gateway's HTTP → NATS → Nest → NATS → HTTP round
   * trip — same payload a vanilla Fastify / Express direct hit
   * would return, so a harness can compare them apples-to-apples.
   * @returns Static JSON body suitable for throughput benches.
   */
  @GatewayRoute({
    pattern: 'bench.hello',
    method: 'GET',
    path: '/bench/hello',
  })
  public benchHello(): { readonly ok: true } {
    return { ok: true };
  }

  /**
   * Benchmark 418 teapot path. Throws NestJS's built-in
   * `ImATeapotException` which the shared `GatewayExceptionFilter`
   * catches, formats as an `IGatewayReply` envelope with status
   * 418, and writes to the wire. Measures the exception-path
   * throughput end-to-end: Nest's exception filter chain, the
   * gateway-sdk exception filter, the envelope serialization,
   * and the gateway-server reply decoder — all of which the
   * success path does not pay for.
   * @throws ImATeapotException Always. The stack under test is
   *         the framework's error-formatting path, not the
   *         handler body.
   */
  @GatewayRoute({
    pattern: 'bench.teapot',
    method: 'GET',
    path: '/bench/teapot',
  })
  public benchTeapot(): never {
    throw new ImATeapotException({ error: 'teapot' });
  }
}
