import {
  Inject,
  Injectable,
  type CallHandler,
  type ExecutionContext,
  type NestInterceptor,
} from '@nestjs/common';
import { Reflector } from '@nestjs/core';
import { PATTERN_EXTRAS_METADATA } from '@nestjs/microservices/constants';

import { map, type Observable } from 'rxjs';

import { GATEWAY_REPLY_BUILDER, GATEWAY_STATUS_RESOLVER } from '../tokens/gateway-tokens.constant';

import type { IGatewayReplyBuilder } from '../normalization/contracts/reply-builder.interface';
import type { IStatusResolver } from '../normalization/contracts/status-resolver.interface';
import type { IGatewayHttpMeta } from '../types/gateway-http-meta.interface';

/**
 * Shape that `@MessagePattern`'s `extras.meta` is expected to hold for a
 * gateway-exposed handler.
 * @remarks
 * File-private, non-exported. Duck-types the `extras` record stored at
 * `PATTERN_EXTRAS_METADATA` by `@nestjs/microservices` so the interceptor
 * does not have to import the framework's internal `PatternMetadata`
 * descriptor and stays tolerant of additive schema changes.
 *
 * The two top-level keys are mutually exclusive in practice:
 *   - `http` — regular `@GatewayRoute` handler. The value populates
 *     routing metadata on the Go side and drives status resolution.
 *   - `verifier` — `@GatewayAuthVerifier` handler. Its presence
 *     alone tells the interceptor to wrap the return in a 200 reply;
 *     the fields of the object itself are only read on the gateway
 *     side.
 */
interface IExtrasWithGatewayMeta {
  readonly meta?: {
    readonly http?: IGatewayHttpMeta;
    readonly verifier?: object;
  };
}

/**
 * HTTP status used for every successful verifier reply. Verifiers
 * that need to signal anything other than success throw an
 * `HttpException` subclass, which `GatewayExceptionFilter` converts
 * into the appropriate error envelope — so the success path is
 * always 200, unambiguously.
 */
const VERIFIER_SUCCESS_STATUS = 200;

/**
 * Wraps the return value of an `@GatewayRoute`-decorated handler into an
 * `IGatewayReply` envelope, applying configured status-resolution rules.
 * @remarks
 * **Locally attached** via `@UseInterceptors(GatewayResponseInterceptor)`
 * inside the `@GatewayRoute` decorator — never registered globally. Because
 * it is bound only to gateway-exposed handlers, the interceptor never has
 * to discriminate between gateway and non-gateway calls: its mere presence
 * on the execution stack is proof of the former.
 *
 * HTTP metadata is read from `PATTERN_EXTRAS_METADATA` — the same NestJS
 * reflection key that `@MessagePattern(pattern, { meta: { http } })`
 * writes to. No custom reflection key is introduced, so HTTP routing
 * metadata has a single source of truth per handler.
 *
 * All normalization policy is delegated to injected contracts:
 *   - `IGatewayReplyBuilder` via `GATEWAY_REPLY_BUILDER`
 *   - `IStatusResolver` via `GATEWAY_STATUS_RESOLVER`
 *
 * The defensive `meta.http` guard covers the unsupported edge case in
 * which a consumer manually attaches this interceptor to a non-gateway
 * handler; rather than throwing at runtime, the interceptor passes the
 * handler output through untouched.
 * @example
 * ```ts
 * @GatewayRoute({ pattern: 'users.create', method: 'POST', path: '/users' })
 * createUser(@GatewayBody() dto: CreateUserDto) {
 *   return this.usersService.create(dto);
 * }
 * ```
 */
@Injectable()
export class GatewayResponseInterceptor implements NestInterceptor {
  public constructor(
    private readonly reflector: Reflector,
    @Inject(GATEWAY_REPLY_BUILDER)
    private readonly replyBuilder: IGatewayReplyBuilder,
    @Inject(GATEWAY_STATUS_RESOLVER)
    private readonly statusResolver: IStatusResolver,
  ) {}

  public intercept(context: ExecutionContext, next: CallHandler): Observable<unknown> {
    const extras = this.reflector.get<IExtrasWithGatewayMeta | undefined>(
      PATTERN_EXTRAS_METADATA,
      context.getHandler(),
    );
    const httpMeta = extras?.meta?.http;
    const isVerifier = extras?.meta?.verifier !== undefined;

    if (httpMeta !== undefined) {
      return next
        .handle()
        .pipe(
          map((value: unknown) =>
            this.replyBuilder.success(this.statusResolver.resolveSuccess(httpMeta, value), value),
          ),
        );
    }

    if (isVerifier) {
      return next
        .handle()
        .pipe(map((value: unknown) => this.replyBuilder.success(VERIFIER_SUCCESS_STATUS, value)));
    }

    return next.handle();
  }
}
