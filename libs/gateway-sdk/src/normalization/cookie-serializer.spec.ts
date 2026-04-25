import { afterEach, beforeEach, describe, expect, it, jest } from '@jest/globals';

import { serializeCookie } from './cookie-serializer';

describe('serializeCookie', () => {
  let warnSpy: jest.SpiedFunction<typeof console.warn>;

  beforeEach(() => {
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });
  it('emits name=value with no attributes when options are empty', () => {
    const sut = serializeCookie('sid', 'abc123');

    expect(sut).toBe('sid=abc123');
  });

  it('emits Max-Age for numeric maxAge', () => {
    const sut = serializeCookie('sid', 'abc', { maxAge: 3600 });

    expect(sut).toBe('sid=abc; Max-Age=3600');
  });

  it('emits Expires as an RFC 7231 date string', () => {
    const sut = serializeCookie('sid', 'abc', {
      expires: new Date(Date.UTC(2026, 0, 1, 0, 0, 0)),
    });

    expect(sut).toBe('sid=abc; Expires=Thu, 01 Jan 2026 00:00:00 GMT');
  });

  it('emits HttpOnly only when explicitly true', () => {
    const withFlag = serializeCookie('sid', 'abc', { httpOnly: true });
    const withoutFlag = serializeCookie('sid', 'abc', { httpOnly: false });

    expect(withFlag).toBe('sid=abc; HttpOnly');
    expect(withoutFlag).toBe('sid=abc');
  });

  it('emits Secure only when explicitly true', () => {
    const withFlag = serializeCookie('sid', 'abc', { secure: true });
    const withoutFlag = serializeCookie('sid', 'abc', { secure: false });

    expect(withFlag).toBe('sid=abc; Secure');
    expect(withoutFlag).toBe('sid=abc');
  });

  it('capitalizes SameSite for Strict and Lax (no policy interaction)', () => {
    expect(serializeCookie('sid', 'abc', { sameSite: 'strict' })).toBe('sid=abc; SameSite=Strict');
    expect(serializeCookie('sid', 'abc', { sameSite: 'lax' })).toBe('sid=abc; SameSite=Lax');
  });

  it('auto-promotes Secure when SameSite=None has no secure option', () => {
    // Production-default policy: SameSite=None without Secure is
    // silently rejected by every modern browser. The serializer
    // auto-promotes Secure so a developer who forgot the second
    // flag still ships a cookie the browser accepts.
    const sut = serializeCookie('sid-promote', 'abc', { sameSite: 'none' });

    expect(sut).toBe('sid-promote=abc; Secure; SameSite=None');
  });

  it('emits Partitioned only when explicitly true', () => {
    const withFlag = serializeCookie('sid', 'abc', { partitioned: true });
    const withoutFlag = serializeCookie('sid', 'abc', { partitioned: false });

    expect(withFlag).toBe('sid=abc; Partitioned');
    expect(withoutFlag).toBe('sid=abc');
  });

  it('appends Partitioned after SameSite in the canonical order', () => {
    const sut = serializeCookie('sid', 'abc', {
      secure: true,
      sameSite: 'none',
      partitioned: true,
    });

    expect(sut).toBe('sid=abc; Secure; SameSite=None; Partitioned');
  });

  it('threads Path and Domain through unchanged', () => {
    const sut = serializeCookie('sid', 'abc', {
      path: '/api',
      domain: '.example.com',
    });

    expect(sut).toBe('sid=abc; Domain=.example.com; Path=/api');
  });

  it('composes every attribute in RFC 6265 §4.1.1 order', () => {
    const sut = serializeCookie('sid', 'abc', {
      domain: '.example.com',
      path: '/api',
      expires: new Date(Date.UTC(2026, 0, 1, 0, 0, 0)),
      maxAge: 3600,
      httpOnly: true,
      secure: true,
      sameSite: 'strict',
    });

    expect(sut).toBe(
      'sid=abc; Domain=.example.com; Path=/api; Expires=Thu, 01 Jan 2026 00:00:00 GMT; Max-Age=3600; HttpOnly; Secure; SameSite=Strict',
    );
  });

  it('percent-encodes non-ASCII-token characters in the value', () => {
    const sut = serializeCookie('sid', 'hello world; drop table');

    expect(sut).toBe('sid=hello%20world%3B%20drop%20table');
  });

  it('passes ASCII-alphanumeric values through unchanged (fast path)', () => {
    const sut = serializeCookie('sid', 'abc123XYZ');

    expect(sut).toBe('sid=abc123XYZ');
  });

  it('percent-encodes non-ASCII-token characters in the name', () => {
    const sut = serializeCookie('foo bar', 'v');

    expect(sut).toBe('foo%20bar=v');
  });

  it('rounds fractional Max-Age to an integer', () => {
    const sut = serializeCookie('sid', 'abc', { maxAge: 3600.5 });

    expect(sut).toBe('sid=abc; Max-Age=3600');
  });

  it('allows Max-Age=0 for immediate deletion', () => {
    const sut = serializeCookie('sid', '', { maxAge: 0 });

    expect(sut).toBe('sid=; Max-Age=0');
  });

  describe('SameSite=None secure policy', () => {
    it('auto-promotes Secure and warns once when SameSite=None lacks a secure flag', () => {
      const out = serializeCookie('promote-1', 'abc', { sameSite: 'none' });

      expect(out).toContain('Secure');
      expect(out).toContain('SameSite=None');
      expect(warnSpy).toHaveBeenCalledTimes(1);
      expect(warnSpy.mock.calls[0]?.[0]).toEqual(
        expect.stringContaining('auto-promoted to Secure'),
      );
      expect(warnSpy.mock.calls[0]?.[0]).toEqual(expect.stringContaining('promote-1'));
    });

    it('honours an explicit Secure: false override but warns LOUDLY', () => {
      const out = serializeCookie('explicit-insecure', 'abc', {
        sameSite: 'none',
        secure: false,
      });

      // Explicit override is honoured (local-dev / HTTP test fixtures
      // need it) — the cookie omits Secure on the wire.
      expect(out).not.toContain('Secure');
      expect(out).toContain('SameSite=None');

      expect(warnSpy).toHaveBeenCalledTimes(1);
      const warning = warnSpy.mock.calls[0]?.[0];

      expect(warning).toEqual(expect.stringContaining('modern browsers WILL reject this cookie'));
      expect(warning).toEqual(expect.stringContaining('explicit-insecure'));
    });

    it('does not warn when SameSite=None is paired with Secure: true', () => {
      const out = serializeCookie('ok-none-secure', 'abc', {
        sameSite: 'none',
        secure: true,
      });

      expect(out).toContain('Secure');
      expect(out).toContain('SameSite=None');
      expect(warnSpy).not.toHaveBeenCalled();
    });

    it('does not warn for SameSite=Strict or SameSite=Lax', () => {
      serializeCookie('ok-strict', 'abc', { sameSite: 'strict' });
      serializeCookie('ok-lax', 'abc', { sameSite: 'lax' });

      expect(warnSpy).not.toHaveBeenCalled();
    });

    it('deduplicates the auto-promote warning by cookie name', () => {
      serializeCookie('dedupe-promote', 'abc', { sameSite: 'none' });
      serializeCookie('dedupe-promote', 'def', { sameSite: 'none' });
      serializeCookie('dedupe-promote', 'ghi', { sameSite: 'none' });

      expect(warnSpy).toHaveBeenCalledTimes(1);
    });

    it('emits both warnings if a cookie name escalates from auto-promote to explicit-insecure', () => {
      // First call: secure undefined → auto-promoted, warn fires.
      serializeCookie('escalate', 'abc', { sameSite: 'none' });
      // Second call: secure: false → explicit override, distinct warning
      // shape MUST fire even though the cookie name was already seen.
      serializeCookie('escalate', 'def', { sameSite: 'none', secure: false });

      expect(warnSpy).toHaveBeenCalledTimes(2);
      expect(warnSpy.mock.calls[0]?.[0]).toEqual(
        expect.stringContaining('auto-promoted to Secure'),
      );
      expect(warnSpy.mock.calls[1]?.[0]).toEqual(
        expect.stringContaining('modern browsers WILL reject this cookie'),
      );
    });
  });

  describe('defaults merging', () => {
    it('applies defaults when no per-cookie options given', () => {
      const sut = serializeCookie('sid', 'abc', {}, { secure: true, httpOnly: true, path: '/' });

      expect(sut).toBe('sid=abc; Path=/; HttpOnly; Secure');
    });

    it('per-cookie options override defaults', () => {
      const sut = serializeCookie(
        'sid',
        'abc',
        { secure: false, sameSite: 'strict' },
        { secure: true, path: '/' },
      );

      expect(sut).toContain('Path=/');
      expect(sut).not.toContain('Secure');
      expect(sut).toContain('SameSite=Strict');
    });

    it('empty defaults produce the same result as no defaults', () => {
      const withEmptyDefaults = serializeCookie('sid', 'abc', { httpOnly: true }, {});
      const withoutDefaults = serializeCookie('sid', 'abc', { httpOnly: true });

      expect(withEmptyDefaults).toBe(withoutDefaults);
    });
  });
});
