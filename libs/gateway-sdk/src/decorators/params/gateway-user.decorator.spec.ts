import { faker } from '@faker-js/faker';
import { createMock } from '@golevelup/ts-jest';
import { describe, expect, it } from '@jest/globals';

import { readGatewayEnvelope } from './envelope-accessor';
import { GatewayUser } from './gateway-user.decorator';

import type { IGatewayRequest } from '../../types/gateway-request.interface';
import type { ExecutionContext } from '@nestjs/common';

interface ITestUser {
  readonly id: string;
  readonly email: string;
}

const extractAuth = (context: ExecutionContext): unknown =>
  readGatewayEnvelope<unknown>(context).auth;

const buildEnvelope = <TAuth>(auth: TAuth): IGatewayRequest<unknown, TAuth> => ({
  route: { method: 'GET', path: '/users/me', matchedPath: '/users/me' },
  params: {},
  query: {},
  headers: {},
  body: null,
  meta: {
    requestId: faker.string.uuid(),
    remoteAddr: '127.0.0.1',
    receivedAt: 0,
    timeoutMs: 30000,
  },
  auth,
});

const buildContext = (envelope: IGatewayRequest): ExecutionContext =>
  createMock<ExecutionContext>({
    switchToRpc: () =>
      ({
        getData: () => envelope,
      }) as ReturnType<ExecutionContext['switchToRpc']>,
  });

describe('GatewayUser decorator', () => {
  it('returns the verifier claims from envelope.auth', () => {
    expect(GatewayUser).toBeDefined();

    const user: ITestUser = {
      id: faker.string.uuid(),
      email: faker.internet.email(),
    };
    const envelope = buildEnvelope<ITestUser>(user);
    const sut = extractAuth;

    const result = sut(buildContext(envelope));

    expect(result).toBe(user);
  });

  it('returns undefined when envelope omits the auth field', () => {
    const envelope: IGatewayRequest = {
      route: { method: 'GET', path: '/public', matchedPath: '/public' },
      params: {},
      query: {},
      headers: {},
      body: null,
      meta: {
        requestId: faker.string.uuid(),
        remoteAddr: '127.0.0.1',
        receivedAt: 0,
        timeoutMs: 30000,
      },
    };
    const sut = extractAuth;

    const result = sut(buildContext(envelope));

    expect(result).toBeUndefined();
  });

  it('returns undefined when envelope.auth is explicitly undefined', () => {
    const envelope = buildEnvelope<ITestUser | undefined>(undefined);
    const sut = extractAuth;

    const result = sut(buildContext(envelope));

    expect(result).toBeUndefined();
  });
});
