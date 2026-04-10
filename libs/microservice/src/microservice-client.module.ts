import { DynamicModule, Module } from '@nestjs/common';

import { JetstreamModule } from '@horizon-republic/nestjs-jetstream';

import { IMicroserviceModuleOptions } from './types/microservice-module.options';

/**
 * Client-side module that exposes a JetStream client proxy for a target
 * service.
 *
 * Wraps `JetstreamModule.forFeature(...)` from `@horizon-republic/nestjs-jetstream`,
 * which reuses the shared NATS connection established by the root module in the
 * application process. Import this module in any feature module that needs to
 * publish events or issue RPC requests to another service.
 *
 * The registered client is retrievable via `getClientToken(options.name)` and
 * is a `JetstreamClient` instance capable of `.emit()` for events and
 * `.send()` for RPC requests.
 * @example
 * ```typescript
 * @Module({
 *   imports: [MicroserviceClientModule.forRoot({ name: 'users', servers: [] })],
 *   providers: [OrdersService],
 * })
 * export class OrdersModule {}
 *
 * @Injectable()
 * export class OrdersService {
 *   public constructor(
 *     @Inject(getClientToken('users'))
 *     private readonly users: JetstreamClient,
 *   ) {}
 * }
 * ```
 */
@Module({})
export class MicroserviceClientModule {
  /**
   * Register a JetStream client proxy for the target service.
   *
   * Only `name` is forwarded to the underlying `forFeature` call — the NATS
   * servers are shared with the root module's connection and do not need to be
   * specified here. Accepting the full options object keeps the public
   * contract symmetric with {@link MicroserviceServerModule}.
   * @param options Client module options. Only `name` is consumed; `servers`
   * is ignored and sourced from the root module.
   * @returns A dynamic module that exposes the `JetstreamClient` for the
   * target service.
   */
  public static forRoot(options: IMicroserviceModuleOptions): DynamicModule {
    return {
      module: MicroserviceClientModule,
      imports: [JetstreamModule.forFeature({ name: options.name })],
    };
  }
}
