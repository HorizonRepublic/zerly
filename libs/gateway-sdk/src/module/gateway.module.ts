import { Global, Module, type DynamicModule, type Provider } from '@nestjs/common';
import { DiscoveryModule } from '@nestjs/core';

import { GatewayExceptionFilter } from '../filters/gateway-exception.filter';
import { GatewayResponseInterceptor } from '../interceptors/gateway-response.interceptor';
import { DefaultErrorBodyFactory } from '../normalization/default-error-body.factory';
import { DefaultGatewayReplyBuilder } from '../normalization/default-reply.builder';
import { DefaultStatusResolver } from '../normalization/default-status.resolver';
import { GatewayMetadataEnricher } from '../runtime/gateway-metadata-enricher';
import {
  GATEWAY_DEFAULTS,
  GATEWAY_ERROR_BODY_FACTORY,
  GATEWAY_REPLY_BUILDER,
  GATEWAY_STATUS_RESOLVER,
} from '../tokens/gateway-tokens.constant';

import type {
  IGatewayModuleAsyncOptions,
  IGatewayModuleOptions,
} from './gateway-module-options.interface';

/**
 * Global NestJS module that wires the gateway SDK building blocks into the
 * application's DI container.
 * @remarks
 * Marked `@Global()` so that `GatewayResponseInterceptor` and
 * `GatewayExceptionFilter` — which are referenced by class in the
 * `@GatewayRoute` decorator via `@UseInterceptors`/`@UseFilters` — are
 * resolvable anywhere in the application without re-importing the module
 * inside every feature module.
 *
 * Override any of the three normalization contracts via `forRoot()` options.
 * All three slots have production-ready defaults; pass `{}` (or nothing)
 * to accept all defaults.
 * @example
 * ```ts
 * @Module({
 *   imports: [GatewayModule.forRoot({})],
 * })
 * export class AppModule {}
 * ```
 */
@Global()
@Module({})
export class GatewayModule {
  /**
   * Build a dynamic module descriptor with the provided options.
   * @param options - Configuration for the gateway SDK. Override any of the
   *                  three normalization contracts by passing a class
   *                  reference; the remaining slots use their defaults.
   *                  Pass `defaults` to apply module-level endpoint defaults
   *                  merged into every `@GatewayRoute` handler at registration
   *                  time.
   */
  public static forRoot(options: IGatewayModuleOptions = {}): DynamicModule {
    const replyBuilderProvider: Provider = {
      provide: GATEWAY_REPLY_BUILDER,
      useClass: options.replyBuilder ?? DefaultGatewayReplyBuilder,
    };

    const statusResolverProvider: Provider = {
      provide: GATEWAY_STATUS_RESOLVER,
      useClass: options.statusResolver ?? DefaultStatusResolver,
    };

    const errorBodyFactoryProvider: Provider = {
      provide: GATEWAY_ERROR_BODY_FACTORY,
      useClass: options.errorBodyFactory ?? DefaultErrorBodyFactory,
    };

    const defaultsProvider: Provider = {
      provide: GATEWAY_DEFAULTS,
      useValue: Object.freeze(options.defaults ?? {}),
    };

    return {
      module: GatewayModule,
      global: true,
      imports: [DiscoveryModule],
      providers: [
        defaultsProvider,
        replyBuilderProvider,
        statusResolverProvider,
        errorBodyFactoryProvider,
        GatewayMetadataEnricher,
        GatewayResponseInterceptor,
        GatewayExceptionFilter,
      ],
      exports: [
        GATEWAY_DEFAULTS,
        GATEWAY_REPLY_BUILDER,
        GATEWAY_STATUS_RESOLVER,
        GATEWAY_ERROR_BODY_FACTORY,
        GatewayResponseInterceptor,
        GatewayExceptionFilter,
      ],
    };
  }

  /**
   * Build a dynamic module descriptor from async configuration.
   * @param asyncOptions - Async options including an optional `imports` array,
   *                       an optional `inject` array, and a `useFactory`
   *                       function that returns `IGatewayModuleOptions` or a
   *                       `Promise` of it.
   * @remarks
   * Use this variant when options depend on providers that must be resolved
   * by NestJS DI at startup — for example when timeout or CORS origins come
   * from a config service backed by environment variables.
   *
   * Note: the three normalization contract slots (`replyBuilder`,
   * `statusResolver`, `errorBodyFactory`) always use their default
   * implementations in the async variant. Only `defaults` is resolved
   * asynchronously.
   * @example
   * ```ts
   * GatewayModule.forRootAsync({
   *   imports: [ConfigModule],
   *   inject: [APP_CONFIG],
   *   useFactory: (config: IAppConfig) => ({
   *     defaults: {
   *       cors: { origins: config.corsOrigins },
   *       timeout: config.requestTimeout,
   *     },
   *   }),
   * })
   * ```
   */
  public static forRootAsync(asyncOptions: IGatewayModuleAsyncOptions): DynamicModule {
    const defaultsProvider: Provider = {
      provide: GATEWAY_DEFAULTS,
      useFactory: async (...args: unknown[]) => {
        const resolved = await asyncOptions.useFactory(...args);

        return Object.freeze(resolved.defaults ?? {});
      },
      inject: asyncOptions.inject ?? [],
    };

    return {
      module: GatewayModule,
      global: true,
      imports: [DiscoveryModule, ...(asyncOptions.imports ?? [])],
      providers: [
        defaultsProvider,
        {
          provide: GATEWAY_REPLY_BUILDER,
          useClass: DefaultGatewayReplyBuilder,
        },
        {
          provide: GATEWAY_STATUS_RESOLVER,
          useClass: DefaultStatusResolver,
        },
        {
          provide: GATEWAY_ERROR_BODY_FACTORY,
          useClass: DefaultErrorBodyFactory,
        },
        GatewayMetadataEnricher,
        GatewayResponseInterceptor,
        GatewayExceptionFilter,
      ],
      exports: [
        GATEWAY_DEFAULTS,
        GATEWAY_REPLY_BUILDER,
        GATEWAY_STATUS_RESOLVER,
        GATEWAY_ERROR_BODY_FACTORY,
        GatewayResponseInterceptor,
        GatewayExceptionFilter,
      ],
    };
  }
}
