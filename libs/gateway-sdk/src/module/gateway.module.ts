import { Global, Module, type DynamicModule, type Provider } from '@nestjs/common';

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

import type { IGatewayModuleOptions } from './gateway-module-options.interface';

/**
 * Global NestJS module that wires the gateway SDK building blocks into the
 * application's DI container.
 * @remarks
 * Marked `@Global()` so that `GatewayResponseInterceptor` and
 * `GatewayExceptionFilter` — which are referenced by class in the
 * `@ApiGateway` decorator via `@UseInterceptors`/`@UseFilters` — are
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

    return {
      module: GatewayModule,
      global: true,
      providers: [
        replyBuilderProvider,
        statusResolverProvider,
        errorBodyFactoryProvider,
        GatewayResponseInterceptor,
        GatewayExceptionFilter,
      ],
      exports: [
        GATEWAY_REPLY_BUILDER,
        GATEWAY_STATUS_RESOLVER,
        GATEWAY_ERROR_BODY_FACTORY,
        GatewayResponseInterceptor,
        GatewayExceptionFilter,
      ],
    };
  }
}
