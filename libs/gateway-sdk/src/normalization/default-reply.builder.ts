import { Injectable } from '@nestjs/common';

import type { IGatewayReplyBuilder } from './contracts/reply-builder.interface';
import type { IGatewayErrorBody } from '../types/gateway-error-body.interface';
import type { IGatewayReply } from '../types/gateway-reply.interface';

/**
 * Default `IGatewayReplyBuilder` implementation. Produces plain JSON
 * envelopes with no content negotiation and RFC 7807 content type for errors.
 * @remarks
 * This class is the only place in the SDK allowed to construct `IGatewayReply`
 * values — see spec §6.2. All other code paths (response interceptor,
 * exception filter) delegate here via the `IGatewayReplyBuilder` interface so
 * that envelope shape stays a single-source-of-truth concern. Whenever the
 * wire format evolves, this is the single file to audit.
 *
 * Success replies ship with an empty headers map; the gateway layer merges
 * transport-level defaults (`Content-Type`, `X-Request-Id`) before the bytes
 * hit the socket. Error replies carry `application/problem+json` so RFC 7807
 * consumers can recognize the payload without peeking at the body.
 *
 * Bind a custom implementation against the `GATEWAY_REPLY_BUILDER` token
 * from `../tokens/gateway-tokens.constant` when you need alternative wire
 * formats, content negotiation, or cross-cutting reply enrichment (for
 * example an always-appended `X-Api-Version` header).
 * @example
 * ```ts
 * import { GatewayModule } from '@zerly/gateway-sdk';
 * import { MyReplyBuilder } from './my-reply.builder';
 *
 * GatewayModule.forRoot({ replyBuilder: MyReplyBuilder });
 * ```
 */
@Injectable()
export class DefaultGatewayReplyBuilder implements IGatewayReplyBuilder {
  public success<TBody>(status: number, body: TBody | null): IGatewayReply<TBody> {
    return {
      status,
      headers: {},
      /*
       * Coerce `undefined` to `null` so the wire envelope shape stays
       * deterministic across void and explicit-null handler returns.
       * `JSON.stringify` omits `undefined` fields entirely, which would
       * produce different byte shapes for semantically identical 204
       * responses — a real bug for the Go gateway that decodes the reply
       * into a fixed struct.
       */
      body: body ?? null,
    };
  }

  public error(status: number, body: IGatewayErrorBody): IGatewayReply<IGatewayErrorBody> {
    return {
      status,
      headers: { 'content-type': 'application/problem+json' },
      body,
    };
  }
}
