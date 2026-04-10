import 'reflect-metadata';

import { BadRequestException, Controller, Injectable, NotFoundException } from '@nestjs/common';
import { Reflector } from '@nestjs/core';
import { PATTERN_EXTRAS_METADATA } from '@nestjs/microservices/constants';
import { Test } from '@nestjs/testing';

import { beforeAll, describe, expect, it } from '@jest/globals';
import { firstValueFrom, of } from 'rxjs';

import { ApiGateway } from '../decorators/api-gateway.decorator';
import { GatewayBody } from '../decorators/params/gateway-body.decorator';
import { GatewayParam } from '../decorators/params/gateway-param.decorator';
import { GatewayRequestId } from '../decorators/params/gateway-request-id.decorator';
import { GatewayExceptionFilter } from '../filters/gateway-exception.filter';
import { GatewayResponseInterceptor } from '../interceptors/gateway-response.interceptor';
import { GatewayModule } from '../module/gateway.module';

import type { IGatewayReply } from '../types/gateway-reply.interface';
import type { IGatewayRequest } from '../types/gateway-request.interface';

/**
 * Fixture domain exception carrying the `isDomainException` marker recognized
 * by `DefaultErrorBodyFactory`.
 * @remarks
 * Deliberately a local fixture rather than an import from `@zerly/errors` —
 * this proves the duck-type contract the factory depends on works without
 * the real class, and keeps the integration test self-contained and free
 * from cross-library coupling. Any consumer that implements the same shape
 * (true marker + status/code/message/optional details) must get identical
 * treatment from the default factory; this fixture is what certifies that
 * invariant end-to-end.
 */
class FixtureDomainException extends Error {
  public readonly isDomainException = true as const;

  public constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly details?: Readonly<Record<string, unknown>>,
  ) {
    super(message);
  }
}

/**
 * Minimal user aggregate shape returned by the fixture controller. Mirrors
 * a typical read-model DTO without depending on `@zerly/core`'s
 * `IBaseResource` because the integration test is concerned with the wire
 * envelope, not entity semantics.
 */
interface ITestUser {
  readonly id: string;
  readonly name: string;
}

/**
 * Payload received by the `POST /users` handler. Kept intentionally minimal
 * (no validation decorators) — the gateway SDK layer under test does not
 * touch request-body validation; that responsibility lives in downstream
 * NestJS pipes.
 */
interface ICreateUserDto {
  readonly email: string;
  readonly name: string;
}

/**
 * Expanded shape returned by the `POST /users` handler. Echoes the gateway
 * request id back to the caller so the integration test can verify that
 * `@GatewayRequestId` resolution participates in the final envelope.
 */
interface ITestUserWithRequestId extends ITestUser {
  readonly requestId: string;
}

@Injectable()
class TestUsersService {
  private readonly users = new Map<string, ITestUser>([['1', { id: '1', name: 'Alice' }]]);

  public findById(id: string): ITestUser | null {
    return this.users.get(id) ?? null;
  }

  public create(dto: ICreateUserDto): ITestUser {
    const id = String(this.users.size + 1);
    const user: ITestUser = { id, name: dto.name };

    this.users.set(id, user);

    return user;
  }
}

@Controller()
class TestUsersController {
  public constructor(private readonly service: TestUsersService) {}

  @ApiGateway({ pattern: 'users.get', method: 'GET', path: '/users/:id' })
  public getUser(@GatewayParam('id') id: string): ITestUser {
    const user = this.service.findById(id);

    if (!user) {
      throw new FixtureDomainException(404, 'USER_NOT_FOUND', 'User not found', {
        lookupId: id,
      });
    }

    return user;
  }

  @ApiGateway({
    pattern: 'users.create',
    method: 'POST',
    path: '/users',
    statusCode: 201,
  })
  public createUser(
    @GatewayBody() dto: ICreateUserDto,
    @GatewayRequestId() requestId: string,
  ): ITestUserWithRequestId {
    const user = this.service.create(dto);

    return { ...user, requestId };
  }

  @ApiGateway({ pattern: 'users.delete', method: 'DELETE', path: '/users/:id' })
  public deleteUser(@GatewayParam('id') _id: string): void {
    // Void return should yield 204 via DefaultStatusResolver.
  }
}

/**
 * Shape of the `extras` record stored at `PATTERN_EXTRAS_METADATA` by the
 * composed `@ApiGateway` -> `@MessagePattern(..., { meta: { http } })`
 * chain. Declared locally so metadata assertions stay type-safe without
 * leaking a framework-internal descriptor.
 */
interface IExtrasWithHttpMeta {
  readonly meta: {
    readonly http: {
      readonly method: string;
      readonly path: string;
      readonly statusCode?: number;
    };
  };
}

/**
 * Build a minimal envelope for feeding into the interceptor/filter under
 * test. Each caller overrides only the fields it cares about (route,
 * params, body) while leaving the rest (query, headers, meta) populated
 * with sensible defaults mirroring what the Go gateway would produce for a
 * real HTTP request.
 */
const buildEnvelope = <TBody>(
  body: TBody,
  overrides: Partial<Omit<IGatewayRequest, 'body'>> = {},
): IGatewayRequest<TBody> => ({
  route: { method: 'GET', path: '/users/:id', matchedPath: '/users/1' },
  params: { id: '1' },
  query: {},
  headers: {},
  meta: {
    requestId: 'req-integration-1',
    remoteAddr: '127.0.0.1',
    receivedAt: Date.now(),
    timeoutMs: 30_000,
  },
  ...overrides,
  body,
});

type InterceptContext = Parameters<GatewayResponseInterceptor['intercept']>[0];
type FilterHost = Parameters<GatewayExceptionFilter['catch']>[1];

/**
 * Construct a test double for `ExecutionContext` that satisfies the slice
 * of the interface the interceptor actually reads: `getHandler()` for
 * metadata lookup and `switchToRpc().getData()` for envelope access. The
 * `as unknown as` cast is the canonical escape hatch for partial interface
 * doubles — full `ExecutionContext` implementations are out of scope.
 */
const buildExecutionContext = (
  handler: (...args: never[]) => unknown,
  envelope: IGatewayRequest,
): InterceptContext =>
  ({
    getHandler: (): typeof handler => handler,
    getClass: (): typeof TestUsersController => TestUsersController,
    switchToRpc: () => ({
      getData: (): IGatewayRequest => envelope,
      getContext: (): Readonly<Record<string, unknown>> => ({}),
    }),
  }) as unknown as InterceptContext;

/**
 * Construct a test double for `ArgumentsHost` that satisfies the slice of
 * the interface the filter actually reads: `switchToRpc().getData()` for
 * envelope access.
 */
const buildArgumentsHost = (envelope: IGatewayRequest | undefined): FilterHost =>
  ({
    switchToRpc: () => ({
      getData: (): IGatewayRequest | undefined => envelope,
      getContext: (): Readonly<Record<string, unknown>> => ({}),
    }),
  }) as unknown as FilterHost;

/**
 * End-to-end integration of `@ApiGateway` flowing through a real
 * `Test.createTestingModule`. Verifies that (1) the composed decorator
 * writes HTTP metadata to NestJS's native `PATTERN_EXTRAS_METADATA` key
 * where the interceptor can read it, (2) the real `GatewayResponseInterceptor`
 * resolved from the DI container wraps handler returns into an
 * `IGatewayReply` envelope with status chosen by the resolver, and (3) the
 * real `GatewayExceptionFilter` serializes both duck-typed `DomainException`
 * throws and plain `Error` throws into the RFC 7807 error envelope.
 *
 * No mocks are used for `@nestjs/common` / `@nestjs/core` /
 * `@nestjs/microservices`; the real runtime from the ESM-migrated test
 * infrastructure (M8.5) provides every framework surface touched here.
 */
describe('@ApiGateway end-to-end flow (integration)', () => {
  let interceptor: GatewayResponseInterceptor;
  let filter: GatewayExceptionFilter;
  let reflector: Reflector;

  beforeAll(async () => {
    const moduleRef = await Test.createTestingModule({
      imports: [GatewayModule.forRoot({ isProduction: false })],
      controllers: [TestUsersController],
      providers: [TestUsersService],
    }).compile();

    interceptor = moduleRef.get(GatewayResponseInterceptor);
    filter = moduleRef.get(GatewayExceptionFilter);
    reflector = moduleRef.get(Reflector);
  });

  /**
   * Reflection layer: the composed decorator MUST write HTTP routing
   * metadata to the same `PATTERN_EXTRAS_METADATA` key NestJS uses
   * natively, with the correct shape per handler. These three cases
   * together cover (GET, no statusCode), (POST, explicit 201), and
   * (DELETE, no statusCode) — the last also asserts that `statusCode`
   * is ABSENT (not `undefined`) under `exactOptionalPropertyTypes`.
   */
  describe('metadata readback', () => {
    it('exposes GET /users/:id without statusCode', () => {
      const extras = reflector.get<IExtrasWithHttpMeta | undefined>(
        PATTERN_EXTRAS_METADATA,
        TestUsersController.prototype.getUser,
      );

      expect(extras).toEqual({
        meta: { http: { method: 'GET', path: '/users/:id' } },
      });
      expect(extras?.meta.http).not.toHaveProperty('statusCode');
    });

    it('exposes POST /users with explicit statusCode 201', () => {
      const extras = reflector.get<IExtrasWithHttpMeta | undefined>(
        PATTERN_EXTRAS_METADATA,
        TestUsersController.prototype.createUser,
      );

      expect(extras).toEqual({
        meta: {
          http: { method: 'POST', path: '/users', statusCode: 201 },
        },
      });
    });

    it('exposes DELETE /users/:id and omits statusCode entirely', () => {
      const extras = reflector.get<IExtrasWithHttpMeta | undefined>(
        PATTERN_EXTRAS_METADATA,
        TestUsersController.prototype.deleteUser,
      );

      expect(extras).toEqual({
        meta: { http: { method: 'DELETE', path: '/users/:id' } },
      });
      expect(extras?.meta.http).not.toHaveProperty('statusCode');
    });
  });

  /**
   * Success path: the real `GatewayResponseInterceptor` from the DI
   * container reads metadata for the current handler, applies the status
   * resolver, and wraps the return value via the reply builder. Three
   * variants cover the matrix of (default 200), (explicit 201), and
   * (void/null -> 204) without touching the param decorators — the
   * interceptor is fed the handler result directly through a synthetic
   * `CallHandler`, which is sufficient to exercise the wrap-and-envelope
   * contract under real DI.
   */
  describe('success path via interceptor', () => {
    it('wraps a GET return value into a 200 envelope with empty headers', async () => {
      const envelope = buildEnvelope(null);
      const handlerResult: ITestUser = { id: '1', name: 'Alice' };
      const context = buildExecutionContext(TestUsersController.prototype.getUser, envelope);

      const reply = (await firstValueFrom(
        interceptor.intercept(context, { handle: () => of(handlerResult) }),
      )) as IGatewayReply<ITestUser>;

      expect(reply).toEqual({
        status: 200,
        headers: {},
        body: { id: '1', name: 'Alice' },
      });
    });

    it('wraps a POST return value into a 201 envelope (explicit statusCode wins)', async () => {
      const envelope = buildEnvelope<ICreateUserDto>(
        { email: 'bob@example.com', name: 'Bob' },
        {
          route: { method: 'POST', path: '/users', matchedPath: '/users' },
          params: {},
        },
      );
      const handlerResult: ITestUserWithRequestId = {
        id: '2',
        name: 'Bob',
        requestId: envelope.meta.requestId,
      };
      const context = buildExecutionContext(TestUsersController.prototype.createUser, envelope);

      const reply = (await firstValueFrom(
        interceptor.intercept(context, { handle: () => of(handlerResult) }),
      )) as IGatewayReply<ITestUserWithRequestId>;

      expect(reply.status).toBe(201);
      expect(reply.headers).toEqual({});
      expect(reply.body).toEqual({
        id: '2',
        name: 'Bob',
        requestId: 'req-integration-1',
      });
    });

    it('wraps a void DELETE return into a 204 envelope with null body', async () => {
      const envelope = buildEnvelope(null, {
        route: { method: 'DELETE', path: '/users/:id', matchedPath: '/users/1' },
      });
      const context = buildExecutionContext(TestUsersController.prototype.deleteUser, envelope);

      const reply = (await firstValueFrom(
        interceptor.intercept(context, { handle: () => of(undefined) }),
      )) as IGatewayReply<unknown>;

      expect(reply.status).toBe(204);
      expect(reply.headers).toEqual({});
      // DefaultGatewayReplyBuilder coerces `undefined` to `null` so the wire
      // envelope shape is deterministic across void and explicit-null handler
      // returns. Without coercion, JSON.stringify would omit the `body` field
      // entirely, producing divergent byte shapes for semantically identical
      // 204 responses.
      expect(reply.body).toBeNull();
    });
  });

  /**
   * Error path: the real `GatewayExceptionFilter` from the DI container
   * routes through the real `DefaultErrorBodyFactory`. The first case
   * proves the duck-typed `isDomainException` marker is honored end-to-end
   * (status from the exception, all fields populated including stack in
   * non-production mode). The second case proves a plain `Error` degrades
   * to a generic `500` with the sanitized `INTERNAL_SERVER_ERROR` code,
   * never leaking the raw thrown message.
   */
  describe('error path via filter', () => {
    it('serializes a FixtureDomainException into a structured 404 envelope', () => {
      const envelope = buildEnvelope(null, {
        meta: {
          requestId: 'req-err-1',
          remoteAddr: '127.0.0.1',
          receivedAt: Date.now(),
          timeoutMs: 30_000,
        },
      });
      const host = buildArgumentsHost(envelope);
      const exception = new FixtureDomainException(404, 'USER_NOT_FOUND', 'User not found', {
        lookupId: '42',
      });

      const reply = filter.catch(exception, host);

      expect(reply.status).toBe(404);
      expect(reply.headers).toEqual({ 'content-type': 'application/problem+json' });
      expect(reply.body?.error).toBe('USER_NOT_FOUND');
      expect(reply.body?.message).toBe('User not found');
      expect(reply.body?.requestId).toBe('req-err-1');
      expect(reply.body?.details).toEqual({ lookupId: '42' });
      expect(typeof reply.body?.stack).toBe('string');
    });

    it('serializes a plain Error into a sanitized 500 envelope', () => {
      const envelope = buildEnvelope(null, {
        meta: {
          requestId: 'req-err-2',
          remoteAddr: '127.0.0.1',
          receivedAt: Date.now(),
          timeoutMs: 30_000,
        },
      });
      const host = buildArgumentsHost(envelope);
      const reply = filter.catch(new Error('raw internal detail'), host);

      expect(reply.status).toBe(500);
      expect(reply.headers).toEqual({ 'content-type': 'application/problem+json' });
      expect(reply.body?.error).toBe('INTERNAL_SERVER_ERROR');
      expect(reply.body?.message).toBe('An unexpected error occurred');
      expect(reply.body?.message).not.toContain('raw internal detail');
      expect(reply.body?.requestId).toBe('req-err-2');
      expect(typeof reply.body?.stack).toBe('string');
    });

    /**
     * Certifies that the duck-typed `IHttpExceptionLike` contract inside
     * `DefaultErrorBodyFactory` correctly recognizes the REAL
     * `@nestjs/common` `HttpException` family end-to-end. The unit spec
     * exercises recognition with a hermetic fixture; this case proves the
     * same code path also handles the actual class hierarchy Nest ships,
     * without a hard import dependency leaking into the SDK runtime.
     */
    it('serializes a real NestJS NotFoundException into a 404 envelope', () => {
      const envelope = buildEnvelope(null, {
        meta: {
          requestId: 'req-err-3',
          remoteAddr: '127.0.0.1',
          receivedAt: Date.now(),
          timeoutMs: 30_000,
        },
      });
      const host = buildArgumentsHost(envelope);
      const reply = filter.catch(new NotFoundException('User not found'), host);

      expect(reply.status).toBe(404);
      expect(reply.headers).toEqual({ 'content-type': 'application/problem+json' });
      expect(reply.body?.error).toBe('NOT_FOUND');
      expect(reply.body?.message).toBe('User not found');
      expect(reply.body?.requestId).toBe('req-err-3');
    });

    /**
     * Certifies handling of `BadRequestException` populated with a
     * `message: string[]` — the shape Nest's `ValidationPipe` produces for
     * aggregated class-validator violations. The factory must join the
     * array into a single human-readable string so the wire envelope stays
     * a flat JSON object (RFC 7807 `detail` is a string, not an array).
     */
    it('serializes a NestJS BadRequestException with array message', () => {
      const envelope = buildEnvelope(null, {
        meta: {
          requestId: 'req-err-4',
          remoteAddr: '127.0.0.1',
          receivedAt: Date.now(),
          timeoutMs: 30_000,
        },
      });
      const host = buildArgumentsHost(envelope);
      const reply = filter.catch(
        new BadRequestException(['email must be an email', 'age must be a number']),
        host,
      );

      expect(reply.status).toBe(400);
      expect(reply.headers).toEqual({ 'content-type': 'application/problem+json' });
      expect(reply.body?.error).toBe('BAD_REQUEST');
      expect(reply.body?.message).toContain('email must be an email');
      expect(reply.body?.message).toContain('age must be a number');
      expect(reply.body?.requestId).toBe('req-err-4');
    });
  });
});
