import { Logger } from '@nestjs/common';

import { afterEach, beforeEach, describe, expect, it, jest } from '@jest/globals';
import { firstValueFrom } from 'rxjs';

import { GatewayExceptionFilter } from './gateway-exception.filter';

import type { IErrorBodyFactory } from '../normalization/contracts/error-body-factory.interface';
import type { IGatewayReplyBuilder } from '../normalization/contracts/reply-builder.interface';
import type { IGatewayErrorBody } from '../types/gateway-error-body.interface';
import type { IGatewayReply } from '../types/gateway-reply.interface';
import type { IGatewayRequest } from '../types/gateway-request.interface';
import type { ArgumentsHost } from '@nestjs/common';

const buildHost = (data: unknown): ArgumentsHost =>
  ({
    switchToRpc: () => ({
      getData: () => data,
    }),
  }) as unknown as ArgumentsHost;

const buildRequest = (requestId: string): IGatewayRequest => ({
  route: { method: 'GET', path: '/users/:id', matchedPath: '/users/1' },
  params: { id: '1' },
  query: {},
  headers: {},
  body: null,
  meta: {
    requestId,
    remoteAddr: '127.0.0.1',
    receivedAt: 0,
    timeoutMs: 30000,
  },
});

describe('GatewayExceptionFilter', () => {
  let replyBuilder: jest.Mocked<IGatewayReplyBuilder>;
  let errorBodyFactory: jest.Mocked<IErrorBodyFactory>;
  let filter: GatewayExceptionFilter;
  let loggerErrorSpy: jest.SpiedFunction<Logger['error']>;

  const sampleBody: IGatewayErrorBody = {
    error: 'USER_NOT_FOUND',
    message: 'User not found',
    requestId: 'req-1',
  };
  const sampleEnvelope: IGatewayReply<IGatewayErrorBody> = {
    status: 404,
    headers: {},
    body: sampleBody,
  };

  beforeEach(() => {
    // Under `@jest/globals`, `jest.fn()` with no generic resolves to
    // `Mock<UnknownFunction>`, which is not directly assignable to a
    // `jest.Mocked<T>` slot. Casting through `unknown` is the canonical
    // escape hatch for ad-hoc test doubles backed by a per-method `jest.fn()`.
    replyBuilder = {
      success: jest.fn(),
      error: jest.fn().mockReturnValue(sampleEnvelope),
    } as unknown as jest.Mocked<IGatewayReplyBuilder>;
    errorBodyFactory = {
      build: jest.fn().mockReturnValue({ status: 404, body: sampleBody }),
    } as unknown as jest.Mocked<IErrorBodyFactory>;
    filter = new GatewayExceptionFilter(replyBuilder, errorBodyFactory);

    // Silence the per-instance Logger without replacing it — the filter
    // constructs its own `new Logger(GatewayExceptionFilter.name)`, and
    // spying on the prototype reaches that instance transparently.
    loggerErrorSpy = jest.spyOn(Logger.prototype, 'error').mockImplementation(() => {});
  });

  afterEach(() => {
    loggerErrorSpy.mockRestore();
  });

  it('delegates an HttpException-shaped throw to the factory and builder', async () => {
    const request = buildRequest('req-1');
    // Plain object stand-in — the factory is mocked, so the filter never
    // inspects `instanceof HttpException` on this test path.
    const httpExceptionLike = {
      status: 404,
      message: 'User not found',
    };

    const emitted = await firstValueFrom(filter.catch(httpExceptionLike, buildHost(request)));

    expect(errorBodyFactory.build).toHaveBeenCalledWith(httpExceptionLike, request);
    expect(replyBuilder.error).toHaveBeenCalledWith(404, sampleBody);
    expect(emitted).toBe(sampleEnvelope);
  });

  it('does not log 4xx responses', () => {
    // 404 already set up by the default errorBodyFactory mock.
    filter.catch(new Error('not found'), buildHost(buildRequest('req-silent'))).subscribe();

    expect(loggerErrorSpy).not.toHaveBeenCalled();
  });

  it('logs 5xx responses with request context and the raw exception', () => {
    const request = buildRequest('req-boom');

    errorBodyFactory.build.mockReturnValueOnce({
      status: 500,
      body: { error: 'INTERNAL_SERVER_ERROR', message: 'boom', requestId: 'req-boom' },
    });

    const err = new TypeError("Cannot read properties of null (reading 'name')");

    filter.catch(err, buildHost(request)).subscribe();

    expect(loggerErrorSpy).toHaveBeenCalledTimes(1);
    const [payload] = loggerErrorSpy.mock.calls[0] ?? [];

    expect(payload).toMatchObject({
      msg: 'Gateway Handler Error',
      err,
      status: 500,
      pattern: '/users/:id',
      method: 'GET',
      matchedPath: '/users/1',
      requestId: 'req-boom',
      remoteAddr: '127.0.0.1',
    });
  });

  it('logs 5xx even when the request envelope is missing', () => {
    errorBodyFactory.build.mockReturnValueOnce({
      status: 500,
      body: { error: 'INTERNAL_SERVER_ERROR', message: 'boom', requestId: null },
    });

    filter.catch(new Error('x'), buildHost(undefined)).subscribe();

    expect(loggerErrorSpy).toHaveBeenCalledTimes(1);
    const [payload] = loggerErrorSpy.mock.calls[0] ?? [];

    expect(payload).toMatchObject({
      msg: 'Gateway Handler Error',
      status: 500,
      pattern: undefined,
      requestId: undefined,
    });
  });

  it('delegates a generic Error throw through the same path', async () => {
    const request = buildRequest('req-2');

    errorBodyFactory.build.mockReturnValueOnce({
      status: 500,
      body: { error: 'INTERNAL_SERVER_ERROR', message: 'boom', requestId: 'req-2' },
    });
    const internalEnvelope: IGatewayReply<IGatewayErrorBody> = {
      status: 500,
      headers: {},
      body: { error: 'INTERNAL_SERVER_ERROR', message: 'boom', requestId: 'req-2' },
    };

    replyBuilder.error.mockReturnValueOnce(internalEnvelope);

    const err = new Error('boom');
    const emitted = await firstValueFrom(filter.catch(err, buildHost(request)));

    expect(errorBodyFactory.build).toHaveBeenCalledWith(err, request);
    expect(replyBuilder.error).toHaveBeenCalledWith(500, {
      error: 'INTERNAL_SERVER_ERROR',
      message: 'boom',
      requestId: 'req-2',
    });
    expect(emitted).toBe(internalEnvelope);
  });

  it('forwards non-Error throws (string, number, null) to the factory unchanged', () => {
    const request = buildRequest('req-3');

    filter.catch('string throw', buildHost(request)).subscribe();
    filter.catch(42, buildHost(request)).subscribe();
    filter.catch(null, buildHost(request)).subscribe();

    expect(errorBodyFactory.build).toHaveBeenNthCalledWith(1, 'string throw', request);
    expect(errorBodyFactory.build).toHaveBeenNthCalledWith(2, 42, request);
    expect(errorBodyFactory.build).toHaveBeenNthCalledWith(3, null, request);
  });

  it('reads the request via host.switchToRpc().getData()', () => {
    const request = buildRequest('req-4');
    const switchToRpc = jest.fn().mockReturnValue({ getData: () => request });
    const host = { switchToRpc } as unknown as ArgumentsHost;

    filter.catch(new Error('x'), host).subscribe();

    expect(switchToRpc).toHaveBeenCalledTimes(1);
  });

  it('passes even a missing/undefined request through to the factory', () => {
    filter.catch(new Error('x'), buildHost(undefined)).subscribe();

    // `@jest/globals` types reject passing a literal `undefined` slot to
    // `toHaveBeenCalledWith`, so assert the arguments via the mock call record
    // instead of relying on matcher-position argument checking.
    const [error, request] = errorBodyFactory.build.mock.calls.at(-1) ?? [];

    expect(error).toBeInstanceOf(Error);
    expect(request).toBeUndefined();
  });
});
