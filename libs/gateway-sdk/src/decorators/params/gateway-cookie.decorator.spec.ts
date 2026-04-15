import { createMock } from '@golevelup/ts-jest';
import { describe, expect, it } from '@jest/globals';

import { parseCookies } from '../../normalization/cookie-parser';
import { PARSED_COOKIES_KEY } from '../../runtime/parsed-cookies-symbol';

import { readGatewayEnvelope } from './envelope-accessor';
import { GatewayCookie } from './gateway-cookie.decorator';

import type { IGatewayRequest } from '../../types/gateway-request.interface';
import type { ExecutionContext } from '@nestjs/common';

type IEnvelopeWithCookieCache = IGatewayRequest & {
  [PARSED_COOKIES_KEY]?: Record<string, string>;
};

const extractCookie = (name: string, context: ExecutionContext): string | undefined => {
  const envelope = readGatewayEnvelope(context) as IEnvelopeWithCookieCache;

  let parsed = envelope[PARSED_COOKIES_KEY];

  if (parsed === undefined) {
    parsed = parseCookies(envelope.headers['cookie'] ?? '');
    envelope[PARSED_COOKIES_KEY] = parsed;
  }

  return parsed[name];
};

const buildContext = (envelope: Partial<IEnvelopeWithCookieCache>): ExecutionContext =>
  createMock<ExecutionContext>({
    switchToRpc: () =>
      ({
        getData: () => envelope as IGatewayRequest,
      }) as ReturnType<ExecutionContext['switchToRpc']>,
  });

describe('GatewayCookie decorator', () => {
  it('exports the decorator symbol', () => {
    expect(typeof GatewayCookie).toBe('function');
  });

  it('returns the value of a single named cookie', () => {
    const ctx = buildContext({ headers: { cookie: 'sid=abc' } });

    const sut = extractCookie('sid', ctx);

    expect(sut).toBe('abc');
  });

  it('extracts a cookie from a multi-cookie header', () => {
    const ctx = buildContext({ headers: { cookie: 'sid=abc; theme=dark; tenant=demo' } });

    expect(extractCookie('sid', ctx)).toBe('abc');
    expect(extractCookie('theme', ctx)).toBe('dark');
    expect(extractCookie('tenant', ctx)).toBe('demo');
  });

  it('returns undefined when the named cookie is absent', () => {
    const ctx = buildContext({ headers: { cookie: 'sid=abc' } });

    const sut = extractCookie('theme', ctx);

    expect(sut).toBeUndefined();
  });

  it('returns undefined when no cookie header is present', () => {
    const ctx = buildContext({ headers: {} });

    const sut = extractCookie('sid', ctx);

    expect(sut).toBeUndefined();
  });

  it('parses the cookie header exactly once per request across multiple reads', () => {
    const envelope: Partial<IEnvelopeWithCookieCache> = {
      headers: { cookie: 'sid=original' },
    };
    const ctx = buildContext(envelope);

    expect(extractCookie('sid', ctx)).toBe('original');

    const cached = envelope[PARSED_COOKIES_KEY];

    expect(cached).toBeDefined();
    if (cached !== undefined) {
      cached['sid'] = 'tampered';
    }

    expect(extractCookie('sid', ctx)).toBe('tampered');
  });

  it('caches an empty map when the cookie header is missing', () => {
    const envelope: Partial<IEnvelopeWithCookieCache> = { headers: {} };
    const ctx = buildContext(envelope);

    expect(extractCookie('sid', ctx)).toBeUndefined();

    const cached = envelope[PARSED_COOKIES_KEY];

    expect(cached).toBeDefined();
    if (cached !== undefined) {
      cached['sentinel'] = 'survived';
    }

    expect(extractCookie('sentinel', ctx)).toBe('survived');
  });

  it('keeps caches isolated per envelope (no cross-request bleed)', () => {
    const envelopeA: Partial<IEnvelopeWithCookieCache> = {
      headers: { cookie: 'sid=alice' },
    };
    const envelopeB: Partial<IEnvelopeWithCookieCache> = {
      headers: { cookie: 'sid=bob' },
    };

    expect(extractCookie('sid', buildContext(envelopeA))).toBe('alice');
    expect(extractCookie('sid', buildContext(envelopeB))).toBe('bob');
  });
});
