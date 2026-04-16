// @ts-nocheck — bench stack has its own tsconfig + npm install happens inside docker
import { Controller, Get, ImATeapotException } from '@nestjs/common';

/**
 * Bench controller — three routes, zero business logic.
 *
 * - GET /bench/hello (sync) returns a static JSON body directly.
 *   Measures the pure happy-path hot loop of NestJS + the HTTP
 *   adapter under test with a synchronous handler.
 *
 * - GET /bench/hello-async (async) returns a resolved Promise of
 *   the same body. Measures the Promise / microtask overhead Nest
 *   adds on async handlers — relevant for the sync-flow improvements
 *   the SDK is getting, where sync returns should skip awaits that
 *   async returns still pay.
 *
 * - GET /bench/teapot throws `ImATeapotException`. Measures the
 *   exception-formatting cost: Nest's exception filter chain,
 *   HttpException → reply body conversion, adapter-side serialization.
 *   Must be a throw, not a `res.status(418).json(...)` emission —
 *   otherwise we'd be benching a happy path that happens to emit 418
 *   and missing the exception path entirely.
 */
@Controller('bench')
export class BenchController {
  @Get('hello')
  public hello(): { readonly ok: true } {
    return { ok: true };
  }

  @Get('hello-async')
  public async helloAsync(): Promise<{ readonly ok: true }> {
    return { ok: true };
  }

  @Get('teapot')
  public teapot(): never {
    throw new ImATeapotException({ error: 'teapot' });
  }
}
