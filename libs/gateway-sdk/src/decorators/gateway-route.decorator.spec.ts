import 'reflect-metadata';

import { PATTERN_EXTRAS_METADATA } from '@nestjs/microservices/constants';

import { describe, expect, it } from '@jest/globals';

import { GatewayRoute } from './gateway-route.decorator';

describe('GatewayRoute decorator', () => {
  class TestController {
    @GatewayRoute({
      pattern: 'users.create',
      method: 'POST',
      path: '/users',
      statusCode: 201,
      cors: { origins: ['https://app.example.com'], credentials: true },
      rateLimit: { rps: 10, burst: 20, keyBy: ['user:id', 'ip'] },
      headers: { 'cache-control': 'no-store' },
      timeout: 5000,
    })
    public createUser(): { id: number } {
      return { id: 1 };
    }

    @GatewayRoute({ pattern: 'users.get', method: 'GET', path: '/users/:id' })
    public getUser(): { id: number } {
      return { id: 1 };
    }
  }

  it('writes http metadata to PATTERN_EXTRAS_METADATA with explicit statusCode', () => {
    const handler = TestController.prototype.createUser;
    const extras = Reflect.getMetadata(PATTERN_EXTRAS_METADATA, handler) as {
      meta: { http: Record<string, unknown> };
    };

    expect(extras).toEqual({
      meta: {
        http: { method: 'POST', path: '/users', statusCode: 201 },
        cors: { origins: ['https://app.example.com'], credentials: true },
        rateLimit: { rps: 10, burst: 20, keyBy: ['user:id', 'ip'] },
        headers: { 'cache-control': 'no-store' },
        timeout: 5000,
      },
    });
  });

  it('omits statusCode from http metadata when not provided', () => {
    const handler = TestController.prototype.getUser;
    const extras = Reflect.getMetadata(PATTERN_EXTRAS_METADATA, handler) as {
      meta: { http: Record<string, unknown> };
    };

    expect(extras).toEqual({
      meta: {
        http: { method: 'GET', path: '/users/:id' },
      },
    });
    expect(extras.meta.http).not.toHaveProperty('statusCode');
  });

  it('omits cors, rateLimit, headers, timeout from metadata when not provided', () => {
    const handler = TestController.prototype.getUser;
    const extras = Reflect.getMetadata(PATTERN_EXTRAS_METADATA, handler) as {
      meta: Record<string, unknown>;
    };

    expect(extras.meta).not.toHaveProperty('cors');
    expect(extras.meta).not.toHaveProperty('rateLimit');
    expect(extras.meta).not.toHaveProperty('headers');
    expect(extras.meta).not.toHaveProperty('timeout');
  });

  describe('CORS wildcard + credentials guard', () => {
    it('allows an explicit origin with credentials', () => {
      expect(() => {
        class OkController {
          @GatewayRoute({
            pattern: 'ok.explicit',
            method: 'GET',
            path: '/ok',
            cors: { origins: ['https://app.example.com'], credentials: true },
          })
          public handler(): void {}
        }

        return OkController;
      }).not.toThrow();
    });

    it('allows wildcard without credentials', () => {
      expect(() => {
        class OkController {
          @GatewayRoute({
            pattern: 'ok.wildcard',
            method: 'GET',
            path: '/ok',
            cors: { origins: ['*'] },
          })
          public handler(): void {}
        }

        return OkController;
      }).not.toThrow();
    });

    it('rejects wildcard combined with credentials: true at decoration time', () => {
      expect(() => {
        class BadController {
          @GatewayRoute({
            pattern: 'bad.wildcard.creds',
            method: 'POST',
            path: '/bad',
            cors: { origins: ['*'], credentials: true },
          })
          public handler(): void {}
        }

        return BadController;
      }).toThrow(/cannot be combined with cors.origins: '\*'/);
    });
  });

  describe('rateLimit shape guard', () => {
    it('rejects rps: 0 at decoration time', () => {
      expect(() => {
        class BadController {
          @GatewayRoute({
            pattern: 'bad.rl.zero',
            method: 'POST',
            path: '/bad-zero',
            rateLimit: { rps: 0 },
          })
          public handler(): void {}
        }

        return BadController;
      }).toThrow(/rateLimit\.rps must be a positive integer/);
    });

    it('rejects negative burst at decoration time', () => {
      expect(() => {
        class BadController {
          @GatewayRoute({
            pattern: 'bad.rl.burst',
            method: 'POST',
            path: '/bad-burst',
            rateLimit: { rps: 10, burst: -1 },
          })
          public handler(): void {}
        }

        return BadController;
      }).toThrow(/rateLimit\.burst must be a non-negative integer/);
    });

    it('accepts a well-formed rateLimit block', () => {
      expect(() => {
        class OkController {
          @GatewayRoute({
            pattern: 'ok.rl',
            method: 'GET',
            path: '/ok-rl',
            rateLimit: { rps: 1, burst: 0 },
          })
          public handler(): void {}
        }

        return OkController;
      }).not.toThrow();
    });
  });
});
