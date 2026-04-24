import { describe, it, expect } from '@jest/globals';

import { mergeRouteDefaults } from './merge-route-defaults';

import type { IGatewayDefaults } from '../types';

describe('mergeRouteDefaults', () => {
  const defaults: IGatewayDefaults = {
    cors: { origins: ['https://global.com'], credentials: true },
    rateLimit: { rps: 100, keyBy: ['ip'] },
    headers: { 'x-frame-options': 'DENY', 'x-content-type-options': 'nosniff' },
    timeout: 30_000,
  };

  it('should use defaults when route has no overrides', () => {
    const route = { http: { method: 'GET', path: '/users' } };

    const result = mergeRouteDefaults(defaults, route);

    expect(result['cors']).toEqual(defaults.cors);
    expect(result['rateLimit']).toEqual(defaults.rateLimit);
    expect(result['headers']).toEqual(defaults.headers);
    expect(result['timeout']).toBe(30_000);
  });

  it('should shallow-replace cors when route overrides it', () => {
    const route = {
      http: { method: 'GET', path: '/users' },
      cors: { origins: ['https://specific.com'] },
    };

    const result = mergeRouteDefaults(defaults, route);

    expect(result['cors']).toEqual({ origins: ['https://specific.com'] });
  });

  it('should shallow-replace rateLimit when route overrides it', () => {
    const route = {
      http: { method: 'POST', path: '/login' },
      rateLimit: { rps: 5 },
    };

    const result = mergeRouteDefaults(defaults, route);

    expect(result['rateLimit']).toEqual({ rps: 5 });
  });

  it('should deep-merge headers per key', () => {
    const route = {
      http: { method: 'GET', path: '/users' },
      headers: { 'cache-control': 'no-store' },
    };

    const result = mergeRouteDefaults(defaults, route);

    expect(result['headers']).toEqual({
      'x-frame-options': 'DENY',
      'x-content-type-options': 'nosniff',
      'cache-control': 'no-store',
    });
  });

  it('should allow route headers to override same key from defaults', () => {
    const route = {
      http: { method: 'GET', path: '/users' },
      headers: { 'x-frame-options': 'SAMEORIGIN' },
    };

    const result = mergeRouteDefaults(defaults, route);

    expect((result['headers'] as Record<string, string>)['x-frame-options']).toBe('SAMEORIGIN');
  });

  it('should override timeout with route value', () => {
    const route = { http: { method: 'GET', path: '/users' }, timeout: 5000 };

    const result = mergeRouteDefaults(defaults, route);

    expect(result['timeout']).toBe(5000);
  });

  it('should not include cookies in merged output', () => {
    const withCookies: IGatewayDefaults = { ...defaults, cookies: { secure: true } };
    const route = { http: { method: 'GET', path: '/users' } };

    const result = mergeRouteDefaults(withCookies, route);

    expect(result).not.toHaveProperty('cookies');
  });

  it('should pass through http and auth unchanged', () => {
    const route = {
      http: { method: 'POST', path: '/users', statusCode: 201 },
      auth: { verifier: 'jwt', optional: false },
    };

    const result = mergeRouteDefaults(defaults, route);

    expect(result['http']).toEqual(route.http);
    expect(result['auth']).toEqual(route.auth);
  });

  it('should handle empty defaults gracefully', () => {
    const route = {
      http: { method: 'GET', path: '/users' },
      cors: { origins: ['https://specific.com'] },
    };

    const result = mergeRouteDefaults({}, route);

    expect(result['cors']).toEqual({ origins: ['https://specific.com'] });
    expect(result).not.toHaveProperty('rateLimit');
    expect(result).not.toHaveProperty('timeout');
  });

  describe('rateLimit.store inheritance', () => {
    it('inherits store from forRoot default when per-route omits rateLimit', () => {
      const storeDefaults: IGatewayDefaults = {
        rateLimit: { rps: 100, store: 'nats-kv' },
      };
      const route = { http: { method: 'GET', path: '/users' } };

      const result = mergeRouteDefaults(storeDefaults, route);

      expect(result['rateLimit']).toEqual({ rps: 100, store: 'nats-kv' });
    });

    it('per-route rateLimit shallow-replaces default — store NOT inherited', () => {
      const storeDefaults: IGatewayDefaults = {
        rateLimit: { rps: 100, store: 'nats-kv' },
      };
      const route = {
        http: { method: 'POST', path: '/login' },
        rateLimit: { rps: 10 },
      };

      const result = mergeRouteDefaults(storeDefaults, route);

      expect(result['rateLimit']).toEqual({ rps: 10 });
    });
  });
});
