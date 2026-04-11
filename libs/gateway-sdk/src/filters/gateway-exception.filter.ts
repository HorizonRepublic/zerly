import {
  Catch,
  Inject,
  Injectable,
  type ArgumentsHost,
  type ExceptionFilter,
} from '@nestjs/common';

import { of, type Observable } from 'rxjs';

import {
  GATEWAY_ERROR_BODY_FACTORY,
  GATEWAY_REPLY_BUILDER,
} from '../tokens/gateway-tokens.constant';

import type { IErrorBodyFactory } from '../normalization/contracts/error-body-factory.interface';
import type { IGatewayReplyBuilder } from '../normalization/contracts/reply-builder.interface';
import type { IGatewayErrorBody } from '../types/gateway-error-body.interface';
import type { IGatewayReply } from '../types/gateway-reply.interface';
import type { IGatewayRequest } from '../types/gateway-request.interface';

/**
 * Catches any exception thrown from an `@ApiGateway`-decorated handler and
 * serializes it into an `IGatewayReply` envelope with the appropriate HTTP
 * status.
 * @remarks
 * **Locally attached** via `@UseFilters(GatewayExceptionFilter)` inside the
 * `@ApiGateway` decorator — never registered globally. Because it is bound
 * only to gateway-exposed handlers, every invocation of this filter is
 * guaranteed to originate from one, so there is no need to discriminate
 * between gateway and non-gateway exception origins at catch time.
 *
 * Policy is delegated to injected contracts:
 *   - `IErrorBodyFactory` via `GATEWAY_ERROR_BODY_FACTORY` — recognizes
 *     `DomainException` via duck-typing and extracts structured fields
 *   - `IGatewayReplyBuilder` via `GATEWAY_REPLY_BUILDER` — assembles the
 *     outbound envelope
 *
 * Pipe and guard exceptions are also caught here: NestJS runs exception
 * filters after pipes/guards throw, so validation errors (e.g., from
 * typia pipes) are correctly serialized into structured HTTP responses
 * rather than surfacing as raw 500s on the client side.
 * @example
 * ```ts
 * // Attached automatically by @ApiGateway — consumers never reference this
 * // class directly. Throw any DomainException subclass inside a handler
 * // and the filter produces the matching envelope.
 * @ApiGateway({ pattern: 'users.get', method: 'GET', path: '/users/:id' })
 * getUser(@GatewayParam('id') id: string) {
 *   const user = this.users.findById(id);
 *   if (!user) {
 *     throw new NotFoundException({ code: 'USER_NOT_FOUND' });
 *   }
 *   return user;
 * }
 * ```
 */
@Catch()
@Injectable()
export class GatewayExceptionFilter implements ExceptionFilter {
  public constructor(
    @Inject(GATEWAY_REPLY_BUILDER)
    private readonly replyBuilder: IGatewayReplyBuilder,
    @Inject(GATEWAY_ERROR_BODY_FACTORY)
    private readonly errorBodyFactory: IErrorBodyFactory,
  ) {}

  /**
   * Serializes the exception into an `IGatewayReply` envelope and emits
   * it as a successful Observable value.
   * @remarks
   * The return type is `Observable<IGatewayReply<IGatewayErrorBody>>`
   * — NOT a bare envelope — because NestJS microservices transports
   * (including `@horizon-republic/nestjs-jetstream`) serialize the
   * value each Observable emits as the RPC reply body. Returning a
   * plain object from an RPC exception filter makes Nest wrap it in
   * the default transport error envelope (`{err, response,
   * isDisposed}`), which the Go gateway's decoder cannot parse as a
   * `GatewayReply` — it sees `status: 0` and surfaces the whole
   * response as a 502 Bad Gateway.
   *
   * Wrapping the envelope in `of(...)` makes Nest treat it as a
   * normal reply — exactly mirroring what the response interceptor
   * does on the success path — so the wire envelope shape stays
   * identical regardless of whether the handler returned or threw.
   */
  public catch(
    exception: unknown,
    host: ArgumentsHost,
  ): Observable<IGatewayReply<IGatewayErrorBody>> {
    const request = host.switchToRpc().getData<IGatewayRequest>();
    const { status, body } = this.errorBodyFactory.build(exception, request);

    return of(this.replyBuilder.error(status, body));
  }
}
