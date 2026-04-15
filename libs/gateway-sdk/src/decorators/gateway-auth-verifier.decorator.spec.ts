import 'reflect-metadata';

import { PATTERN_EXTRAS_METADATA, PATTERN_METADATA } from '@nestjs/microservices/constants';

import { describe, expect, it } from '@jest/globals';

import { GatewayAuthVerifier } from './gateway-auth-verifier.decorator';

describe('GatewayAuthVerifier decorator', () => {
  class TestController {
    @GatewayAuthVerifier({ id: 'jwt', default: true })
    public verifyJwt(): { sub: string } {
      return { sub: 'u1' };
    }

    @GatewayAuthVerifier({ id: 'session' })
    public verifySession(): { sub: string } {
      return { sub: 'u2' };
    }

    @GatewayAuthVerifier({ id: 'a'.repeat(63) })
    public verifyMaxLen(): { sub: string } {
      return { sub: 'u3' };
    }
  }

  it('registers pattern and writes verifier metadata with default: true', () => {
    const handler = TestController.prototype.verifyJwt;
    const patterns = Reflect.getMetadata(PATTERN_METADATA, handler) as unknown[];
    const extras = Reflect.getMetadata(PATTERN_EXTRAS_METADATA, handler) as {
      meta: { verifier: Record<string, unknown> };
    };

    expect(patterns).toEqual(['auth.verifier.jwt']);
    expect(extras).toEqual({
      meta: { verifier: { id: 'jwt', default: true } },
    });
  });

  it('omits default key from verifier metadata when not provided', () => {
    const handler = TestController.prototype.verifySession;
    const patterns = Reflect.getMetadata(PATTERN_METADATA, handler) as unknown[];
    const extras = Reflect.getMetadata(PATTERN_EXTRAS_METADATA, handler) as {
      meta: { verifier: Record<string, unknown> };
    };

    expect(patterns).toEqual(['auth.verifier.session']);
    expect(extras).toEqual({
      meta: { verifier: { id: 'session' } },
    });
    expect(extras.meta.verifier).not.toHaveProperty('default');
  });

  it('accepts a 63-character alphanumeric id', () => {
    const handler = TestController.prototype.verifyMaxLen;
    const patterns = Reflect.getMetadata(PATTERN_METADATA, handler) as unknown[];

    expect(patterns).toEqual([`auth.verifier.${'a'.repeat(63)}`]);
  });

  it('throws at decoration time when id is empty', () => {
    const sut = (): void => {
      class Bad {
        @GatewayAuthVerifier({ id: '' })
        public verify(): void {}
      }

      String(Bad);
    };

    expect(sut).toThrow(/verifier id/i);
  });

  it('throws at decoration time when id exceeds 63 characters', () => {
    const sut = (): void => {
      class Bad {
        @GatewayAuthVerifier({ id: 'a'.repeat(64) })
        public verify(): void {}
      }

      String(Bad);
    };

    expect(sut).toThrow(/verifier id/i);
  });

  it('throws at decoration time when id contains invalid characters', () => {
    const invalidIds = ['has space', 'has/slash', 'has.dot'];

    for (const id of invalidIds) {
      const sut = (): void => {
        class Bad {
          @GatewayAuthVerifier({ id })
          public verify(): void {}
        }

        String(Bad);
      };

      expect(sut).toThrow(/verifier id/i);
    }
  });
});
