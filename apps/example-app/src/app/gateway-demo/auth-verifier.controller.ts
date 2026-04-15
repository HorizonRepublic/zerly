import { Controller, ForbiddenException, UnauthorizedException } from '@nestjs/common';

import { GatewayAuthVerifier, GatewayHeaders, GatewayResponse } from '@zerly/gateway-sdk';

import type { IGatewayResponse } from '@zerly/gateway-sdk';

/**
 * Toy "bearer token" user shape produced by the demo verifier.
 * Real consumers project their own user DTO here; the gateway is
 * agnostic and forwards whatever shape the verifier returns.
 */
export interface IDemoAuthUser {
  readonly id: string;
  readonly email: string;
  readonly roles: readonly string[];
}

/**
 * Demonstrates the `@GatewayAuthVerifier` surface with a trivial
 * prefix-match verifier that accepts any `Bearer demo-<name>` token
 * and rejects everything else.
 * @remarks
 * Nothing about this verifier is cryptographically meaningful — it
 * exists purely so the example app can exercise the entire auth
 * contract end-to-end without depending on `jsonwebtoken` or a
 * session store. Swap `verify(...)` for whatever real logic your
 * project needs; the decorator contract is unchanged.
 *
 * Token parsing rules:
 * - `demo-banned` → throws `ForbiddenException` (403) so consumers
 *   can see the "known user, denied" path.
 * - `demo-admin` → returns a user with `['admin', 'user']` roles.
 * - `demo-rotate-<anything>` → returns the user AND sets a freshly
 *   rotated `sid` cookie via `@GatewayResponse().cookie()`. Verifies
 *   that verifier-side cookie mutations merge into the final HTTP
 *   response through the Phase E Go-side mergeAuthHeaders path.
 * - Any other `demo-<name>` → returns a baseline `['user']` user.
 * - Missing, non-bearer, or non-`demo-` tokens → throws
 *   `UnauthorizedException` (401).
 *
 * Registered as the default verifier (`default: true`), so routes
 * can opt in with a bare `auth: true` rather than naming the id
 * explicitly.
 */
@Controller()
export class AuthVerifierController {
  @GatewayAuthVerifier({ id: 'demo', default: true })
  public verify(
    @GatewayHeaders() headers: Readonly<Record<string, string>>,
    @GatewayResponse() res: IGatewayResponse,
  ): IDemoAuthUser {
    const authHeader = headers['authorization'];
    const token = authHeader?.replace(/^Bearer /, '').trim();

    if (token === undefined || token === '' || !token.startsWith('demo-')) {
      throw new UnauthorizedException('Missing or invalid bearer token');
    }

    const name = token.slice('demo-'.length);

    if (name === 'banned') {
      throw new ForbiddenException('Account suspended');
    }

    // Rotation path: any `demo-rotate-*` token triggers the verifier
    // to emit a fresh session cookie alongside its claims. This is
    // the canonical session-rotation demo — the cookie reaches the
    // client verbatim via the verifier→route header merge, without
    // the route handler knowing anything about it.
    if (name.startsWith('rotate-')) {
      res.cookie('sid', `demo-${name}-rotated`, {
        httpOnly: true,
        secure: false,
        sameSite: 'lax',
        path: '/',
        maxAge: 3600,
      });
    }

    const roles = name === 'admin' ? (['admin', 'user'] as const) : (['user'] as const);

    return {
      id: `user-${name}`,
      email: `${name}@example.test`,
      roles,
    };
  }
}
