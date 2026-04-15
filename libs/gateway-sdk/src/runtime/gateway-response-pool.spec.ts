import { beforeEach, describe, expect, it } from '@jest/globals';

import { GatewayResponseAccumulator } from './gateway-response-accumulator';
import {
  acquireAccumulator,
  getPoolSizeForTesting,
  releaseAccumulator,
} from './gateway-response-pool';

describe('gateway-response-pool', () => {
  beforeEach(() => {
    while (getPoolSizeForTesting() > 0) {
      acquireAccumulator();
    }
  });

  it('returns a fresh accumulator when the pool is empty', () => {
    const sut = acquireAccumulator();

    expect(sut).toBeInstanceOf(GatewayResponseAccumulator);
    expect(sut.statusCode).toBeUndefined();
    expect(Object.keys(sut.headers)).toHaveLength(0);
  });

  it('recycles a released accumulator on the next acquire (reference equal)', () => {
    const first = acquireAccumulator();

    releaseAccumulator(first);

    const second = acquireAccumulator();

    expect(second).toBe(first);
  });

  it('resets state before handing an accumulator back out', () => {
    const first = acquireAccumulator();

    first.status(201).header('x-leak', 'value').cookie('sid', 'abc');
    releaseAccumulator(first);

    const second = acquireAccumulator();

    expect(second.statusCode).toBeUndefined();
    expect(Object.keys(second.headers)).toHaveLength(0);
  });

  it('does not grow unboundedly beyond POOL_MAX', () => {
    const instances: GatewayResponseAccumulator[] = [];

    for (let i = 0; i < 2048; i++) {
      instances.push(new GatewayResponseAccumulator());
    }

    for (const inst of instances) {
      releaseAccumulator(inst);
    }

    expect(getPoolSizeForTesting()).toBeLessThanOrEqual(1024);
  });

  it('serves acquirers in LIFO order under healthy load', () => {
    const a = acquireAccumulator();
    const b = acquireAccumulator();
    const c = acquireAccumulator();

    releaseAccumulator(a);
    releaseAccumulator(b);
    releaseAccumulator(c);

    expect(acquireAccumulator()).toBe(c);
    expect(acquireAccumulator()).toBe(b);
    expect(acquireAccumulator()).toBe(a);
  });
});
