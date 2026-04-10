import { DynamicModule, Module } from '@nestjs/common';

import { JetstreamModule } from '@horizon-republic/nestjs-jetstream';

import { MICROSERVICE_OPTIONS } from './const';
import { MicroserviceServerProvider } from './providers/microservice-server.provider';
import { IMicroserviceModuleOptions } from './types/microservice-module.options';

/**
 * Server-side module that attaches a JetStream transport strategy to a hybrid
 * NestJS HTTP application.
 *
 * Wraps `JetstreamModule.forRoot(...)` from `@horizon-republic/nestjs-jetstream`
 * and installs a lifecycle-aware provider ({@link MicroserviceServerProvider})
 * that connects the strategy to the host app once the kernel transitions to the
 * listening state. This allows an application to serve both HTTP and NATS
 * JetStream messages from the same process without manually orchestrating the
 * microservice bootstrap sequence in `main.ts`.
 *
 * Consumers typically import this module from their `AppModule`:
 * @example
 * ```typescript
 * @Module({
 *   imports: [
 *     MicroserviceServerModule.forRoot({
 *       name: 'orders',
 *       servers: ['nats://localhost:4222'],
 *     }),
 *   ],
 * })
 * export class AppModule {}
 * ```
 */
@Module({})
export class MicroserviceServerModule {
  /**
   * Register the JetStream transport synchronously.
   *
   * Use this overload when the NATS servers and service name are known at
   * bootstrap time (e.g. sourced from a resolved config object).
   * @param options Static module options resolved before module initialization.
   * @returns A dynamic module that provides the JetStream strategy and attaches
   * it to the host application on listen.
   */
  public static forRoot(options: IMicroserviceModuleOptions): DynamicModule {
    return {
      module: MicroserviceServerModule,
      imports: [
        JetstreamModule.forRoot({
          name: options.name,
          servers: [...options.servers],
        }),
      ],
      providers: [
        {
          provide: MICROSERVICE_OPTIONS,
          useValue: options,
        },
        MicroserviceServerProvider,
      ],
    };
  }

  /**
   * Register the JetStream transport with asynchronously resolved options.
   *
   * Delegates to `JetstreamModule.forRootAsync` with a `useFactory` that yields
   * the `{ name, servers }` shape the underlying transport expects. The caller
   * is responsible for providing a fully populated `IMicroserviceModuleOptions`
   * object — the async wiring here exists so the dynamic module contract
   * matches the synchronous `forRoot` overload and leaves room for downstream
   * consumers to defer module instantiation behind their own async
   * initialization pipeline.
   * @param options Module options resolved before module initialization. The
   * underlying `JetstreamModule.forRootAsync` still uses a factory internally
   * so its DI graph is constructed lazily.
   * @returns A dynamic module that provides the JetStream strategy and attaches
   * it to the host application on listen.
   */
  public static forRootAsync(options: IMicroserviceModuleOptions): DynamicModule {
    return {
      module: MicroserviceServerModule,
      imports: [
        JetstreamModule.forRootAsync({
          name: options.name,
          imports: [],
          inject: [],
          useFactory: () => ({
            servers: [...options.servers],
          }),
        }),
      ],
      providers: [
        {
          provide: MICROSERVICE_OPTIONS,
          useValue: options,
        },
        MicroserviceServerProvider,
      ],
    };
  }
}
