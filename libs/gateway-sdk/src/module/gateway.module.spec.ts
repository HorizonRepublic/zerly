import { Logger } from '@nestjs/common';
import { Test } from '@nestjs/testing';

import { afterEach, describe, expect, it, jest } from '@jest/globals';

import { GatewayExceptionFilter } from '../filters/gateway-exception.filter';
import { GatewayResponseInterceptor } from '../interceptors/gateway-response.interceptor';
import { DefaultErrorBodyFactory } from '../normalization/default-error-body.factory';
import { DefaultGatewayReplyBuilder } from '../normalization/default-reply.builder';
import { DefaultStatusResolver } from '../normalization/default-status.resolver';
import {
  GATEWAY_DEFAULTS,
  GATEWAY_ERROR_BODY_FACTORY,
  GATEWAY_REPLY_BUILDER,
  GATEWAY_STATUS_RESOLVER,
} from '../tokens/gateway-tokens.constant';

import { GatewayModule } from './gateway.module';

import type { IErrorBodyFactory } from '../normalization/contracts/error-body-factory.interface';
import type { IGatewayReplyBuilder } from '../normalization/contracts/reply-builder.interface';
import type { IGatewayDefaults } from '../types/gateway-defaults.interface';
import type { ClassProvider, DynamicModule, Provider, ValueProvider } from '@nestjs/common';

const findUseClassProvider = (
  providers: readonly Provider[],
  token: symbol,
): ClassProvider | undefined =>
  providers.find(
    (p): p is ClassProvider =>
      typeof p === 'object' && 'provide' in p && p.provide === token && 'useClass' in p,
  );

describe('GatewayModule', () => {
  describe('forRoot()', () => {
    it('returns a DynamicModule with global: true and module: GatewayModule', () => {
      const mod = GatewayModule.forRoot();

      expect(mod.module).toBe(GatewayModule);
      expect(mod.global).toBe(true);
    });

    it('accepts being called with no arguments at all', () => {
      // All normalization slots have production-ready defaults; the module
      // should be usable with a bare `forRoot()` invocation for zero-config
      // quickstarts.
      const mod = GatewayModule.forRoot();
      const providers = mod.providers ?? [];

      expect(findUseClassProvider(providers, GATEWAY_REPLY_BUILDER)?.useClass).toBe(
        DefaultGatewayReplyBuilder,
      );
      expect(findUseClassProvider(providers, GATEWAY_STATUS_RESOLVER)?.useClass).toBe(
        DefaultStatusResolver,
      );
      expect(findUseClassProvider(providers, GATEWAY_ERROR_BODY_FACTORY)?.useClass).toBe(
        DefaultErrorBodyFactory,
      );
    });

    it('registers default impls when an empty options object is passed', () => {
      const mod = GatewayModule.forRoot({});
      const providers = mod.providers ?? [];

      expect(findUseClassProvider(providers, GATEWAY_REPLY_BUILDER)?.useClass).toBe(
        DefaultGatewayReplyBuilder,
      );
      expect(findUseClassProvider(providers, GATEWAY_STATUS_RESOLVER)?.useClass).toBe(
        DefaultStatusResolver,
      );
      expect(findUseClassProvider(providers, GATEWAY_ERROR_BODY_FACTORY)?.useClass).toBe(
        DefaultErrorBodyFactory,
      );
    });

    it('uses the custom replyBuilder override when provided', () => {
      class CustomReplyBuilder implements IGatewayReplyBuilder {
        public success(): never {
          throw new Error('not implemented');
        }

        public error(): never {
          throw new Error('not implemented');
        }
      }

      const mod = GatewayModule.forRoot({ replyBuilder: CustomReplyBuilder });
      const provider = findUseClassProvider(mod.providers ?? [], GATEWAY_REPLY_BUILDER);

      expect(provider?.useClass).toBe(CustomReplyBuilder);
      expect(provider?.useClass).not.toBe(DefaultGatewayReplyBuilder);
    });

    it('uses the custom errorBodyFactory override when provided', () => {
      class CustomFactory implements IErrorBodyFactory {
        public build(): never {
          throw new Error('not implemented');
        }
      }

      const mod = GatewayModule.forRoot({ errorBodyFactory: CustomFactory });
      const provider = findUseClassProvider(mod.providers ?? [], GATEWAY_ERROR_BODY_FACTORY);

      expect(provider?.useClass).toBe(CustomFactory);
      expect(provider?.useClass).not.toBe(DefaultErrorBodyFactory);
    });

    it('includes GatewayResponseInterceptor and GatewayExceptionFilter in providers', () => {
      const mod = GatewayModule.forRoot();

      expect(mod.providers).toContain(GatewayResponseInterceptor);
      expect(mod.providers).toContain(GatewayExceptionFilter);
    });

    it('exports all three tokens plus the interceptor and filter classes', () => {
      const mod = GatewayModule.forRoot();

      expect(mod.exports).toEqual(
        expect.arrayContaining([
          GATEWAY_REPLY_BUILDER,
          GATEWAY_STATUS_RESOLVER,
          GATEWAY_ERROR_BODY_FACTORY,
          GatewayResponseInterceptor,
          GatewayExceptionFilter,
        ]),
      );
    });

    it('provides GATEWAY_DEFAULTS with a frozen defaults object', () => {
      const mod = GatewayModule.forRoot({
        defaults: {
          cors: { origins: ['https://example.com'] },
          timeout: 5000,
        },
      });

      const provider = (mod.providers as Provider[]).find(
        (p): p is ValueProvider =>
          typeof p === 'object' && 'provide' in p && p.provide === GATEWAY_DEFAULTS,
      ) as ValueProvider;

      expect(provider).toBeDefined();
      expect(provider.useValue).toEqual({
        cors: { origins: ['https://example.com'] },
        timeout: 5000,
      });
      expect(Object.isFrozen(provider.useValue)).toBe(true);
    });

    it('provides empty frozen defaults when no defaults option given', () => {
      const mod = GatewayModule.forRoot();

      const provider = (mod.providers as Provider[]).find(
        (p): p is ValueProvider =>
          typeof p === 'object' && 'provide' in p && p.provide === GATEWAY_DEFAULTS,
      ) as ValueProvider;

      expect(provider.useValue).toEqual({});
      expect(Object.isFrozen(provider.useValue)).toBe(true);
    });

    it('exports GATEWAY_DEFAULTS', () => {
      const mod = GatewayModule.forRoot();

      expect(mod.exports).toContain(GATEWAY_DEFAULTS);
    });
  });

  describe('forRootAsync()', () => {
    it('returns a DynamicModule with global: true', () => {
      const mod = GatewayModule.forRootAsync({
        useFactory: () => ({ defaults: { timeout: 3000 } }),
      });

      expect(mod.module).toBe(GatewayModule);
      expect(mod.global).toBe(true);
    });

    it('exports GATEWAY_DEFAULTS', () => {
      const mod = GatewayModule.forRootAsync({
        useFactory: () => ({}),
      });

      expect(mod.exports).toContain(GATEWAY_DEFAULTS);
    });
  });

  describe('forRootAsync integration', () => {
    it('should resolve GATEWAY_DEFAULTS from async factory', async () => {
      const moduleRef = await Test.createTestingModule({
        imports: [
          GatewayModule.forRootAsync({
            useFactory: () => ({
              defaults: {
                cors: { origins: ['https://async.example.com'] },
                timeout: 7000,
              },
            }),
          }),
        ],
      }).compile();

      const defaults = moduleRef.get<IGatewayDefaults>(GATEWAY_DEFAULTS);

      expect(defaults).toEqual({
        cors: { origins: ['https://async.example.com'] },
        timeout: 7000,
      });
      expect(Object.isFrozen(defaults)).toBe(true);

      await moduleRef.close();
    });

    it('should resolve GATEWAY_DEFAULTS from async factory with inject', async () => {
      const CONFIG_TOKEN = Symbol('test-config');
      const configValue = {
        corsOrigins: ['https://injected.example.com'],
        requestTimeout: 15000,
      };

      const configModule: DynamicModule = {
        module: class ConfigStubModule {},
        providers: [{ provide: CONFIG_TOKEN, useValue: configValue }],
        exports: [CONFIG_TOKEN],
        global: true,
      };

      const moduleRef = await Test.createTestingModule({
        imports: [
          configModule,
          GatewayModule.forRootAsync({
            inject: [CONFIG_TOKEN],
            useFactory: (config: typeof configValue) => ({
              defaults: {
                cors: { origins: config.corsOrigins },
                timeout: config.requestTimeout,
              },
            }),
          }),
        ],
      }).compile();

      const defaults = moduleRef.get<IGatewayDefaults>(GATEWAY_DEFAULTS);

      expect(defaults).toEqual({
        cors: { origins: ['https://injected.example.com'] },
        timeout: 15000,
      });

      await moduleRef.close();
    });

    it('should provide empty frozen defaults when async factory returns no defaults', async () => {
      const moduleRef = await Test.createTestingModule({
        imports: [
          GatewayModule.forRootAsync({
            useFactory: () => ({}),
          }),
        ],
      }).compile();

      const defaults = moduleRef.get<IGatewayDefaults>(GATEWAY_DEFAULTS);

      expect(defaults).toEqual({});
      expect(Object.isFrozen(defaults)).toBe(true);

      await moduleRef.close();
    });
  });

  describe('CORS wildcard + credentials guard', () => {
    it('forRoot throws when defaults.cors combines wildcard origin with credentials', () => {
      expect(() =>
        GatewayModule.forRoot({
          defaults: {
            cors: { origins: ['*'], credentials: true },
          },
        }),
      ).toThrow(/cannot be combined with cors.origins: '\*'/);
    });

    it('forRoot accepts explicit origin with credentials', () => {
      expect(() =>
        GatewayModule.forRoot({
          defaults: {
            cors: { origins: ['https://app.example.com'], credentials: true },
          },
        }),
      ).not.toThrow();
    });

    it('forRoot accepts wildcard without credentials', () => {
      expect(() =>
        GatewayModule.forRoot({
          defaults: {
            cors: { origins: ['*'] },
          },
        }),
      ).not.toThrow();
    });

    it('forRootAsync throws when factory returns wildcard + credentials', async () => {
      const moduleRef = Test.createTestingModule({
        imports: [
          GatewayModule.forRootAsync({
            useFactory: () => ({
              defaults: {
                cors: { origins: ['*'], credentials: true },
              },
            }),
          }),
        ],
      }).compile();

      await expect(moduleRef).rejects.toThrow(/cannot be combined with cors.origins: '\*'/);
    });
  });

  describe('rateLimit defaults guard', () => {
    afterEach(() => {
      jest.restoreAllMocks();
    });

    it('forRoot throws on rps: 0 in defaults.rateLimit', () => {
      expect(() =>
        GatewayModule.forRoot({
          defaults: {
            rateLimit: { rps: 0 },
          },
        }),
      ).toThrow(/rateLimit\.rps must be a positive integer/);
    });

    it('forRoot throws on negative burst in defaults.rateLimit', () => {
      expect(() =>
        GatewayModule.forRoot({
          defaults: {
            rateLimit: { rps: 10, burst: -1 },
          },
        }),
      ).toThrow(/rateLimit\.burst must be a non-negative integer/);
    });

    it('forRoot warns when defaults.rateLimit.burst is below rps', () => {
      const warnSpy = jest.spyOn(Logger.prototype, 'warn').mockImplementation(() => {});

      GatewayModule.forRoot({
        defaults: {
          rateLimit: { rps: 100, burst: 1 },
        },
      });

      expect(warnSpy).toHaveBeenCalledWith(expect.stringMatching(/burst.*less than/));
    });

    it('forRoot warns when defaults.rateLimit.keyBy includes a user: prefix', () => {
      const warnSpy = jest.spyOn(Logger.prototype, 'warn').mockImplementation(() => {});

      GatewayModule.forRoot({
        defaults: {
          rateLimit: { rps: 10, keyBy: ['user:id', 'ip'] },
        },
      });

      expect(warnSpy).toHaveBeenCalledWith(expect.stringMatching(/keyBy.*user:/));
    });

    it('forRoot does not warn for a well-formed rateLimit default', () => {
      const warnSpy = jest.spyOn(Logger.prototype, 'warn').mockImplementation(() => {});

      GatewayModule.forRoot({
        defaults: {
          rateLimit: { rps: 10, burst: 20, keyBy: ['ip'] },
        },
      });

      expect(warnSpy).not.toHaveBeenCalled();
    });
  });
});
