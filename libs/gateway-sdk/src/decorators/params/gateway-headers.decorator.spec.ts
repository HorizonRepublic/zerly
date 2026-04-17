import { createMock } from '@golevelup/ts-jest';
import { describe, expect, it } from '@jest/globals';

import { readGatewayEnvelope } from './envelope-accessor';
import { GatewayHeaders } from './gateway-headers.decorator';

import type { IGatewayRequest } from '../../types/gateway-request.interface';
import type { ExecutionContext } from '@nestjs/common';

const buildContext = (headers: Record<string, string>): ExecutionContext =>
  createMock<ExecutionContext>({
    switchToRpc: () =>
      ({
        getData: () => ({ headers }) as Partial<IGatewayRequest> as IGatewayRequest,
      }) as ReturnType<ExecutionContext['switchToRpc']>,
  });

const extractHeaders = (key: string | undefined, context: ExecutionContext): unknown => {
  const headers = readGatewayEnvelope(context).headers;

  return key === undefined ? headers : headers[key.toLowerCase()];
};

describe('GatewayHeaders decorator', () => {
  it('exports the decorator symbol', () => {
    expect(typeof GatewayHeaders).toBe('function');
  });

  it('returns the entire headers map when no key is provided', () => {
    const headers = { authorization: 'Bearer tok', 'content-type': 'application/json' };
    const ctx = buildContext(headers);

    const sut = extractHeaders(undefined, ctx);

    expect(sut).toEqual({
      authorization: 'Bearer tok',
      'content-type': 'application/json',
    });
  });

  it('returns a single header value by lowercase key', () => {
    const ctx = buildContext({ 'x-api-version': '3.0' });

    const sut = extractHeaders('x-api-version', ctx);

    expect(sut).toBe('3.0');
  });

  it('matches case-insensitively when called with mixed case', () => {
    const ctx = buildContext({ 'x-api-version': '3.0' });

    const sut = extractHeaders('X-Api-Version', ctx);

    expect(sut).toBe('3.0');
  });

  it('returns undefined when the header is missing', () => {
    const ctx = buildContext({ 'x-api-version': '3.0' });

    const sut = extractHeaders('authorization', ctx);

    expect(sut).toBeUndefined();
  });

  it('returns undefined for an empty headers map with a key', () => {
    const ctx = buildContext({});

    const sut = extractHeaders('content-type', ctx);

    expect(sut).toBeUndefined();
  });

  it('returns the full empty map when no key is provided and headers are empty', () => {
    const ctx = buildContext({});

    const sut = extractHeaders(undefined, ctx);

    expect(sut).toEqual({});
  });
});
