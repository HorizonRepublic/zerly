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
});
