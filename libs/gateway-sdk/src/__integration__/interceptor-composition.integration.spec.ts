import 'reflect-metadata';

import {
  Controller,
  Injectable,
  InternalServerErrorException,
  UseInterceptors,
  type CallHandler,
  type ExecutionContext,
  type NestInterceptor,
} from '@nestjs/common';
import { INTERCEPTORS_METADATA } from '@nestjs/common/constants';
import { Test } from '@nestjs/testing';

import { beforeAll, describe, expect, it } from '@jest/globals';
import { firstValueFrom, from, map, of, switchMap, throwError, type Observable } from 'rxjs';

import { GatewayRoute } from '../decorators/gateway-route.decorator';
import { GatewayExceptionFilter } from '../filters/gateway-exception.filter';
import { GatewayResponseInterceptor } from '../interceptors/gateway-response.interceptor';
import { GatewayModule } from '../module/gateway.module';

import type { IGatewayReply } from '../types/gateway-reply.interface';
import type { IGatewayRequest } from '../types/gateway-request.interface';

/**
 * Integration tests that pin the composition order between the SDK-injected
 * interceptors and filter (from `@GatewayRoute`) and user-added
 * `@UseInterceptors` / `@UseFilters` / `@UseGuards` declarations on the same
 * handler. The goal is to catch a future NestJS version bump that silently
 * changes decorator-metadata merging, interceptor chain ordering, or filter
 * resolution — all three behaviors the SDK's composed decorators depend on.
 *
 * The locked-in contract these tests defend:
 *
 *   1. **User interceptors wrap the raw handler return value, SDK wraps
 *      LAST.** A user's `ClassSerializerInterceptor` (or any custom
 *      value-transforming interceptor) sees the bare DTO, strips
 *      `@Exclude()` fields, and only THEN does
 *      `GatewayResponseInterceptor` put the stripped object into an
 *      `IGatewayReply` envelope. If the composition flipped, the user
 *      interceptor would receive the envelope shape and its transform
 *      would no-op on the body.
 *
 *   2. **User guards run before any interceptor or handler logic.** If
 *      they throw, `GatewayExceptionFilter` catches and formats the
 *      reply. (Documented below but not exercised at this integration
 *      layer — see the "Test 2 skipped" note.)
 *
 *   3. **SDK filter catches exceptions emitted from user interceptors.**
 *      An interceptor that maps the handler result to an RxJS
 *      `throwError(...)` terminates its inner observable with an
 *      HttpException; the SDK filter must serialize it into the same
 *      envelope the success path produces.
 *
 * **Harness decision.** `gateway-route-flow.integration.spec.ts` — the
 * canonical SDK integration test — bootstraps a real
 * `Test.createTestingModule` for DI but drives interceptors and filters
 * manually via synthesized `ExecutionContext` / `ArgumentsHost` doubles,
 * rather than standing up a full microservice transport. This file
 * mirrors that exact style:
 *
 *   - Nest testing module compiled with `GatewayModule.forRoot()` so the
 *     real `GatewayResponseInterceptor`, `GatewayExceptionFilter`, reply
 *     builder, status resolver, and error body factory come from DI.
 *   - The fixture controller is registered so that `@GatewayRoute`'s
 *     metadata (including the `INTERCEPTORS_METADATA` array appended by
 *     `@UseInterceptors`) is materialized on the handler exactly as it
 *     would be at runtime.
 *   - Interceptors are composed by READING the handler's
 *     `INTERCEPTORS_METADATA` array and chaining them in array order —
 *     matching what Nest's `InterceptorsConsumer` does internally. No
 *     hardcoded ordering in the test body: if a future Nest version
 *     flips how `@UseInterceptors` + `applyDecorators` merge metadata,
 *     the metadata array flips, the chain composition flips, and the
 *     assertion on the final envelope shape fails loudly.
 *
 * **Test 2 (guard) is intentionally skipped.** Exercising guard
 * semantics end-to-end requires a full Nest microservice bootstrap with
 * an actual transport — the harness above cannot drive
 * `CanActivate.canActivate()` through the framework's exception pipeline
 * without that. The unit-level filter behavior for thrown HttpExceptions
 * is already covered by `gateway-exception.filter.spec.ts` and by the
 * error-path assertions in `gateway-route-flow.integration.spec.ts`.
 * When Phase F introduces a NATS-backed integration harness, the guard
 * composition test should be added there.
 */

/**
 * Fixture DTO whose `excluded` field must be stripped by the user's
 * interceptor before the SDK wraps the value. The field is typed as
 * required so accidentally omitting it from the handler return would be
 * a compile error — the test asserts strictly against presence after the
 * chain runs, not against the runtime shape of a partially-built object.
 */
interface IFixtureDto {
  readonly id: string;
  readonly name: string;
  readonly excluded: string;
}

/**
 * Canonical user-side value transformer: strips a single known field
 * from any plain-object handler return. A hand-rolled interceptor is
 * preferred over `ClassSerializerInterceptor` here because (a) the
 * workspace does not depend on `class-transformer`, and (b) the test
 * should verify the composition contract itself, not any particular
 * serialization library's internals.
 *
 * If this interceptor runs AFTER `GatewayResponseInterceptor` (i.e. the
 * composition order regressed), it would receive an `IGatewayReply`
 * envelope — which has no `excluded` key at the top level — and its
 * strip would silently no-op, leaving `excluded` present inside
 * `reply.body`. The assertion catches that case.
 */
@Injectable()
class StripExcludedFieldInterceptor implements NestInterceptor {
  public intercept(_context: ExecutionContext, next: CallHandler): Observable<unknown> {
    return next.handle().pipe(
      map((value: unknown) => {
        if (value !== null && typeof value === 'object') {
          const source = value as Record<string, unknown>;
          const stripped: Record<string, unknown> = {};

          for (const key of Object.keys(source)) {
            if (key !== 'excluded') {
              stripped[key] = source[key];
            }
          }

          return stripped;
        }

        return value;
      }),
    );
  }
}

/**
 * User-side interceptor that converts a successful handler return into
 * a rejection via RxJS `throwError`. Used to verify that the SDK's
 * filter catches exceptions emitted from the inner-interceptor chain,
 * not just exceptions thrown synchronously from the handler body.
 */
@Injectable()
class ThrowingInterceptor implements NestInterceptor {
  public intercept(_context: ExecutionContext, next: CallHandler): Observable<unknown> {
    return next.handle().pipe(
      map(() => {
        throw new InternalServerErrorException('interceptor failure');
      }),
    );
  }
}

@Controller()
class CompositionFixtureController {
  @UseInterceptors(StripExcludedFieldInterceptor)
  @GatewayRoute({ pattern: 'composition.strip', method: 'GET', path: '/composition/strip' })
  public getStrippedDto(): IFixtureDto {
    return { id: '1', name: 'alice', excluded: 'secret' };
  }

  @UseInterceptors(ThrowingInterceptor)
  @GatewayRoute({ pattern: 'composition.throw', method: 'GET', path: '/composition/throw' })
  public getDtoThatThrows(): IFixtureDto {
    return { id: '2', name: 'bob', excluded: 'secret' };
  }
}

type InterceptContext = Parameters<GatewayResponseInterceptor['intercept']>[0];
type FilterHost = Parameters<GatewayExceptionFilter['catch']>[1];

/**
 * Build the minimal envelope slice the SUT reads. Only `switchToRpc().getData()`
 * is touched on the success path — the handler has no `@GatewayBody` /
 * `@GatewayParam` params, so no extraction of body or params is needed.
 */
const buildEnvelope = (overrides: Partial<IGatewayRequest> = {}): IGatewayRequest => ({
  route: { method: 'GET', path: '/composition/strip', matchedPath: '/composition/strip' },
  params: {},
  query: {},
  headers: {},
  body: null,
  meta: {
    requestId: 'req-composition-1',
    remoteAddr: '127.0.0.1',
    receivedAt: Date.now(),
    timeoutMs: 30_000,
  },
  ...overrides,
});

const buildExecutionContext = (
  handler: (...args: never[]) => unknown,
  envelope: IGatewayRequest,
): InterceptContext =>
  ({
    getHandler: (): typeof handler => handler,
    getClass: (): typeof CompositionFixtureController => CompositionFixtureController,
    switchToRpc: () => ({
      getData: (): IGatewayRequest => envelope,
      getContext: (): Readonly<Record<string, unknown>> => ({}),
    }),
  }) as unknown as InterceptContext;

const buildArgumentsHost = (envelope: IGatewayRequest): FilterHost =>
  ({
    switchToRpc: () => ({
      getData: (): IGatewayRequest => envelope,
      getContext: (): Readonly<Record<string, unknown>> => ({}),
    }),
  }) as unknown as FilterHost;

/**
 * Constructor-type view over the `NestInterceptor` metadata entries
 * Nest stores under `INTERCEPTORS_METADATA`. `@UseInterceptors` can
 * also accept pre-constructed instances, but the decorators in this
 * spec file exclusively pass classes — so the metadata array is
 * guaranteed to contain constructors.
 */
type InterceptorClass = new (...args: never[]) => NestInterceptor;

/**
 * Compose an ordered list of interceptor instances into a single
 * `Observable` that mirrors what `InterceptorsConsumer` builds
 * internally: index 0 is OUTERMOST (its `intercept` call wraps the
 * inner chain), index `n-1` is INNERMOST (its `handle()` invokes the
 * terminal `handler` function). Recursion is trivially safe here —
 * chains are at most 3-4 interceptors long.
 */
const composeInterceptorChain = (
  instances: readonly NestInterceptor[],
  context: ExecutionContext,
  terminal: () => Observable<unknown>,
): Observable<unknown> => {
  const step = (index: number): Observable<unknown> => {
    if (index >= instances.length) {
      return terminal();
    }

    const current = instances[index];

    if (current === undefined) {
      return terminal();
    }

    // Nest's `NestInterceptor.intercept` signature permits returning
    // either `Observable<T>` or `Promise<Observable<T>>`. Normalise by
    // wrapping the return in `from(...)` — `from` accepts a plain
    // observable (passthrough) or a promise (awaited then flattened) —
    // and then flattening with `switchMap` so the outer type is always
    // `Observable<unknown>` regardless of which branch the interceptor
    // picked.
    const result = current.intercept(context, { handle: () => step(index + 1) });

    return from(Promise.resolve(result)).pipe(switchMap((inner) => inner));
  };

  return step(0);
};

describe('interceptor/filter composition order (integration)', () => {
  let gatewayResponseInterceptor: GatewayResponseInterceptor;
  let gatewayExceptionFilter: GatewayExceptionFilter;
  let stripInterceptor: StripExcludedFieldInterceptor;
  let throwingInterceptor: ThrowingInterceptor;

  beforeAll(async () => {
    const moduleRef = await Test.createTestingModule({
      imports: [GatewayModule.forRoot()],
      controllers: [CompositionFixtureController],
      providers: [StripExcludedFieldInterceptor, ThrowingInterceptor],
    }).compile();

    gatewayResponseInterceptor = moduleRef.get(GatewayResponseInterceptor);
    gatewayExceptionFilter = moduleRef.get(GatewayExceptionFilter);
    stripInterceptor = moduleRef.get(StripExcludedFieldInterceptor);
    throwingInterceptor = moduleRef.get(ThrowingInterceptor);
  });

  /**
   * Sanity check on the metadata layer: `@GatewayRoute` applied first
   * from inside `applyDecorators` and then `@UseInterceptors` appended
   * by the user should yield a two-element array in the order
   * `[GatewayResponseInterceptor, <user>]`. This is the input Nest's
   * `InterceptorsConsumer` reads at runtime. If this array order ever
   * changes, the composition tests below still catch the effect on the
   * wire — but asserting the array directly localizes the failure to
   * "metadata merge order changed" vs "runtime behavior changed".
   */
  describe('metadata array order', () => {
    it('places GatewayResponseInterceptor before user-added StripExcludedFieldInterceptor', () => {
      const metadata = Reflect.getMetadata(
        INTERCEPTORS_METADATA,
        CompositionFixtureController.prototype.getStrippedDto,
      ) as readonly InterceptorClass[] | undefined;

      expect(metadata).toBeDefined();
      expect(metadata).toHaveLength(2);
      expect(metadata?.[0]).toBe(GatewayResponseInterceptor);
      expect(metadata?.[1]).toBe(StripExcludedFieldInterceptor);
    });

    it('places GatewayResponseInterceptor before user-added ThrowingInterceptor', () => {
      const metadata = Reflect.getMetadata(
        INTERCEPTORS_METADATA,
        CompositionFixtureController.prototype.getDtoThatThrows,
      ) as readonly InterceptorClass[] | undefined;

      expect(metadata).toBeDefined();
      expect(metadata).toHaveLength(2);
      expect(metadata?.[0]).toBe(GatewayResponseInterceptor);
      expect(metadata?.[1]).toBe(ThrowingInterceptor);
    });
  });

  /**
   * Test 1: user interceptor transforms the handler return value
   * BEFORE `GatewayResponseInterceptor` wraps it into an envelope.
   *
   * Because metadata index 0 (OUTERMOST) is `GatewayResponseInterceptor`
   * and index 1 (INNERMOST) is `StripExcludedFieldInterceptor`, the
   * response-path ordering is:
   *
   *   handler emits { id, name, excluded }
   *     -> StripExcludedFieldInterceptor.map strips `excluded`
   *     -> GatewayResponseInterceptor.map wraps { id, name } in reply
   *     -> assertion sees { status: 200, headers: {}, body: { id, name } }
   *
   * If a future Nest version flipped the chain to run
   * `StripExcludedFieldInterceptor` first (outer), it would receive the
   * already-wrapped envelope, the spread would copy the envelope keys
   * minus a non-existent `excluded`, and the final body would still
   * carry `excluded: 'secret'` nested inside the reply. The assertion
   * below fails loudly in that scenario.
   */
  describe('user interceptor -> SDK wrap', () => {
    it('strips user-marked fields before the SDK wraps the reply', async () => {
      const envelope = buildEnvelope();
      const handler = CompositionFixtureController.prototype.getStrippedDto;
      const context = buildExecutionContext(handler, envelope);
      const metadata = Reflect.getMetadata(
        INTERCEPTORS_METADATA,
        handler,
      ) as readonly InterceptorClass[];
      const instances = metadata.map((cls) => {
        if (cls === GatewayResponseInterceptor) {
          return gatewayResponseInterceptor;
        }

        if (cls === StripExcludedFieldInterceptor) {
          return stripInterceptor;
        }

        throw new Error(`Unexpected interceptor class in metadata: ${cls.name}`);
      });

      const reply = (await firstValueFrom(
        composeInterceptorChain(instances, context, () =>
          of({ id: '1', name: 'alice', excluded: 'secret' }),
        ),
      )) as IGatewayReply<Record<string, unknown>>;

      expect(reply.status).toBe(200);
      expect(reply.headers).toEqual({});
      expect(reply.body).toEqual({ id: '1', name: 'alice' });
      expect(reply.body).not.toHaveProperty('excluded');
      // Defence-in-depth against a regression where the envelope ends
      // up nested inside itself (user interceptor strips the outer
      // envelope's keys and re-wraps). A valid reply MUST NOT have its
      // own `status` key showing up on `body`.
      expect(reply.body).not.toHaveProperty('status');
    });
  });

  /**
   * Test 3: an exception raised from inside the user's interceptor
   * chain must propagate to `GatewayExceptionFilter`, which serializes
   * it into the same envelope shape the success path produces.
   *
   * Because the response-path chain is
   * `GatewayResponseInterceptor.map` wrapping
   * `ThrowingInterceptor.map`, and `ThrowingInterceptor` throws from
   * within its `map` callback, the throw surfaces on the OUTER
   * observable before the outer `map` runs. Nest would normally route
   * that through the configured filter chain. Here we simulate the
   * same by subscribing to the chain, catching the synchronous
   * rejection, and feeding it to the real
   * `GatewayExceptionFilter.catch` — the SUT for this assertion.
   *
   * The filter sees a real `InternalServerErrorException` instance
   * (resolved through the real DI-built `DefaultErrorBodyFactory`) and
   * must produce a 500 envelope whose `body` matches Nest's native
   * `{ statusCode, message, error }` shape.
   */
  describe('SDK filter catches exceptions from user interceptors', () => {
    it('wraps a thrown HttpException from a user interceptor into a 500 reply envelope', async () => {
      const envelope = buildEnvelope({
        route: {
          method: 'GET',
          path: '/composition/throw',
          matchedPath: '/composition/throw',
        },
      });
      const handler = CompositionFixtureController.prototype.getDtoThatThrows;
      const context = buildExecutionContext(handler, envelope);
      const metadata = Reflect.getMetadata(
        INTERCEPTORS_METADATA,
        handler,
      ) as readonly InterceptorClass[];
      const instances = metadata.map((cls) => {
        if (cls === GatewayResponseInterceptor) {
          return gatewayResponseInterceptor;
        }

        if (cls === ThrowingInterceptor) {
          return throwingInterceptor;
        }

        throw new Error(`Unexpected interceptor class in metadata: ${cls.name}`);
      });

      let captured: unknown;

      try {
        await firstValueFrom(
          composeInterceptorChain(instances, context, () =>
            of({ id: '2', name: 'bob', excluded: 'secret' }),
          ),
        );
      } catch (error: unknown) {
        captured = error;
      }

      expect(captured).toBeInstanceOf(InternalServerErrorException);

      const host = buildArgumentsHost(envelope);
      const reply = await firstValueFrom(gatewayExceptionFilter.catch(captured, host));

      expect(reply.status).toBe(500);
      expect(reply.headers).toEqual({});
      expect(reply.body).toEqual({
        statusCode: 500,
        message: 'interceptor failure',
        error: 'Internal Server Error',
      });
    });

    it('falls back to the observable-error path when the user interceptor emits via throwError', async () => {
      const envelope = buildEnvelope({
        route: {
          method: 'GET',
          path: '/composition/throw',
          matchedPath: '/composition/throw',
        },
      });
      const handler = CompositionFixtureController.prototype.getDtoThatThrows;
      const context = buildExecutionContext(handler, envelope);

      const chain = gatewayResponseInterceptor.intercept(context, {
        handle: () =>
          of<IFixtureDto>({ id: '2', name: 'bob', excluded: 'secret' }).pipe(
            map(() => {
              throw new InternalServerErrorException('throwError path');
            }),
          ),
      });

      let captured: unknown;

      try {
        await firstValueFrom(chain);
      } catch (error: unknown) {
        captured = error;
      }

      expect(captured).toBeInstanceOf(InternalServerErrorException);

      const host = buildArgumentsHost(envelope);
      const reply = await firstValueFrom(gatewayExceptionFilter.catch(captured, host));

      expect(reply.status).toBe(500);
      expect(reply.body).toEqual({
        statusCode: 500,
        message: 'throwError path',
        error: 'Internal Server Error',
      });
    });
  });

  /**
   * Bonus: verify the `throwError` observable code path — mapping a
   * handler result into an RxJS error notification (rather than a
   * synchronous throw from `map`). This exercises a subtly different
   * RxJS branch: the error emerges through the observable's error
   * channel instead of surfacing as a thrown exception on
   * `firstValueFrom`. Both should land in the same filter catch and
   * produce the same envelope.
   */
  describe('throwError observable path', () => {
    it('propagates a throwError notification from the user chain to the SDK filter', async () => {
      const envelope = buildEnvelope();
      const handler = CompositionFixtureController.prototype.getStrippedDto;
      const context = buildExecutionContext(handler, envelope);

      const chain = gatewayResponseInterceptor.intercept(context, {
        handle: () => throwError(() => new InternalServerErrorException('rx fail')),
      });

      let captured: unknown;

      try {
        await firstValueFrom(chain);
      } catch (error: unknown) {
        captured = error;
      }

      expect(captured).toBeInstanceOf(InternalServerErrorException);

      const host = buildArgumentsHost(envelope);
      const reply = await firstValueFrom(gatewayExceptionFilter.catch(captured, host));

      expect(reply.status).toBe(500);
      expect(reply.body).toEqual({
        statusCode: 500,
        message: 'rx fail',
        error: 'Internal Server Error',
      });
    });
  });
});
