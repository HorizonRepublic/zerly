import { Reflector } from '@nestjs/core';

import { beforeEach, describe, expect, it, jest } from '@jest/globals';
import { firstValueFrom, of } from 'rxjs';

import { GatewayResponseInterceptor } from './gateway-response.interceptor';

import type { IGatewayReplyBuilder } from '../normalization/contracts/reply-builder.interface';
import type { IStatusResolver } from '../normalization/contracts/status-resolver.interface';
import type { IGatewayHttpMeta } from '../types/gateway-http-meta.interface';
import type { IGatewayReply } from '../types/gateway-reply.interface';
import type { CallHandler, ExecutionContext } from '@nestjs/common';

const buildContext = (handler: () => void): ExecutionContext =>
  ({
    getHandler: () => handler,
  }) as unknown as ExecutionContext;

const buildCallHandler = (value: unknown): CallHandler =>
  ({
    handle: () => of(value),
  }) as unknown as CallHandler;

describe('GatewayResponseInterceptor', () => {
  const httpMeta: IGatewayHttpMeta = { method: 'POST', path: '/users', statusCode: 201 };
  const handler = (): void => undefined;

  let reflector: jest.Mocked<Reflector>;
  let replyBuilder: jest.Mocked<IGatewayReplyBuilder>;
  let statusResolver: jest.Mocked<IStatusResolver>;
  let interceptor: GatewayResponseInterceptor;

  beforeEach(() => {
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

    interceptor = new GatewayResponseInterceptor(reflector, replyBuilder, statusResolver);
  });

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
      interceptor.intercept(buildContext(handler), buildCallHandler({ id: 1 })),
    );

    expect(statusResolver.resolveSuccess).toHaveBeenCalledWith(httpMeta, { id: 1 });
    expect(replyBuilder.success).toHaveBeenCalledWith(201, { id: 1 });
    expect(result).toBe(envelope);
  });

  it('passes the resolved status verbatim to the reply builder', async () => {
    reflector.get.mockReturnValue({ meta: { http: httpMeta } });
    statusResolver.resolveSuccess.mockReturnValue(204);
    replyBuilder.success.mockReturnValue({ status: 204, headers: {}, body: null });

    await firstValueFrom(interceptor.intercept(buildContext(handler), buildCallHandler(null)));

    expect(statusResolver.resolveSuccess).toHaveBeenCalledWith(httpMeta, null);
    expect(replyBuilder.success).toHaveBeenCalledWith(204, null);
  });

  it('passes through without wrapping when metadata is entirely missing', async () => {
    reflector.get.mockReturnValue(undefined);

    const raw = { id: 'passthrough' };
    const result = await firstValueFrom(
      interceptor.intercept(buildContext(handler), buildCallHandler(raw)),
    );

    expect(result).toBe(raw);
    expect(statusResolver.resolveSuccess).not.toHaveBeenCalled();
    expect(replyBuilder.success).not.toHaveBeenCalled();
  });

  it('passes through without wrapping when meta.http is undefined', async () => {
    reflector.get.mockReturnValue({ meta: {} });

    const raw = { id: 'passthrough' };
    const result = await firstValueFrom(
      interceptor.intercept(buildContext(handler), buildCallHandler(raw)),
    );

    expect(result).toBe(raw);
    expect(statusResolver.resolveSuccess).not.toHaveBeenCalled();
    expect(replyBuilder.success).not.toHaveBeenCalled();
  });

  it('passes through when extras exists but meta is undefined', async () => {
    reflector.get.mockReturnValue({});

    const raw = { id: 'passthrough' };
    const result = await firstValueFrom(
      interceptor.intercept(buildContext(handler), buildCallHandler(raw)),
    );

    expect(result).toBe(raw);
    expect(replyBuilder.success).not.toHaveBeenCalled();
  });

  it('reads metadata using PATTERN_EXTRAS_METADATA key and the current handler', async () => {
    reflector.get.mockReturnValue({ meta: { http: httpMeta } });
    statusResolver.resolveSuccess.mockReturnValue(201);
    replyBuilder.success.mockReturnValue({ status: 201, headers: {}, body: null });

    await firstValueFrom(interceptor.intercept(buildContext(handler), buildCallHandler({ id: 1 })));

    expect(reflector.get).toHaveBeenCalledTimes(1);
    const [metadataKey, reflectedHandler] = reflector.get.mock.calls[0] ?? [];

    expect(metadataKey).toBe('microservices:pattern_extras');
    expect(reflectedHandler).toBe(handler);
  });
});
