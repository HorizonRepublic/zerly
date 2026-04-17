import { Reflector } from '@nestjs/core';

import { beforeEach, describe, expect, it, jest } from '@jest/globals';
import { firstValueFrom, of, throwError } from 'rxjs';

import { GatewayResponseAccumulator } from '../runtime/gateway-response-accumulator';
import {
  acquireAccumulator,
  getPoolSizeForTesting,
  releaseAccumulator,
} from '../runtime/gateway-response-pool';
import { RESPONSE_ACCUMULATOR_KEY } from '../runtime/response-accumulator-symbol';

import { GatewayResponseInterceptor } from './gateway-response.interceptor';

import type { IGatewayReplyBuilder } from '../normalization/contracts/reply-builder.interface';
import type { IStatusResolver } from '../normalization/contracts/status-resolver.interface';
import type { IGatewayDefaults } from '../types/gateway-defaults.interface';
import type { IGatewayHttpMeta } from '../types/gateway-http-meta.interface';
import type { IGatewayReply } from '../types/gateway-reply.interface';
import type { CallHandler, ExecutionContext } from '@nestjs/common';

type EnvelopeWithAccumulatorSlot = Record<symbol, unknown>;

const buildContext = (
  handler: () => void,
  envelope: EnvelopeWithAccumulatorSlot = {},
): ExecutionContext =>
  ({
    getHandler: () => handler,
    switchToRpc: () => ({
      getData: () => envelope,
    }),
  }) as unknown as ExecutionContext;

const buildCallHandler = (value: unknown): CallHandler =>
  ({
    handle: () => of(value),
  }) as unknown as CallHandler;

const buildThrowingCallHandler = (error: Error): CallHandler =>
  ({
    handle: () => throwError(() => error),
  }) as unknown as CallHandler;

const plantAccumulator = (envelope: EnvelopeWithAccumulatorSlot): GatewayResponseAccumulator => {
  const acc = acquireAccumulator();

  envelope[RESPONSE_ACCUMULATOR_KEY] = acc;

  return acc;
};

describe('GatewayResponseInterceptor', () => {
  const httpMeta: IGatewayHttpMeta = { method: 'POST', path: '/users', statusCode: 201 };
  const handler = (): void => undefined;

  let reflector: jest.Mocked<Reflector>;
  let replyBuilder: jest.Mocked<IGatewayReplyBuilder>;
  let statusResolver: jest.Mocked<IStatusResolver>;
  let sut: GatewayResponseInterceptor;

  beforeEach(() => {
    while (getPoolSizeForTesting() > 0) {
      acquireAccumulator();
    }

    // Under `@jest/globals`, `jest.fn()` with no generic resolves to
    // `Mock<UnknownFunction>`, which is not directly assignable to a
    // `jest.Mocked<T>` slot. Casting through `unknown` is the canonical
    // escape hatch for ad-hoc test doubles backed by a per-method `jest.fn()`.
    reflector = {
      get: jest.fn(),
    } as unknown as jest.Mocked<Reflector>;

    replyBuilder = {
      success: jest.fn(),
      error: jest.fn(),
    } as unknown as jest.Mocked<IGatewayReplyBuilder>;

    statusResolver = {
      resolveSuccess: jest.fn(),
    } as unknown as jest.Mocked<IStatusResolver>;

    sut = new GatewayResponseInterceptor(reflector, replyBuilder, statusResolver, undefined);
  });

  describe('route handler fast path (no accumulator)', () => {
    it('wraps the handler return value through the reply builder', async () => {
      reflector.get.mockReturnValue({ meta: { http: httpMeta } });
      statusResolver.resolveSuccess.mockReturnValue(201);
      const envelope: IGatewayReply<{ id: number }> = {
        status: 201,
        headers: {},
        body: { id: 1 },
      };

      replyBuilder.success.mockReturnValue(envelope);

      const result = await firstValueFrom(
        sut.intercept(buildContext(handler), buildCallHandler({ id: 1 })),
      );

      expect(statusResolver.resolveSuccess).toHaveBeenCalledWith(httpMeta, { id: 1 });
      expect(replyBuilder.success).toHaveBeenCalledWith(201, { id: 1 }, {});
      expect(result).toBe(envelope);
    });

    it('passes the resolved status verbatim to the reply builder', async () => {
      reflector.get.mockReturnValue({ meta: { http: httpMeta } });
      statusResolver.resolveSuccess.mockReturnValue(204);
      replyBuilder.success.mockReturnValue({ status: 204, headers: {}, body: null });

      await firstValueFrom(sut.intercept(buildContext(handler), buildCallHandler(null)));

      expect(statusResolver.resolveSuccess).toHaveBeenCalledWith(httpMeta, null);
      expect(replyBuilder.success).toHaveBeenCalledWith(204, null, {});
    });

    it('pre-acquires and returns an accumulator to the pool even when @GatewayResponse is not injected', async () => {
      reflector.get.mockReturnValue({ meta: { http: httpMeta } });
      statusResolver.resolveSuccess.mockReturnValue(201);
      replyBuilder.success.mockReturnValue({ status: 201, headers: {}, body: null });

      const envelope: EnvelopeWithAccumulatorSlot = {};
      const poolSizeBefore = getPoolSizeForTesting();

      await firstValueFrom(
        sut.intercept(buildContext(handler, envelope), buildCallHandler({ id: 1 })),
      );

      expect(getPoolSizeForTesting()).toBe(poolSizeBefore + 1);
      expect(envelope[RESPONSE_ACCUMULATOR_KEY]).toBeUndefined();
    });
  });

  describe('route handler pass-through branches', () => {
    it('passes through without wrapping when metadata is entirely missing', async () => {
      reflector.get.mockReturnValue(undefined);

      const raw = { id: 'passthrough' };
      const result = await firstValueFrom(
        sut.intercept(buildContext(handler), buildCallHandler(raw)),
      );

      expect(result).toBe(raw);
      expect(statusResolver.resolveSuccess).not.toHaveBeenCalled();
      expect(replyBuilder.success).not.toHaveBeenCalled();
    });

    it('passes through without wrapping when meta.http is undefined', async () => {
      reflector.get.mockReturnValue({ meta: {} });

      const raw = { id: 'passthrough' };
      const result = await firstValueFrom(
        sut.intercept(buildContext(handler), buildCallHandler(raw)),
      );

      expect(result).toBe(raw);
      expect(statusResolver.resolveSuccess).not.toHaveBeenCalled();
      expect(replyBuilder.success).not.toHaveBeenCalled();
    });

    it('passes through when extras exists but meta is undefined', async () => {
      reflector.get.mockReturnValue({});

      const raw = { id: 'passthrough' };
      const result = await firstValueFrom(
        sut.intercept(buildContext(handler), buildCallHandler(raw)),
      );

      expect(result).toBe(raw);
      expect(replyBuilder.success).not.toHaveBeenCalled();
    });

    it('leaves pool size untouched when passing a handler through', async () => {
      reflector.get.mockReturnValue(undefined);

      const poolSizeBefore = getPoolSizeForTesting();

      await firstValueFrom(
        sut.intercept(buildContext(handler), buildCallHandler({ id: 'passthrough' })),
      );

      expect(getPoolSizeForTesting()).toBe(poolSizeBefore);
    });

    it('reads metadata using PATTERN_EXTRAS_METADATA key and the current handler', async () => {
      reflector.get.mockReturnValue({ meta: { http: httpMeta } });
      statusResolver.resolveSuccess.mockReturnValue(201);
      replyBuilder.success.mockReturnValue({ status: 201, headers: {}, body: null });

      await firstValueFrom(sut.intercept(buildContext(handler), buildCallHandler({ id: 1 })));

      expect(reflector.get).toHaveBeenCalledTimes(1);
      const [metadataKey, reflectedHandler] = reflector.get.mock.calls[0] ?? [];

      expect(metadataKey).toBe('microservices:pattern_extras');
      expect(reflectedHandler).toBe(handler);
    });
  });

  describe('route handler with injected @GatewayResponse', () => {
    it('merges accumulator status and headers into the reply', async () => {
      reflector.get.mockReturnValue({ meta: { http: httpMeta } });
      statusResolver.resolveSuccess.mockReturnValue(200);
      replyBuilder.success.mockImplementation((status, body, headers) => ({
        status,
        headers: headers ?? {},
        body: body ?? null,
      }));

      const envelope: EnvelopeWithAccumulatorSlot = {};
      const acc = plantAccumulator(envelope);

      acc.status(201).header('x-foo', 'bar').cookie('sid', 'abc', { httpOnly: true });

      const result = (await firstValueFrom(
        sut.intercept(buildContext(handler, envelope), buildCallHandler({ id: 1 })),
      )) as IGatewayReply<{ id: number }>;

      expect(result.status).toBe(201);
      expect(result.headers['x-foo']).toEqual(['bar']);
      expect(result.headers['set-cookie']).toEqual(['sid=abc; HttpOnly']);
      expect(statusResolver.resolveSuccess).not.toHaveBeenCalled();
    });

    it('falls back to the status resolver when accumulator has headers but no status', async () => {
      reflector.get.mockReturnValue({ meta: { http: httpMeta } });
      statusResolver.resolveSuccess.mockReturnValue(202);
      replyBuilder.success.mockImplementation((status, body, headers) => ({
        status,
        headers: headers ?? {},
        body: body ?? null,
      }));

      const envelope: EnvelopeWithAccumulatorSlot = {};
      const acc = plantAccumulator(envelope);

      acc.header('x-trace', 'abc123');

      const result = (await firstValueFrom(
        sut.intercept(buildContext(handler, envelope), buildCallHandler({ id: 1 })),
      )) as IGatewayReply<{ id: number }>;

      expect(result.status).toBe(202);
      expect(result.headers['x-trace']).toEqual(['abc123']);
      expect(statusResolver.resolveSuccess).toHaveBeenCalledWith(httpMeta, { id: 1 });
    });

    it('releases the accumulator back to the pool on success completion', async () => {
      reflector.get.mockReturnValue({ meta: { http: httpMeta } });
      statusResolver.resolveSuccess.mockReturnValue(200);
      replyBuilder.success.mockReturnValue({ status: 200, headers: {}, body: null });

      const envelope: EnvelopeWithAccumulatorSlot = {};

      plantAccumulator(envelope);
      const poolSizeBefore = getPoolSizeForTesting();

      await firstValueFrom(
        sut.intercept(buildContext(handler, envelope), buildCallHandler({ id: 1 })),
      );

      expect(getPoolSizeForTesting()).toBe(poolSizeBefore + 1);
      expect(envelope[RESPONSE_ACCUMULATOR_KEY]).toBeUndefined();
    });

    it('releases the accumulator via finalize when the handler throws', async () => {
      reflector.get.mockReturnValue({ meta: { http: httpMeta } });

      const envelope: EnvelopeWithAccumulatorSlot = {};
      const acc = plantAccumulator(envelope);

      acc.status(201).header('x-leak', 'should-be-reset');
      const poolSizeBefore = getPoolSizeForTesting();

      await expect(
        firstValueFrom(
          sut.intercept(
            buildContext(handler, envelope),
            buildThrowingCallHandler(new Error('boom')),
          ),
        ),
      ).rejects.toThrow('boom');

      expect(getPoolSizeForTesting()).toBe(poolSizeBefore + 1);
      expect(envelope[RESPONSE_ACCUMULATOR_KEY]).toBeUndefined();

      // Pool-returned instance must be in reset state; re-acquire and pin it.
      const reacquired = acquireAccumulator();

      expect(reacquired).toBe(acc);
      expect(reacquired.statusCode).toBeUndefined();
      expect(Object.keys(reacquired.headers)).toHaveLength(0);
    });
  });

  describe('verifier handler branch', () => {
    it('wraps verifier return values with hardcoded status 200 and empty headers', async () => {
      reflector.get.mockReturnValue({ meta: { verifier: { id: 'jwt' } } });
      replyBuilder.success.mockImplementation((status, body, headers) => ({
        status,
        headers: headers ?? {},
        body: body ?? null,
      }));

      const claims = { sub: 'user-1' };
      const result = (await firstValueFrom(
        sut.intercept(buildContext(handler), buildCallHandler(claims)),
      )) as IGatewayReply<typeof claims>;

      expect(result.status).toBe(200);
      expect(result.headers).toEqual({});
      expect(result.body).toBe(claims);
      expect(statusResolver.resolveSuccess).not.toHaveBeenCalled();
    });

    it('merges accumulator headers but ignores accumulator status (verifier success is always 200)', async () => {
      reflector.get.mockReturnValue({ meta: { verifier: { id: 'jwt' } } });
      replyBuilder.success.mockImplementation((status, body, headers) => ({
        status,
        headers: headers ?? {},
        body: body ?? null,
      }));

      const envelope: EnvelopeWithAccumulatorSlot = {};
      const acc = plantAccumulator(envelope);

      acc.status(418).header('x-verifier', 'true');
      const poolSizeBefore = getPoolSizeForTesting();

      const result = (await firstValueFrom(
        sut.intercept(buildContext(handler, envelope), buildCallHandler({ sub: 'user-1' })),
      )) as IGatewayReply<{ sub: string }>;

      expect(result.status).toBe(200);
      expect(result.headers['x-verifier']).toEqual(['true']);
      expect(getPoolSizeForTesting()).toBe(poolSizeBefore + 1);
      expect(envelope[RESPONSE_ACCUMULATOR_KEY]).toBeUndefined();
    });
  });

  describe('cookie defaults propagation', () => {
    const buildCookieCallHandler = (
      envelope: EnvelopeWithAccumulatorSlot,
      cookieName: string,
      cookieValue: string,
      options?: Parameters<GatewayResponseAccumulator['cookie']>[2],
    ): CallHandler =>
      ({
        handle: () => {
          const acc = envelope[RESPONSE_ACCUMULATOR_KEY] as GatewayResponseAccumulator | undefined;

          acc?.cookie(cookieName, cookieValue, options);

          return of({ id: 1 });
        },
      }) as unknown as CallHandler;

    it('applies module-level cookie defaults when no per-cookie options are given', async () => {
      const defaults: IGatewayDefaults = { cookies: { httpOnly: true, secure: true, path: '/' } };

      reflector.get.mockReturnValue({ meta: { http: httpMeta } });
      statusResolver.resolveSuccess.mockReturnValue(200);
      replyBuilder.success.mockImplementation((status, body, headers) => ({
        status,
        headers: headers ?? {},
        body: body ?? null,
      }));

      const sutWithDefaults = new GatewayResponseInterceptor(
        reflector,
        replyBuilder,
        statusResolver,
        defaults,
      );

      const envelope: EnvelopeWithAccumulatorSlot = {};

      const result = (await firstValueFrom(
        sutWithDefaults.intercept(
          buildContext(handler, envelope),
          buildCookieCallHandler(envelope, 'sid', 'abc'),
        ),
      )) as IGatewayReply<{ id: number }>;

      expect(result.headers['set-cookie']).toEqual(['sid=abc; Path=/; HttpOnly; Secure']);
    });

    it('per-cookie options override module-level defaults', async () => {
      const defaults: IGatewayDefaults = { cookies: { secure: true, path: '/' } };

      reflector.get.mockReturnValue({ meta: { http: httpMeta } });
      statusResolver.resolveSuccess.mockReturnValue(200);
      replyBuilder.success.mockImplementation((status, body, headers) => ({
        status,
        headers: headers ?? {},
        body: body ?? null,
      }));

      const sutWithDefaults = new GatewayResponseInterceptor(
        reflector,
        replyBuilder,
        statusResolver,
        defaults,
      );

      const envelope: EnvelopeWithAccumulatorSlot = {};

      const result = (await firstValueFrom(
        sutWithDefaults.intercept(
          buildContext(handler, envelope),
          buildCookieCallHandler(envelope, 'sid', 'abc', { secure: false, sameSite: 'strict' }),
        ),
      )) as IGatewayReply<{ id: number }>;

      expect(result.headers['set-cookie']).toEqual(['sid=abc; Path=/; SameSite=Strict']);
    });

    it('sets cookieDefaults on a pre-existing accumulator without double-acquiring from pool', async () => {
      const defaults: IGatewayDefaults = { cookies: { httpOnly: true } };

      reflector.get.mockReturnValue({ meta: { http: httpMeta } });
      statusResolver.resolveSuccess.mockReturnValue(200);
      replyBuilder.success.mockReturnValue({ status: 200, headers: {}, body: null });

      const sutWithDefaults = new GatewayResponseInterceptor(
        reflector,
        replyBuilder,
        statusResolver,
        defaults,
      );

      const envelope: EnvelopeWithAccumulatorSlot = {};
      const acc = plantAccumulator(envelope);
      const poolSizeBefore = getPoolSizeForTesting();

      let capturedDefaults: unknown;

      const callHandler: CallHandler = {
        handle: () => {
          capturedDefaults = { ...acc.cookieDefaults };

          return of({ id: 1 });
        },
      };

      await firstValueFrom(sutWithDefaults.intercept(buildContext(handler, envelope), callHandler));

      expect(capturedDefaults).toEqual({ httpOnly: true });
      expect(getPoolSizeForTesting()).toBe(poolSizeBefore + 1);
    });
  });

  it('does not double-release an accumulator planted by external tooling but not consumed', () => {
    // Given: a pass-through handler (no http, no verifier meta) that somehow
    // has an accumulator planted on the envelope. The interceptor's
    // pass-through branch must not try to release it — `finalize` runs only
    // on the wrapping branches, which is the documented contract.
    reflector.get.mockReturnValue(undefined);

    const envelope: EnvelopeWithAccumulatorSlot = {};
    const acc = plantAccumulator(envelope);
    const poolSizeBefore = getPoolSizeForTesting();

    // When
    const result$ = sut.intercept(buildContext(handler, envelope), buildCallHandler({ id: 1 }));

    // Force synchronous drain.
    result$.subscribe();

    // Then: the pool did not grow (nothing released by the interceptor)
    // and the stashed instance is untouched on the envelope for a later
    // interceptor layer to claim. Manually release to keep the pool
    // bookkeeping honest for subsequent tests.
    expect(getPoolSizeForTesting()).toBe(poolSizeBefore);
    expect(envelope[RESPONSE_ACCUMULATOR_KEY]).toBe(acc);

    releaseAccumulator(acc);
  });
});
