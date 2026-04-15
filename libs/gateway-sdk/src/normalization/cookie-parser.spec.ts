import { describe, expect, it } from '@jest/globals';

import { parseCookies } from './cookie-parser';

describe('parseCookies', () => {
  it('parses a single name=value pair', () => {
    const sut = parseCookies('sid=abc');

    expect(sut).toEqual({ sid: 'abc' });
  });

  it('parses multiple cookies separated by semicolons', () => {
    const sut = parseCookies('sid=abc; tenant=demo; theme=dark');

    expect(sut).toEqual({ sid: 'abc', tenant: 'demo', theme: 'dark' });
  });

  it('trims whitespace around names and values', () => {
    const sut = parseCookies('sid=abc ;  tenant=demo');

    expect(sut).toEqual({ sid: 'abc', tenant: 'demo' });
  });

  it('returns an empty map for an empty header', () => {
    const sut = parseCookies('');

    expect(sut).toEqual({});
  });

  it('decodes percent-encoded values', () => {
    const sut = parseCookies('raw=hello%20world');

    expect(sut).toEqual({ raw: 'hello world' });
  });

  it('decodes percent-encoded names', () => {
    const sut = parseCookies('my%20name=v');

    expect(sut).toEqual({ 'my name': 'v' });
  });

  it('strips surrounding double quotes from quoted values', () => {
    const sut = parseCookies('sid="quoted-value"');

    expect(sut).toEqual({ sid: 'quoted-value' });
  });

  it('treats a pair without `=` as a flag cookie with empty value', () => {
    const sut = parseCookies('secure; sid=abc');

    expect(sut).toEqual({ secure: '', sid: 'abc' });
  });

  it('keeps the first occurrence on duplicate names', () => {
    const sut = parseCookies('sid=first; sid=second');

    expect(sut).toEqual({ sid: 'first' });
  });

  it('falls back to the raw value on malformed percent-encoding', () => {
    const sut = parseCookies('bad=%GG');

    expect(sut).toEqual({ bad: '%GG' });
  });

  it('tolerates leading and trailing semicolons', () => {
    const sut = parseCookies(';sid=abc;');

    expect(sut).toEqual({ sid: 'abc' });
  });

  it('splits on the first `=` so values may contain `=`', () => {
    const sut = parseCookies('token=a=b=c');

    expect(sut).toEqual({ token: 'a=b=c' });
  });
});
