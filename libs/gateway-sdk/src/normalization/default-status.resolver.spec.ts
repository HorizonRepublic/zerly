import { describe, expect, it } from '@jest/globals';

import { DefaultStatusResolver } from './default-status.resolver';

import type { IGatewayHttpMeta } from '../types/gateway-http-meta.interface';

describe('DefaultStatusResolver', () => {
  const resolver = new DefaultStatusResolver();
  const httpMeta: IGatewayHttpMeta = { method: 'POST', path: '/users' };

  it('returns explicit statusCode when provided', () => {
    expect(resolver.resolveSuccess({ ...httpMeta, statusCode: 201 }, { id: 1 })).toBe(201);
  });

  it('returns 204 for undefined return value', () => {
    expect(resolver.resolveSuccess(httpMeta, undefined)).toBe(204);
  });

  it('returns 204 for null return value', () => {
    expect(resolver.resolveSuccess(httpMeta, null)).toBe(204);
  });

  it('returns 200 for a non-null object return', () => {
    expect(resolver.resolveSuccess(httpMeta, { id: 1 })).toBe(200);
  });

  it('returns 200 for zero (falsy but not null/undefined)', () => {
    expect(resolver.resolveSuccess(httpMeta, 0)).toBe(200);
  });

  it('returns 200 for empty string (falsy but not null/undefined)', () => {
    expect(resolver.resolveSuccess(httpMeta, '')).toBe(200);
  });

  it('returns 200 for false boolean (falsy but not null/undefined)', () => {
    expect(resolver.resolveSuccess(httpMeta, false)).toBe(200);
  });

  it('allows explicit statusCode to override the null default', () => {
    expect(resolver.resolveSuccess({ ...httpMeta, statusCode: 200 }, null)).toBe(200);
  });

  it('allows explicit statusCode of 0 to be returned literally', () => {
    expect(resolver.resolveSuccess({ ...httpMeta, statusCode: 0 }, { id: 1 })).toBe(0);
  });
});
