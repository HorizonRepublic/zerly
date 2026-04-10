import 'reflect-metadata';

import { PATTERN_EXTRAS_METADATA } from '@nestjs/microservices/constants';

import { describe, expect, it } from '@jest/globals';

import { ApiGateway } from './api-gateway.decorator';

describe('ApiGateway decorator', () => {
  class TestController {
    @ApiGateway({
      pattern: 'users.create',
      method: 'POST',
      path: '/users',
      statusCode: 201,
    })
    public createUser(): { id: number } {
      return { id: 1 };
    }

    @ApiGateway({ pattern: 'users.get', method: 'GET', path: '/users/:id' })
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
});
