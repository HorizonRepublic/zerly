import { describe, expect, it } from '@jest/globals';

import { assertRateLimitConfig } from './rate-limit-validator';

describe('assertRateLimitConfig', () => {
  describe('happy path', () => {
    it('accepts an undefined block (rate limiting disabled by omission)', () => {
      expect(() => {
        assertRateLimitConfig(undefined, 'test');
      }).not.toThrow();
    });

    it('accepts an integer rps in the supported range', () => {
      expect(() => {
        assertRateLimitConfig({ rps: 1 }, 'test');
      }).not.toThrow();
      expect(() => {
        assertRateLimitConfig({ rps: 100 }, 'test');
      }).not.toThrow();
    });

    it('accepts an explicit non-negative burst', () => {
      expect(() => {
        assertRateLimitConfig({ rps: 10, burst: 0 }, 'test');
      }).not.toThrow();
      expect(() => {
        assertRateLimitConfig({ rps: 10, burst: 100 }, 'test');
      }).not.toThrow();
    });
  });

  describe('rps validation', () => {
    it('rejects rps: 0 with a guidance message about omitting the block', () => {
      expect(() => {
        assertRateLimitConfig({ rps: 0 }, '@GatewayRoute(POST /users)');
      }).toThrow(/rateLimit\.rps must be a positive integer/);

      expect(() => {
        assertRateLimitConfig({ rps: 0 }, '@GatewayRoute(POST /users)');
      }).toThrow(/omit the rateLimit block entirely/);
    });

    it('rejects negative rps', () => {
      expect(() => {
        assertRateLimitConfig({ rps: -1 }, 'test');
      }).toThrow(/rateLimit\.rps must be a positive integer/);
    });

    it('rejects fractional rps', () => {
      expect(() => {
        assertRateLimitConfig({ rps: 1.5 }, 'test');
      }).toThrow(/rateLimit\.rps must be a positive integer/);
    });

    it('rejects rps above 2^32 - 1', () => {
      expect(() => {
        assertRateLimitConfig({ rps: 0x1_00_00_00_00 }, 'test');
      }).toThrow(/rateLimit\.rps must be a positive integer/);
    });

    it('includes the source context in the error message', () => {
      expect(() => {
        assertRateLimitConfig({ rps: 0 }, '@GatewayRoute(POST /users)');
      }).toThrow(/Source: @GatewayRoute\(POST \/users\)/);
    });
  });

  describe('burst validation', () => {
    it('rejects negative burst', () => {
      expect(() => {
        assertRateLimitConfig({ rps: 10, burst: -5 }, 'test');
      }).toThrow(/rateLimit\.burst must be a non-negative integer/);
    });

    it('rejects fractional burst', () => {
      expect(() => {
        assertRateLimitConfig({ rps: 10, burst: 2.5 }, 'test');
      }).toThrow(/rateLimit\.burst must be a non-negative integer/);
    });

    it('rejects burst above 2^32 - 1', () => {
      expect(() => {
        assertRateLimitConfig({ rps: 10, burst: 0x1_00_00_00_00 }, 'test');
      }).toThrow(/rateLimit\.burst must be a non-negative integer/);
    });
  });
});
