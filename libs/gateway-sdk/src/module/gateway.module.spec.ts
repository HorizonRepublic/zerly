// Mock @nestjs/common to avoid ESM-only import failures under ts-jest.
// The PascalCase keys are required to match Nest's public API surface; they
// are supplied via computed properties so the `naming-convention` rule
// targeting shorthand method keys is not triggered.
jest.mock('@nestjs/common', () => ({
  ['Global']: (): ClassDecorator => (target) => target,
  ['Module']: (): ClassDecorator => (target) => target,
  ['Injectable']: (): ClassDecorator => (target) => target,
  ['Inject']: (): ParameterDecorator => () => undefined,
  ['Catch']: (): ClassDecorator => (target) => target,
}));

jest.mock('@nestjs/core', () => ({
  ['Reflector']: class {
    public get = jest.fn();
  },
}));

jest.mock('@nestjs/microservices/constants', () => ({
  ['PATTERN_EXTRAS_METADATA']: 'microservices:pattern_extras',
}));

import { GatewayExceptionFilter } from '../filters/gateway-exception.filter';
import { GatewayResponseInterceptor } from '../interceptors/gateway-response.interceptor';
import { DefaultErrorBodyFactory } from '../normalization/default-error-body.factory';
import { DefaultGatewayReplyBuilder } from '../normalization/default-reply.builder';
import { DefaultStatusResolver } from '../normalization/default-status.resolver';
import {
  GATEWAY_ERROR_BODY_FACTORY,
  GATEWAY_REPLY_BUILDER,
  GATEWAY_STATUS_RESOLVER,
} from '../tokens/gateway-tokens.constant';

import { GatewayModule } from './gateway.module';

import type { IErrorBodyFactory } from '../normalization/contracts/error-body-factory.interface';
import type { IGatewayReplyBuilder } from '../normalization/contracts/reply-builder.interface';
import type { ClassProvider, FactoryProvider, Provider } from '@nestjs/common';

const findUseClassProvider = (
  providers: readonly Provider[],
  token: symbol,
): ClassProvider | undefined =>
  providers.find(
    (p): p is ClassProvider =>
      typeof p === 'object' && 'provide' in p && p.provide === token && 'useClass' in p,
  );

const findUseFactoryProvider = (
  providers: readonly Provider[],
  token: symbol,
): FactoryProvider | undefined =>
  providers.find(
    (p): p is FactoryProvider =>
      typeof p === 'object' && 'provide' in p && p.provide === token && 'useFactory' in p,
  );

describe('GatewayModule', () => {
  describe('forRoot()', () => {
    it('returns a DynamicModule with global: true and module: GatewayModule', () => {
      const mod = GatewayModule.forRoot({ isProduction: false });

      expect(mod.module).toBe(GatewayModule);
      expect(mod.global).toBe(true);
    });

    it('registers default impls when no overrides are provided', () => {
      const mod = GatewayModule.forRoot({ isProduction: false });
      const providers = mod.providers ?? [];

      const replyBuilderProvider = findUseClassProvider(providers, GATEWAY_REPLY_BUILDER);

      expect(replyBuilderProvider).toBeDefined();
      expect(replyBuilderProvider?.useClass).toBe(DefaultGatewayReplyBuilder);

      const statusResolverProvider = findUseClassProvider(providers, GATEWAY_STATUS_RESOLVER);

      expect(statusResolverProvider).toBeDefined();
      expect(statusResolverProvider?.useClass).toBe(DefaultStatusResolver);
    });

    it('binds errorBodyFactory via useFactory producing DefaultErrorBodyFactory with isProduction=true', () => {
      const mod = GatewayModule.forRoot({ isProduction: true });
      const providers = mod.providers ?? [];

      const factoryProvider = findUseFactoryProvider(providers, GATEWAY_ERROR_BODY_FACTORY);

      expect(factoryProvider).toBeDefined();

      const instance = factoryProvider?.useFactory();

      expect(instance).toBeInstanceOf(DefaultErrorBodyFactory);
    });

    it('binds errorBodyFactory via useFactory producing DefaultErrorBodyFactory with isProduction=false', () => {
      const mod = GatewayModule.forRoot({ isProduction: false });
      const providers = mod.providers ?? [];

      const factoryProvider = findUseFactoryProvider(providers, GATEWAY_ERROR_BODY_FACTORY);

      expect(factoryProvider).toBeDefined();

      const instance = factoryProvider?.useFactory();

      expect(instance).toBeInstanceOf(DefaultErrorBodyFactory);
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

      const mod = GatewayModule.forRoot({
        isProduction: false,
        replyBuilder: CustomReplyBuilder,
      });
      const provider = findUseClassProvider(mod.providers ?? [], GATEWAY_REPLY_BUILDER);

      expect(provider?.useClass).toBe(CustomReplyBuilder);
      expect(provider?.useClass).not.toBe(DefaultGatewayReplyBuilder);
    });

    it('uses useClass (not useFactory) when custom errorBodyFactory is provided', () => {
      class CustomFactory implements IErrorBodyFactory {
        public build(): never {
          throw new Error('not implemented');
        }
      }

      const mod = GatewayModule.forRoot({
        isProduction: true,
        errorBodyFactory: CustomFactory,
      });
      const providers = mod.providers ?? [];

      const useClassEntry = findUseClassProvider(providers, GATEWAY_ERROR_BODY_FACTORY);

      expect(useClassEntry?.useClass).toBe(CustomFactory);

      const useFactoryEntry = findUseFactoryProvider(providers, GATEWAY_ERROR_BODY_FACTORY);

      expect(useFactoryEntry).toBeUndefined();
    });

    it('includes GatewayResponseInterceptor and GatewayExceptionFilter in providers', () => {
      const mod = GatewayModule.forRoot({ isProduction: false });

      expect(mod.providers).toContain(GatewayResponseInterceptor);
      expect(mod.providers).toContain(GatewayExceptionFilter);
    });

    it('exports all three tokens plus the interceptor and filter classes', () => {
      const mod = GatewayModule.forRoot({ isProduction: false });

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
  });
});
