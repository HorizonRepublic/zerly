import { faker } from '@faker-js/faker';
import { createMock } from '@golevelup/ts-jest';
import { describe, expect, it } from '@jest/globals';

import { readGatewayEnvelope } from './envelope-accessor';
import { GatewayMeta } from './gateway-meta.decorator';

import type { IGatewayRequestMeta } from '../../types/gateway-request-meta.interface';
import type { IGatewayRequest } from '../../types/gateway-request.interface';
import type { ExecutionContext } from '@nestjs/common';

const buildContext = (meta: IGatewayRequestMeta): ExecutionContext =>
  createMock<ExecutionContext>({
    switchToRpc: () =>
      ({
        getData: () => ({ meta }) as Partial<IGatewayRequest> as IGatewayRequest,
      }) as ReturnType<ExecutionContext['switchToRpc']>,
  });

const extractMeta = (context: ExecutionContext): IGatewayRequestMeta =>
  readGatewayEnvelope(context).meta;

describe('GatewayMeta decorator', () => {
  const baseMeta: IGatewayRequestMeta = {
    requestId: faker.string.ulid(),
    remoteAddr: faker.internet.ip(),
    receivedAt: 1713196800000,
    timeoutMs: 30000,
  };

  it('exports the decorator symbol', () => {
    expect(typeof GatewayMeta).toBe('function');
  });

  it('returns the full meta object', () => {
    const ctx = buildContext(baseMeta);

    const sut = extractMeta(ctx);

    expect(sut).toEqual(baseMeta);
  });

  it('includes traceparent when present', () => {
    const metaWithTrace: IGatewayRequestMeta = {
      ...baseMeta,
      traceparent: '00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01',
    };
    const ctx = buildContext(metaWithTrace);

    const sut = extractMeta(ctx);

    expect(sut.traceparent).toBe('00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01');
  });

  it('returns meta without traceparent when it is absent', () => {
    const ctx = buildContext(baseMeta);

    const sut = extractMeta(ctx);

    expect(sut.traceparent).toBeUndefined();
    expect(sut.requestId).toBe(baseMeta.requestId);
  });

  it('preserves all numeric fields verbatim', () => {
    const ctx = buildContext(baseMeta);

    const sut = extractMeta(ctx);

    expect(sut.receivedAt).toBe(1713196800000);
    expect(sut.timeoutMs).toBe(30000);
  });
});
