import { describe, expect, it } from '@jest/globals';

import { GatewayResponseAccumulator } from './gateway-response-accumulator';

describe('GatewayResponseAccumulator', () => {
  describe('status', () => {
    it('stores the status code and returns this for chaining', () => {
      const sut = new GatewayResponseAccumulator();

      const result = sut.status(201);

      expect(sut.statusCode).toBe(201);
      expect(result).toBe(sut);
    });

    it('overwrites a previously-set status', () => {
      const sut = new GatewayResponseAccumulator();

      sut.status(201).status(202);

      expect(sut.statusCode).toBe(202);
    });
  });

  describe('header', () => {
    it('stores a single value under a lowercase key', () => {
      const sut = new GatewayResponseAccumulator();

      sut.header('X-Api-Version', '2.0');

      expect(sut.headers['x-api-version']).toEqual(['2.0']);
      expect(sut.headers['X-Api-Version']).toBeUndefined();
    });

    it('replaces the value on re-set (set-semantics)', () => {
      const sut = new GatewayResponseAccumulator();

      sut.header('cache-control', 'no-store').header('cache-control', 'private');

      expect(sut.headers['cache-control']).toEqual(['private']);
    });

    it('returns this for chaining', () => {
      const sut = new GatewayResponseAccumulator();

      const result = sut.header('x-foo', 'bar');

      expect(result).toBe(sut);
    });
  });

  describe('appendHeader', () => {
    it('creates a single-element slice on first call', () => {
      const sut = new GatewayResponseAccumulator();

      sut.appendHeader('vary', 'cookie');

      expect(sut.headers['vary']).toEqual(['cookie']);
    });

    it('appends to an existing slice on subsequent calls', () => {
      const sut = new GatewayResponseAccumulator();

      sut.appendHeader('vary', 'cookie').appendHeader('vary', 'accept-encoding');

      expect(sut.headers['vary']).toEqual(['cookie', 'accept-encoding']);
    });

    it('lowercases the name on write', () => {
      const sut = new GatewayResponseAccumulator();

      sut.appendHeader('Vary', 'cookie').appendHeader('VARY', 'accept');

      expect(sut.headers['vary']).toEqual(['cookie', 'accept']);
    });
  });

  describe('removeHeader', () => {
    it('removes a previously-set header', () => {
      const sut = new GatewayResponseAccumulator();

      sut.header('x-foo', 'bar').removeHeader('x-foo');

      expect(sut.headers['x-foo']).toBeUndefined();
    });

    it('is a no-op for headers that were never set', () => {
      const sut = new GatewayResponseAccumulator();

      const result = sut.removeHeader('x-never-set');

      expect(result).toBe(sut);
      expect(sut.headers['x-never-set']).toBeUndefined();
    });

    it('removes using lowercased name even when called with mixed case', () => {
      const sut = new GatewayResponseAccumulator();

      sut.header('X-Foo', 'bar').removeHeader('X-FOO');

      expect(sut.headers['x-foo']).toBeUndefined();
    });
  });

  describe('cookie', () => {
    it('serializes and appends under the set-cookie key', () => {
      const sut = new GatewayResponseAccumulator();

      sut.cookie('sid', 'abc', { httpOnly: true, maxAge: 3600 });

      expect(sut.headers['set-cookie']).toEqual(['sid=abc; Max-Age=3600; HttpOnly']);
    });

    it('appends multiple cookies as separate entries', () => {
      const sut = new GatewayResponseAccumulator();

      sut.cookie('sid', 'abc', { httpOnly: true }).cookie('tenant', 'demo', { path: '/' });

      expect(sut.headers['set-cookie']).toEqual(['sid=abc; HttpOnly', 'tenant=demo; Path=/']);
    });

    it('merges cookieDefaults into each serialized cookie', () => {
      const sut = new GatewayResponseAccumulator();

      sut.cookieDefaults = { httpOnly: true, secure: true, path: '/' };
      sut.cookie('sid', 'abc');

      expect(sut.headers['set-cookie']).toEqual(['sid=abc; Path=/; HttpOnly; Secure']);
    });

    it('per-cookie options override cookieDefaults', () => {
      const sut = new GatewayResponseAccumulator();

      sut.cookieDefaults = { secure: true, path: '/' };
      sut.cookie('sid', 'abc', { secure: false, sameSite: 'strict' });

      const cookie = (sut.headers['set-cookie'] ?? [])[0] ?? '';

      expect(cookie).toContain('Path=/');
      expect(cookie).not.toContain('Secure');
      expect(cookie).toContain('SameSite=Strict');
    });
  });

  describe('clearCookie', () => {
    it('emits a Set-Cookie with Max-Age=0 and the unix-epoch Expires', () => {
      const sut = new GatewayResponseAccumulator();

      sut.clearCookie('sid');

      const cookies = sut.headers['set-cookie'] ?? [];

      expect(cookies).toHaveLength(1);
      expect(cookies[0]).toContain('sid=');
      expect(cookies[0]).toContain('Max-Age=0');
      expect(cookies[0]).toContain('Expires=Thu, 01 Jan 1970 00:00:00 GMT');
    });

    it('threads path and domain through for scoped deletion', () => {
      const sut = new GatewayResponseAccumulator();

      sut.clearCookie('sid', { path: '/api', domain: '.example.com' });

      const cookies = sut.headers['set-cookie'] ?? [];
      const cookie = cookies[0] ?? '';

      expect(cookie).toContain('Domain=.example.com');
      expect(cookie).toContain('Path=/api');
      expect(cookie).toContain('Max-Age=0');
    });
  });

  describe('redirect', () => {
    it('sets status to 302 by default and writes the location header', () => {
      const sut = new GatewayResponseAccumulator();

      sut.redirect('https://example.com/oauth');

      expect(sut.statusCode).toBe(302);
      expect(sut.headers['location']).toEqual(['https://example.com/oauth']);
    });

    it('honors an explicit status override', () => {
      const sut = new GatewayResponseAccumulator();

      sut.redirect('/new-home', 301);

      expect(sut.statusCode).toBe(301);
      expect(sut.headers['location']).toEqual(['/new-home']);
    });

    it('replaces a previously-set location on repeated calls', () => {
      const sut = new GatewayResponseAccumulator();

      sut.redirect('/first').redirect('/second', 303);

      expect(sut.headers['location']).toEqual(['/second']);
      expect(sut.statusCode).toBe(303);
    });
  });

  describe('reset', () => {
    it('clears status, headers, cookieDefaults, and all cookies for pool reuse', () => {
      const sut = new GatewayResponseAccumulator();

      sut.cookieDefaults = { httpOnly: true, secure: true };
      sut
        .status(201)
        .header('x-foo', 'bar')
        .appendHeader('vary', 'cookie')
        .cookie('sid', 'abc', { httpOnly: true });

      sut.reset();

      expect(sut.statusCode).toBeUndefined();
      expect(Object.keys(sut.headers)).toHaveLength(0);
      expect(sut.cookieDefaults).toEqual({});
    });

    it('preserves the headers object identity so pooled users see the same shape', () => {
      const sut = new GatewayResponseAccumulator();
      const headersBefore = sut.headers;

      sut.header('x-foo', 'bar');
      sut.reset();

      expect(sut.headers).toBe(headersBefore);
    });
  });
});
