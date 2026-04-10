import { describe, expect, it } from '@jest/globals';

import { DefaultErrorBodyFactory } from './default-error-body.factory';

import type { IGatewayRequest } from '../types/gateway-request.interface';

describe('DefaultErrorBodyFactory', () => {
  const request: IGatewayRequest = {
    route: { method: 'GET', path: '/users/:id', matchedPath: '/users/1' },
    params: { id: '1' },
    query: {},
    headers: {},
    body: null,
    meta: {
      requestId: 'req-abc',
      remoteAddr: '127.0.0.1',
      receivedAt: 0,
      timeoutMs: 30000,
    },
  };

  describe('with DomainException-shaped error', () => {
    const domainError = {
      isDomainException: true,
      status: 404,
      code: 'USER_NOT_FOUND',
      message: 'User not found',
      details: { userId: '1' },
      stack: 'Error: User not found\n  at ...',
    };

    it('extracts status from the exception', () => {
      const factory = new DefaultErrorBodyFactory(true);

      expect(factory.build(domainError, request).status).toBe(404);
    });

    it('populates error code from exception.code', () => {
      const factory = new DefaultErrorBodyFactory(true);

      expect(factory.build(domainError, request).body.error).toBe('USER_NOT_FOUND');
    });

    it('includes details when present', () => {
      const factory = new DefaultErrorBodyFactory(true);

      expect(factory.build(domainError, request).body.details).toEqual({ userId: '1' });
    });

    it('omits stack trace in production mode', () => {
      const factory = new DefaultErrorBodyFactory(true);

      expect(factory.build(domainError, request).body.stack).toBeUndefined();
    });

    it('includes stack trace in non-production mode', () => {
      const factory = new DefaultErrorBodyFactory(false);

      expect(factory.build(domainError, request).body.stack).toContain('User not found');
    });

    it('echoes requestId from the request', () => {
      const factory = new DefaultErrorBodyFactory(true);

      expect(factory.build(domainError, request).body.requestId).toBe('req-abc');
    });
  });

  describe('with generic Error', () => {
    it('returns 500 with INTERNAL_SERVER_ERROR code', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const result = factory.build(new Error('boom'), request);

      expect(result.status).toBe(500);
      expect(result.body.error).toBe('INTERNAL_SERVER_ERROR');
    });

    it('hides the original message in production', () => {
      const factory = new DefaultErrorBodyFactory(true);

      expect(factory.build(new Error('boom'), request).body.message).toBe(
        'An unexpected error occurred',
      );
    });

    it('includes stack in dev mode for generic Error', () => {
      const factory = new DefaultErrorBodyFactory(false);

      expect(factory.build(new Error('boom'), request).body.stack).toContain('boom');
    });
  });

  describe('with non-Error throws', () => {
    it('handles string throws', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const result = factory.build('some string', request);

      expect(result.status).toBe(500);
      expect(result.body.error).toBe('INTERNAL_SERVER_ERROR');
    });

    it('handles null throws', () => {
      const factory = new DefaultErrorBodyFactory(true);

      expect(factory.build(null, request).status).toBe(500);
    });

    it('handles number throws', () => {
      const factory = new DefaultErrorBodyFactory(true);

      expect(factory.build(42, request).status).toBe(500);
    });
  });

  describe('with NestJS HttpException-shaped error', () => {
    /**
     * Hermetic duck-typed stand-in for NestJS's `HttpException`.
     * @remarks
     * Kept intentionally free of a `@nestjs/common` import so this unit test
     * exercises the factory's duck-type recognition contract in isolation;
     * the real-class verification lives in the integration spec. The response
     * type is carried as a generic parameter so each instance has a
     * monomorphic `getResponse()` return type — this keeps the fixture
     * compatible with `sonarjs/function-return-type` while still allowing the
     * suite to exercise both string and object response shapes via distinct
     * instantiations.
     * @template TResponse The concrete shape of the response payload, either a
     *   plain string body or a structured `Record` mirroring NestJS's built-in
     *   HTTP exception JSON output.
     */
    class FixtureHttpException<
      TResponse extends string | Readonly<Record<string, unknown>>,
    > extends Error {
      public constructor(
        private readonly response: TResponse,
        private readonly status: number,
        name = 'HttpException',
      ) {
        super(typeof response === 'string' ? response : String(response['message'] ?? ''));
        this.name = name;
      }

      public getStatus(): number {
        return this.status;
      }

      public getResponse(): TResponse {
        return this.response;
      }
    }

    it('extracts status from getStatus()', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const exception = new FixtureHttpException(
        { statusCode: 404, message: 'User not found', error: 'Not Found' },
        404,
        'NotFoundException',
      );

      expect(factory.build(exception, request).status).toBe(404);
    });

    it('normalizes response.error "Not Found" into NOT_FOUND code', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const exception = new FixtureHttpException(
        { statusCode: 404, message: 'User not found', error: 'Not Found' },
        404,
        'NotFoundException',
      );

      expect(factory.build(exception, request).body.error).toBe('NOT_FOUND');
    });

    it('uses response.message as the body message', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const exception = new FixtureHttpException(
        { statusCode: 404, message: 'User not found', error: 'Not Found' },
        404,
        'NotFoundException',
      );

      expect(factory.build(exception, request).body.message).toBe('User not found');
    });

    it('joins array messages from ValidationPipe with ", "', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const exception = new FixtureHttpException(
        {
          statusCode: 400,
          message: ['email must be an email', 'age must be a number'],
          error: 'Bad Request',
        },
        400,
        'BadRequestException',
      );

      expect(factory.build(exception, request).body.message).toBe(
        'email must be an email, age must be a number',
      );
    });

    it('handles a plain-string response body', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const exception = new FixtureHttpException(
        'Teapot brewing coffee',
        418,
        'ImATeapotException',
      );
      const result = factory.build(exception, request);

      expect(result.status).toBe(418);
      expect(result.body.message).toBe('Teapot brewing coffee');
    });

    it('projects extra response fields into details', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const exception = new FixtureHttpException(
        {
          statusCode: 418,
          message: 'Short and stout',
          error: 'Im a teapot',
          teapotId: 'tp-42',
          brew: 'earl-grey',
        },
        418,
        'ImATeapotException',
      );

      expect(factory.build(exception, request).body.details).toEqual({
        teapotId: 'tp-42',
        brew: 'earl-grey',
      });
    });

    it('derives error code from class name when response.error is missing', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const exception = new FixtureHttpException(
        { statusCode: 599, message: 'Boom' },
        599,
        'CustomHttpException',
      );

      expect(factory.build(exception, request).body.error).toBe('CUSTOM_HTTP_EXCEPTION');
    });

    it('omits stack trace in production mode', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const exception = new FixtureHttpException(
        { statusCode: 404, message: 'x', error: 'Not Found' },
        404,
        'NotFoundException',
      );

      expect(factory.build(exception, request).body.stack).toBeUndefined();
    });

    it('includes stack trace in non-production mode', () => {
      const factory = new DefaultErrorBodyFactory(false);
      const exception = new FixtureHttpException(
        { statusCode: 404, message: 'x', error: 'Not Found' },
        404,
        'NotFoundException',
      );

      expect(factory.build(exception, request).body.stack).toBeDefined();
    });

    it('takes precedence over the generic Error fallback', () => {
      const factory = new DefaultErrorBodyFactory(true);
      const exception = new FixtureHttpException(
        { statusCode: 403, message: 'Nope', error: 'Forbidden' },
        403,
        'ForbiddenException',
      );
      const result = factory.build(exception, request);

      expect(result.status).toBe(403);
      expect(result.body.error).not.toBe('INTERNAL_SERVER_ERROR');
      expect(result.body.error).toBe('FORBIDDEN');
    });
  });
});
