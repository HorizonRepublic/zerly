import type { IErrorBodyFactory } from '../normalization/contracts/error-body-factory.interface';
import type { IGatewayReplyBuilder } from '../normalization/contracts/reply-builder.interface';
import type { IStatusResolver } from '../normalization/contracts/status-resolver.interface';
import type { Type } from '@nestjs/common';

/**
 * Options accepted by `GatewayModule.forRoot()`.
 * @remarks
 * All implementation slots have production-ready defaults — override only
 * the ones you need. Passing an empty object (or nothing) wires every slot
 * to its default implementation.
 *
 * To swap any building block, provide a `Type<I...>` (a class constructor
 * reference — not an instance) against the corresponding slot. NestJS DI
 * will instantiate the class with its own dependency graph.
 * @example
 * ```ts
 * @Module({
 *   imports: [
 *     GatewayModule.forRoot({
 *       statusResolver: MyDomainStatusResolver,
 *     }),
 *   ],
 * })
 * export class AppModule {}
 * ```
 */
export interface IGatewayModuleOptions {
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
   * `IErrorBodyFactory`. When omitted, `DefaultErrorBodyFactory` is used.
   */
  readonly errorBodyFactory?: Type<IErrorBodyFactory>;
}
