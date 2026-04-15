import { describe, expect, it } from '@jest/globals';

import { DefaultGatewayReplyBuilder } from './default-reply.builder';

import type { IGatewayErrorBody } from '../types/gateway-error-body.interface';

describe('DefaultGatewayReplyBuilder', () => {
  const builder = new DefaultGatewayReplyBuilder();

  describe('success()', () => {
    it('wraps a body into a reply with empty headers', () => {
      expect(builder.success(200, { id: 1 })).toEqual({
        status: 200,
        headers: {},
        body: { id: 1 },
      });
    });

    it('preserves null body as-is', () => {
      expect(builder.success(204, null)).toEqual({
        status: 204,
        headers: {},
        body: null,
      });
    });

    it('coerces undefined body to null for wire-format determinism', () => {
      // The public signature is `TBody | null`, but the interceptor hands
      // off `unknown` values that can be `undefined` when a handler has a
      // `void` return. The builder must normalize so that `JSON.stringify`
      // emits an explicit `"body": null` field instead of omitting it.
      expect(builder.success(204, undefined as unknown as null)).toEqual({
        status: 204,
        headers: {},
        body: null,
      });
    });

    it('returns the provided status verbatim', () => {
      expect(builder.success(418, 'teapot').status).toBe(418);
    });

    it('forwards the provided multi-value headers map by reference', () => {
      // The builder must not clone or merge — callers own the shape.
      // The Go gateway relies on byte-identity between what the
      // accumulator buffers and what lands on the wire, so a
      // defensive clone here would break the zero-copy contract.
      const headers = {
        'set-cookie': ['sid=a; Path=/', 'theme=dark; Path=/'],
        'x-custom': ['one'],
      } as const;

      const reply = builder.success(200, { id: 1 }, headers);

      expect(reply.headers).toBe(headers);
    });
  });

  describe('error()', () => {
    const errorBody: IGatewayErrorBody = {
      statusCode: 404,
      message: 'User 3 not found',
      error: 'Not Found',
    };

    it('returns an empty headers map', () => {
      // The Go gateway transport layer stamps Content-Type and
      // X-Request-Id on its own, so the reply builder MUST not set
      // headers that would be overwritten anyway. Keeping the map
      // empty also matches NestJS BaseExceptionFilter's behaviour of
      // emitting errors with the same application/json content type
      // as successful responses.
      expect(builder.error(404, errorBody).headers).toEqual({});
    });

    it('returns the error body verbatim', () => {
      expect(builder.error(404, errorBody).body).toBe(errorBody);
    });

    it('returns the provided status', () => {
      expect(builder.error(404, errorBody).status).toBe(404);
    });

    it('forwards the provided multi-value headers map by reference', () => {
      const headers = {
        'www-authenticate': ['Bearer realm="api"'],
      } as const;

      const reply = builder.error(401, errorBody, headers);

      expect(reply.headers).toBe(headers);
    });
  });
});
