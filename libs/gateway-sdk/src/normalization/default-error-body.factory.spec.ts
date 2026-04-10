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
});
