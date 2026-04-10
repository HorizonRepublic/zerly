import type { IErrorBodyFactory } from '../normalization/contracts/error-body-factory.interface';
import type { IGatewayReplyBuilder } from '../normalization/contracts/reply-builder.interface';
import type { IStatusResolver } from '../normalization/contracts/status-resolver.interface';
import type { Type } from '@nestjs/common';

/**
 * Options accepted by `GatewayModule.forRoot()`.
 * @remarks
 * All implementation slots have sensible defaults — override only the ones
 * you need. The single mandatory option is `isProduction`, which controls
 * whether stack traces leak into HTTP error response bodies.
 *
 * To swap any building block, provide a `Type<I...>` (a class constructor
 * reference — not an instance) against the corresponding slot. NestJS DI
 * will instantiate the class with its own dependency graph.
 * @example
 * ```ts
 * @Module({
 *   imports: [
 *     GatewayModule.forRoot({
 *       isProduction: process.env.NODE_ENV === 'production',
 *       statusResolver: MyDomainStatusResolver,
 *     }),
 *   ],
 * })
 * export class AppModule {}
 * ```
 */
export interface IGatewayModuleOptions {
  /**
   * Whether the application runs in production mode.
   * @remarks
   * When `true`, the default `DefaultErrorBodyFactory` never exposes stack
   * traces in HTTP error response bodies. Typically sourced from
   * `process.env.NODE_ENV === 'production'` at the call site.
   *
   * Custom `errorBodyFactory` implementations receive this value via their
   * own DI-injected configuration — they are NOT passed `isProduction`
   * automatically, because the module does not know their constructor shape.
   */
  readonly isProduction: boolean;

  /**
   * Override class for the reply envelope builder. Must implement
   * `IGatewayReplyBuilder`. When omitted, `DefaultGatewayReplyBuilder` is used.
   */
  readonly replyBuilder?: Type<IGatewayReplyBuilder>;

  /**
   * Override class for the success-path status resolver. Must implement
   * `IStatusResolver`. When omitted, `DefaultStatusResolver` is used.
   */
  readonly statusResolver?: Type<IStatusResolver>;

  /**
   * Override class for the error body factory. Must implement
   * `IErrorBodyFactory`. When omitted, `DefaultErrorBodyFactory` is used
   * with the `isProduction` flag above.
   * @remarks
   * Unlike the default factory, custom implementations bind to the DI
   * token via `useClass` — they are instantiated by NestJS's DI container
   * with their own constructor dependency graph. If your custom factory
   * needs to know `isProduction`, inject it yourself via `@Inject(CONFIG)`
   * or a similar mechanism on your class.
   */
  readonly errorBodyFactory?: Type<IErrorBodyFactory>;
}
