// Mock @nestjs/common to avoid ESM-only import failures under ts-jest.
// The PascalCase keys are required to match Nest's public API surface; they
// are supplied via computed properties so the `naming-convention` rule
// targeting shorthand method keys is not triggered.
jest.mock('@nestjs/common', () => ({
  ['Injectable']: (): ClassDecorator => (target) => target,
  ['Inject']: (): ParameterDecorator => () => undefined,
  ['Catch']: (): ClassDecorator => (target) => target,
}));

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
    replyBuilder = {
      success: jest.fn(),
      error: jest.fn().mockReturnValue(sampleEnvelope),
    };
    errorBodyFactory = {
      build: jest.fn().mockReturnValue({ status: 404, body: sampleBody }),
    };
    filter = new GatewayExceptionFilter(replyBuilder, errorBodyFactory);
  });

  it('delegates a DomainException-shaped throw to the factory and builder', () => {
    const request = buildRequest('req-1');
    const domainException = {
      isDomainException: true,
      status: 404,
      code: 'USER_NOT_FOUND',
      message: 'User not found',
    };

    const result = filter.catch(domainException, buildHost(request));

    expect(errorBodyFactory.build).toHaveBeenCalledWith(domainException, request);
    expect(replyBuilder.error).toHaveBeenCalledWith(404, sampleBody);
    expect(result).toBe(sampleEnvelope);
  });

  it('delegates a generic Error throw through the same path', () => {
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
    const result = filter.catch(err, buildHost(request));

    expect(errorBodyFactory.build).toHaveBeenCalledWith(err, request);
    expect(replyBuilder.error).toHaveBeenCalledWith(500, {
      error: 'INTERNAL_SERVER_ERROR',
      message: 'boom',
      requestId: 'req-2',
    });
    expect(result).toBe(internalEnvelope);
  });

  it('forwards non-Error throws (string, number, null) to the factory unchanged', () => {
    const request = buildRequest('req-3');

    filter.catch('string throw', buildHost(request));
    filter.catch(42, buildHost(request));
    filter.catch(null, buildHost(request));

    expect(errorBodyFactory.build).toHaveBeenNthCalledWith(1, 'string throw', request);
    expect(errorBodyFactory.build).toHaveBeenNthCalledWith(2, 42, request);
    expect(errorBodyFactory.build).toHaveBeenNthCalledWith(3, null, request);
  });

  it('reads the request via host.switchToRpc().getData()', () => {
    const request = buildRequest('req-4');
    const switchToRpc = jest.fn().mockReturnValue({ getData: () => request });
    const host = { switchToRpc } as unknown as ArgumentsHost;

    filter.catch(new Error('x'), host);

    expect(switchToRpc).toHaveBeenCalledTimes(1);
  });

  it('passes even a missing/undefined request through to the factory', () => {
    filter.catch(new Error('x'), buildHost(undefined));

    expect(errorBodyFactory.build).toHaveBeenCalledWith(expect.any(Error), undefined);
  });
});
