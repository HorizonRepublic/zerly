import {
  Catch,
  Inject,
  Injectable,
  type ArgumentsHost,
  type ExceptionFilter,
} from '@nestjs/common';

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

  public catch(exception: unknown, host: ArgumentsHost): IGatewayReply<IGatewayErrorBody> {
    const request = host.switchToRpc().getData<IGatewayRequest>();
    const { status, body } = this.errorBodyFactory.build(exception, request);

    return this.replyBuilder.error(status, body);
  }
}
