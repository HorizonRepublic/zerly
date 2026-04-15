import { describe, expect, it } from '@jest/globals';

import { serializeCookie } from './cookie-serializer';

describe('serializeCookie', () => {
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

  it('capitalizes SameSite for each variant', () => {
    expect(serializeCookie('sid', 'abc', { sameSite: 'strict' })).toBe('sid=abc; SameSite=Strict');
    expect(serializeCookie('sid', 'abc', { sameSite: 'lax' })).toBe('sid=abc; SameSite=Lax');
    expect(serializeCookie('sid', 'abc', { sameSite: 'none' })).toBe('sid=abc; SameSite=None');
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
});
