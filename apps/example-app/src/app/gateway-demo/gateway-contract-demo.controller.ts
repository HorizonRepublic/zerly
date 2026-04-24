import { Controller } from '@nestjs/common';

import {
  GatewayBody,
  GatewayHeader,
  GatewayMeta,
  GatewayQuery,
  GatewayResponse,
  GatewayRoute,
} from '@zerly/gateway-sdk';

import type { IGatewayRequestMeta, IGatewayResponse } from '@zerly/gateway-sdk';

/**
 * Response body for the simple contract endpoints. Kept trivially small so
 * the wire assertions can focus on status/headers/cookies.
 */
interface IContractEcho {
  readonly ok: true;
}

/**
 * Query payload for the timeout endpoint. `delayMs` is parsed numerically
 * by the gateway SDK's query decoder; the handler uses it to drive a
 * `setTimeout` sleep that deliberately exceeds the per-route timeout.
 */
interface ITimeoutQuery {
  readonly delayMs?: string;
}

/**
 * Request body for `POST /cookies/set`. Kept empty — the handler sets a
 * static session cookie to exercise the `forRoot` cookie defaults merge.
 */
type ICookieSetDto = Record<string, never>;

/**
 * Routes dedicated to the GatewayRoute **contract** e2e harness.
 * @remarks
 * Each handler exercises one slice of the per-route / forRoot contract:
 * CORS, rate limit, timeout, static response headers, or cookie defaults.
 * The bodies are intentionally trivial — the e2e tests assert on the
 * wire envelope (status, headers, cookies), not on any domain behaviour.
 *
 * This controller is mounted by `GatewayDemoModule`; it runs under the
 * same `GatewayModule.forRoot({ defaults })` configured in `AppModule`,
 * so per-route overrides here can be diffed against the module-level
 * defaults from an e2e test perspective.
 */
@Controller()
export class GatewayContractDemoController {
  /**
   * CORS-protected public endpoint. Allows a single explicit origin and
   * no credentials. Used to assert that OPTIONS preflight matches on
   * origin + `Access-Control-Request-Method` and that an actual GET
   * carries the CORS response headers.
   */
  @GatewayRoute({
    pattern: 'contract.cors.public',
    method: 'GET',
    path: '/cors/public',
    cors: {
      origins: ['https://example.test'],
      methods: ['GET'],
      headers: ['Content-Type', 'X-Request-Id'],
      maxAge: 600,
    },
  })
  public corsPublic(): IContractEcho {
    return { ok: true };
  }

  /**
   * CORS-protected endpoint that allows credentials. A separate handler
   * is used because `credentials: true` with a wildcard origin is invalid
   * per the CORS spec — this route pins the "explicit origin + credentials"
   * branch and asserts `Access-Control-Allow-Credentials: true` on
   * preflight.
   */
  @GatewayRoute({
    pattern: 'contract.cors.creds',
    method: 'GET',
    path: '/cors/creds',
    cors: {
      origins: ['https://example.test'],
      methods: ['GET'],
      credentials: true,
    },
  })
  public corsCreds(): IContractEcho {
    return { ok: true };
  }

  /**
   * Rate-limited endpoint with `rps: 1, burst: 1` so the second request
   * within a second is guaranteed to be rejected with 429. The tight
   * budget is deliberate — a larger burst would make the e2e assertion
   * timing-sensitive and flaky under laptop scheduling jitter.
   */
  @GatewayRoute({
    pattern: 'contract.ratelimit.basic',
    method: 'GET',
    path: '/rate-limit/basic',
    rateLimit: { rps: 1, burst: 1 },
  })
  public rateLimitBasic(): IContractEcho {
    return { ok: true };
  }

  /**
   * Rate-limited endpoint keyed by `X-API-Key`. Two different keys must
   * receive independent token buckets so the e2e test can prove the
   * keyBy chain isolates tenants. Falls back to client IP when the
   * header is absent.
   */
  @GatewayRoute({
    pattern: 'contract.ratelimit.bykey',
    method: 'GET',
    path: '/rate-limit/by-header',
    rateLimit: { rps: 1, burst: 1, keyBy: ['header:x-api-key', 'ip'] },
  })
  public rateLimitByHeader(@GatewayHeader('x-api-key') _key: string): IContractEcho {
    return { ok: true };
  }

  /**
   * Per-route timeout endpoint. The route budget is 200ms; the handler
   * sleeps for `delayMs` (default 500ms) so the NATS request exceeds
   * the deadline and the gateway returns 504 Gateway Timeout.
   */
  @GatewayRoute({
    pattern: 'contract.timeout.slow',
    method: 'GET',
    path: '/timeout/slow',
    timeout: 200,
  })
  public async timeoutSlow(@GatewayQuery() query: ITimeoutQuery): Promise<IContractEcho> {
    const ms = Number(query.delayMs ?? '500');

    await new Promise((resolve) => setTimeout(resolve, ms));

    return { ok: true };
  }

  /**
   * Endpoint with a per-route static response header. The e2e test
   * asserts both the route-level `x-custom` is present AND the
   * forRoot-level `x-frame-options` default still flows through — the
   * deep-merge contract MUST preserve module-level headers that the
   * route does not override.
   */
  @GatewayRoute({
    pattern: 'contract.headers.route',
    method: 'GET',
    path: '/headers/route',
    headers: { 'x-custom': 'route-value' },
  })
  public headersRoute(): IContractEcho {
    return { ok: true };
  }

  /**
   * Endpoint that writes a bare `res.cookie('sid', ...)` with no
   * options. The cookie serializer merges `forRoot` cookie defaults
   * (`httpOnly`, `sameSite`, `path`, `maxAge`) into the wire cookie,
   * so the e2e assertion proves the defaults reach the client without
   * the handler repeating them.
   */
  @GatewayRoute({
    pattern: 'contract.cookies.set',
    method: 'POST',
    path: '/cookies/set',
  })
  public cookiesSet(
    @GatewayBody() _dto: ICookieSetDto,
    @GatewayResponse() res: IGatewayResponse,
  ): IContractEcho {
    res.cookie('sid', 'contract-probe');

    return { ok: true };
  }

  /**
   * Echoes the gateway-resolved client IP via `@GatewayMeta()`.
   * @remarks
   * Consumed exclusively by the `trustedproxy_test.go` /
   * `trustedproxy_empty_test.go` e2e suites. The handler body is a
   * single-field response so the tests can assert on a stable
   * shape regardless of future envelope additions.
   * @param meta - Full gateway request metadata; tests read `remoteAddr`.
   * @returns The resolved client IP as observed on the Nest side.
   */
  @GatewayRoute({
    pattern: 'contract.whoami',
    method: 'GET',
    path: '/whoami',
  })
  public whoami(@GatewayMeta() meta: IGatewayRequestMeta): { readonly ip: string } {
    return { ip: meta.remoteAddr };
  }

  /**
   * Dedicated endpoint for the multi-replica NATS KV rate-limit
   * e2e scenario. Configured with a tight rps so the e2e test can
   * drive traffic against two gateway pods and observe the shared
   * bucket rejecting excess requests regardless of which pod
   * served each request.
   *
   * Isolated from other demo routes so the e2e budget is not
   * affected by parallel runs of other test scenarios.
   */
  @GatewayRoute({
    pattern: 'multi-replica-rl.probe',
    method: 'POST',
    path: '/api/multi-replica-rl',
    rateLimit: {
      rps: 10,
      burst: 10,
      store: 'nats-kv',
      keyBy: ['ip'],
    },
  })
  public probeMultiReplicaRL(): IContractEcho {
    return { ok: true };
  }
}
