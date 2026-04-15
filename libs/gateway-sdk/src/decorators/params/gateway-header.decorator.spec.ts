import { createMock } from '@golevelup/ts-jest';
import { describe, expect, it } from '@jest/globals';

import { readGatewayEnvelope } from './envelope-accessor';
import { GatewayHeader } from './gateway-header.decorator';

import type { IGatewayRequest } from '../../types/gateway-request.interface';
import type { ExecutionContext } from '@nestjs/common';

const extractHeader = (name: string, context: ExecutionContext): string | undefined =>
  readGatewayEnvelope(context).headers[name.toLowerCase()];

const buildContext = (headers: Record<string, string>): ExecutionContext =>
  createMock<ExecutionContext>({
    switchToRpc: () =>
      ({
        getData: () => ({ headers }) as Partial<IGatewayRequest> as IGatewayRequest,
      }) as ReturnType<ExecutionContext['switchToRpc']>,
  });

describe('GatewayHeader decorator', () => {
  it('exports the decorator symbol', () => {
    expect(typeof GatewayHeader).toBe('function');
  });

  it('returns the value when the header is present (lowercase name)', () => {
    const ctx = buildContext({ 'x-api-version': '2.0' });

    const sut = extractHeader('x-api-version', ctx);

    expect(sut).toBe('2.0');
  });

  it('matches the header case-insensitively when called with mixed case', () => {
    const ctx = buildContext({ 'x-api-version': '2.0' });

    const sut = extractHeader('X-Api-Version', ctx);

    expect(sut).toBe('2.0');
  });

  it('returns undefined when the header is missing', () => {
    const ctx = buildContext({ 'x-api-version': '2.0' });

    const sut = extractHeader('x-request-id', ctx);

    expect(sut).toBeUndefined();
  });

  it('returns undefined for an empty headers map', () => {
    const ctx = buildContext({});

    const sut = extractHeader('authorization', ctx);

    expect(sut).toBeUndefined();
  });
});
