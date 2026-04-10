import { readGatewayEnvelope } from './envelope-accessor';

import type { IGatewayRequest } from '../../types/gateway-request.interface';
import type { ExecutionContext } from '@nestjs/common';

const buildContext = (envelope: IGatewayRequest): ExecutionContext =>
  ({
    switchToRpc: () => ({
      getData: () => envelope,
    }),
  }) as unknown as ExecutionContext;

describe('readGatewayEnvelope', () => {
  it('returns the envelope from the RPC context', () => {
    const envelope: IGatewayRequest<{ email: string }> = {
      route: { method: 'POST', path: '/users', matchedPath: '/users' },
      params: {},
      query: {},
      headers: {},
      body: { email: 'a@b.c' },
      meta: {
        requestId: 'r1',
        remoteAddr: '127.0.0.1',
        receivedAt: 0,
        timeoutMs: 30000,
      },
    };

    expect(readGatewayEnvelope(buildContext(envelope))).toBe(envelope);
  });

  it('preserves the generic TBody type narrowing', () => {
    interface ITestBody {
      readonly hello: string;
    }
    const envelope: IGatewayRequest<ITestBody> = {
      route: { method: 'GET', path: '/', matchedPath: '/' },
      params: {},
      query: {},
      headers: {},
      body: { hello: 'world' },
      meta: {
        requestId: 'r2',
        remoteAddr: '0.0.0.0',
        receivedAt: 0,
        timeoutMs: 1000,
      },
    };

    const result = readGatewayEnvelope<ITestBody>(buildContext(envelope));

    expect(result.body.hello).toBe('world');
  });
});
