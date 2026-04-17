import { createMock } from '@golevelup/ts-jest';
import { describe, expect, it } from '@jest/globals';

import { readGatewayEnvelope } from './envelope-accessor';
import { GatewayQuery } from './gateway-query.decorator';

import type { IGatewayRequest } from '../../types/gateway-request.interface';
import type { ExecutionContext } from '@nestjs/common';

const buildContext = (query: Record<string, string | readonly string[]>): ExecutionContext =>
  createMock<ExecutionContext>({
    switchToRpc: () =>
      ({
        getData: () => ({ query }) as Partial<IGatewayRequest> as IGatewayRequest,
      }) as ReturnType<ExecutionContext['switchToRpc']>,
  });

const extractQuery = (key: string | undefined, context: ExecutionContext): unknown => {
  const envelope = readGatewayEnvelope(context);

  return key === undefined ? envelope.query : envelope.query[key];
};

describe('GatewayQuery decorator', () => {
  it('exports the decorator symbol', () => {
    expect(typeof GatewayQuery).toBe('function');
  });

  it('returns the entire query map when no key is provided', () => {
    const query = { page: '1', limit: '20' };
    const ctx = buildContext(query);

    const sut = extractQuery(undefined, ctx);

    expect(sut).toEqual({ page: '1', limit: '20' });
  });

  it('returns a single string value for a present key', () => {
    const ctx = buildContext({ page: '3', sort: 'name' });

    const sut = extractQuery('page', ctx);

    expect(sut).toBe('3');
  });

  it('returns an array for repeated query keys', () => {
    const ctx = buildContext({ tag: ['alpha', 'beta'] });

    const sut = extractQuery('tag', ctx);

    expect(sut).toEqual(['alpha', 'beta']);
  });

  it('returns undefined when the key is absent', () => {
    const ctx = buildContext({ page: '1' });

    const sut = extractQuery('missing', ctx);

    expect(sut).toBeUndefined();
  });

  it('returns undefined for an empty query map', () => {
    const ctx = buildContext({});

    const sut = extractQuery('anything', ctx);

    expect(sut).toBeUndefined();
  });

  it('returns the full empty map when no key is provided and query is empty', () => {
    const ctx = buildContext({});

    const sut = extractQuery(undefined, ctx);

    expect(sut).toEqual({});
  });
});
