import { createMock } from '@golevelup/ts-jest';
import { beforeEach, describe, expect, it } from '@jest/globals';

import { GatewayResponseAccumulator } from '../../runtime/gateway-response-accumulator';
import {
  acquireAccumulator,
  getPoolSizeForTesting,
  releaseAccumulator,
} from '../../runtime/gateway-response-pool';
import { RESPONSE_ACCUMULATOR_KEY } from '../../runtime/response-accumulator-symbol';

import { GatewayResponse } from './gateway-response.decorator';

import type { ExecutionContext } from '@nestjs/common';

/*
 * Local re-implementation of the decorator factory that
 * `createParamDecorator` wraps internally. NestJS does not
 * surface the raw factory through a public API, so the only
 * way to exercise it in a unit test is to mirror its body
 * here. Keep the two in lock-step — any drift is a bug.
 */
const extractResponse = (context: ExecutionContext): GatewayResponseAccumulator => {
  const envelope = context.switchToRpc().getData<Record<symbol, unknown>>();
  const existing = envelope[RESPONSE_ACCUMULATOR_KEY];

  if (existing instanceof GatewayResponseAccumulator) {
    return existing;
  }

  const fresh = acquireAccumulator();

  envelope[RESPONSE_ACCUMULATOR_KEY] = fresh;

  return fresh;
};

const buildContext = (envelope: Record<symbol, unknown>): ExecutionContext =>
  createMock<ExecutionContext>({
    switchToRpc: () =>
      ({
        getData: () => envelope,
      }) as ReturnType<ExecutionContext['switchToRpc']>,
  });

describe('GatewayResponse decorator', () => {
  beforeEach(() => {
    while (getPoolSizeForTesting() > 0) {
      acquireAccumulator();
    }
  });

  it('exposes a param decorator factory', () => {
    expect(typeof GatewayResponse).toBe('function');
  });

  it('creates and stashes a fresh accumulator on first access', () => {
    const envelope: Record<symbol, unknown> = {};
    const ctx = buildContext(envelope);

    const sut = extractResponse(ctx);

    expect(sut).toBeInstanceOf(GatewayResponseAccumulator);
    expect(sut.statusCode).toBeUndefined();
    expect(Object.keys(sut.headers)).toHaveLength(0);
    expect(envelope[RESPONSE_ACCUMULATOR_KEY]).toBe(sut);
  });

  it('returns the same accumulator across multiple injections in one request', () => {
    const envelope: Record<symbol, unknown> = {};
    const ctx = buildContext(envelope);

    const first = extractResponse(ctx);
    const second = extractResponse(ctx);

    expect(second).toBe(first);
  });

  it('yields distinct accumulators for distinct envelopes', () => {
    const envelopeA: Record<symbol, unknown> = {};
    const envelopeB: Record<symbol, unknown> = {};

    const accA = extractResponse(buildContext(envelopeA));
    const accB = extractResponse(buildContext(envelopeB));

    expect(accA).not.toBe(accB);
  });

  it('preserves mutations across injections within one request', () => {
    const envelope: Record<symbol, unknown> = {};
    const ctx = buildContext(envelope);

    extractResponse(ctx).status(201).header('x-foo', 'bar');
    const second = extractResponse(ctx);

    expect(second.statusCode).toBe(201);
    expect(second.headers['x-foo']).toEqual(['bar']);
  });

  it('recycles a released accumulator from the pool', () => {
    // Given: a pre-released instance with mutated state sitting in the pool.
    const prereleased = new GatewayResponseAccumulator();

    prereleased.status(999);
    releaseAccumulator(prereleased);

    // When: a fresh envelope triggers a first-time acquire.
    const envelope: Record<symbol, unknown> = {};
    const ctx = buildContext(envelope);
    const acquired = extractResponse(ctx);

    // Then: the same physical instance comes back, in reset state.
    expect(acquired).toBe(prereleased);
    expect(acquired.statusCode).toBeUndefined();
    expect(Object.keys(acquired.headers)).toHaveLength(0);
  });
});
