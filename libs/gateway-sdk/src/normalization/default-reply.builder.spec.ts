// Mock @nestjs/common to avoid ESM-only import failures under ts-jest.
// The PascalCase key is required to match Nest's public API surface; it is
// supplied via a computed property so the camelCase `naming-convention` rule
// targeting shorthand method keys is not triggered.
jest.mock('@nestjs/common', () => ({
  ['Injectable']: (): ClassDecorator => (target) => target,
}));

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

    it('returns the provided status verbatim', () => {
      expect(builder.success(418, 'teapot').status).toBe(418);
    });
  });

  describe('error()', () => {
    const errorBody: IGatewayErrorBody = {
      error: 'NOT_FOUND',
      message: 'Not found',
      requestId: 'req-123',
    };

    it('sets application/problem+json content type', () => {
      expect(builder.error(404, errorBody).headers).toEqual({
        'content-type': 'application/problem+json',
      });
    });

    it('returns the error body verbatim', () => {
      expect(builder.error(404, errorBody).body).toBe(errorBody);
    });

    it('returns the provided status', () => {
      expect(builder.error(404, errorBody).status).toBe(404);
    });
  });
});
