// @ts-nocheck — bench stack has its own tsconfig + npm install happens inside docker
import { Controller, ImATeapotException } from '@nestjs/common';

import { GatewayRoute } from '@zerly/gateway-sdk';

/**
 * Bench controller — three routes exposed via the Zerly gateway SDK.
 *
 * Unlike the vanilla Nest stacks, there is no direct HTTP entry point
 * here: `@GatewayRoute` registers each handler as a NATS message
 * pattern *and* publishes HTTP metadata into the handler registry KV
 * bucket, so the Go gateway-server translates incoming HTTP into NATS
 * requests that land on these methods.
 *
 * The routes mirror the other stacks so the wall-clock numbers are
 * directly comparable:
 *
 *  - sync 200 (`/bench/hello`) — synchronous handler, exercises the
 *    SDK's fast-path for non-Promise returns.
 *  - async 200 (`/bench/hello-async`) — Promise-returning handler,
 *    exercises the await chain through the response interceptor.
 *    The delta between the two is exactly the sync-flow improvement
 *    surface in the SDK.
 *  - 418 throw (`/bench/teapot`) — exercises the gateway SDK's
 *    exception filter + envelope encoder + gateway-server's error
 *    response path.
 */
@Controller()
export class BenchController {
  @GatewayRoute({
    pattern: 'bench.hello',
    method: 'GET',
    path: '/bench/hello',
  })
  public hello(): { readonly ok: true } {
    return { ok: true };
  }

  @GatewayRoute({
    pattern: 'bench.helloAsync',
    method: 'GET',
    path: '/bench/hello-async',
  })
  public async helloAsync(): Promise<{ readonly ok: true }> {
    return { ok: true };
  }

  @GatewayRoute({
    pattern: 'bench.teapot',
    method: 'GET',
    path: '/bench/teapot',
  })
  public teapot(): never {
    throw new ImATeapotException({ error: 'teapot' });
  }
}
