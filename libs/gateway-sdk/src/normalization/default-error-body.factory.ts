import { Injectable } from '@nestjs/common';

import { DEFAULT_STATUS_INTERNAL_ERROR } from '../constants/defaults.constant';

import type { IErrorBodyBuildResult } from './contracts/error-body-build-result.interface';
import type { IErrorBodyFactory } from './contracts/error-body-factory.interface';
import type { IGatewayErrorBody } from '../types/gateway-error-body.interface';
import type { IGatewayRequest } from '../types/gateway-request.interface';

/**
 * Minimal local duck-type of `@zerly/errors` `DomainException`.
 * @remarks
 * Kept as a local interface so that `@zerly/gateway-sdk` has **no hard import
 * dependency** on `@zerly/errors`. Recognition happens at runtime via the
 * `isDomainException` marker field — consumers that roll their own error
 * hierarchies, or that simply do not install `@zerly/errors`, still get
 * sensible `500` responses for plain `Error` throws, and their own exceptions
 * can opt in to rich mapping by setting the marker.
 *
 * Exporting this type would invite misuse (consumers extending from it rather
 * than from `DomainException`), so it is deliberately file-private.
 */
interface IDomainExceptionLike {
  readonly isDomainException: true;
  readonly status: number;
  readonly code: string;
  readonly message: string;
  readonly details?: Readonly<Record<string, unknown>>;
  readonly stack?: string;
}

/**
 * Default `IErrorBodyFactory` implementation.
 * @remarks
 * Recognizes `@zerly/errors`' `DomainException` via the `isDomainException`
 * marker field (duck-typed to avoid a hard import dependency — see
 * `IDomainExceptionLike`). Unknown throws — including non-`Error` values like
 * strings, numbers, or `null` — produce a generic `500` with the
 * `INTERNAL_SERVER_ERROR` code and a sanitized message; the real error
 * detail is expected to be logged server-side by the exception filter and
 * never travels over the wire.
 *
 * Stack traces are included in the response body only when `isProduction` is
 * `false`. In production, stacks are never exposed over the wire even for
 * recognized `DomainException` values.
 *
 * Bind a custom implementation against the `GATEWAY_ERROR_BODY_FACTORY`
 * token from `../tokens/gateway-tokens.constant` when integrating with
 * non-Zerly error libraries (e.g. `http-errors`) or applying project-specific
 * error-code mapping.
 * @example
 * ```ts
 * import { GatewayModule } from '@zerly/gateway-sdk';
 * import { MyErrorBodyFactory } from './my-error-body.factory';
 *
 * GatewayModule.forRoot({ errorBodyFactory: MyErrorBodyFactory });
 * ```
 */
@Injectable()
export class DefaultErrorBodyFactory implements IErrorBodyFactory {
  private readonly isProduction: boolean;

  public constructor(isProduction: boolean) {
    this.isProduction = isProduction;
  }

  public build(error: unknown, request: IGatewayRequest): IErrorBodyBuildResult {
    if (this.isDomainException(error)) {
      return {
        status: error.status,
        body: this.buildFromDomain(error, request),
      };
    }

    return {
      status: DEFAULT_STATUS_INTERNAL_ERROR,
      body: this.buildFromUnknown(error, request),
    };
  }

  private isDomainException(value: unknown): value is IDomainExceptionLike {
    return (
      typeof value === 'object' &&
      value !== null &&
      (value as { isDomainException?: unknown }).isDomainException === true
    );
  }

  private buildFromDomain(
    error: IDomainExceptionLike,
    request: IGatewayRequest,
  ): IGatewayErrorBody {
    const base: IGatewayErrorBody = {
      error: error.code,
      message: error.message,
      requestId: request.meta.requestId,
    };

    const withDetails: IGatewayErrorBody = error.details
      ? { ...base, details: error.details }
      : base;

    if (!this.isProduction && error.stack !== undefined) {
      return { ...withDetails, stack: error.stack };
    }

    return withDetails;
  }

  private buildFromUnknown(error: unknown, request: IGatewayRequest): IGatewayErrorBody {
    const base: IGatewayErrorBody = {
      error: 'INTERNAL_SERVER_ERROR',
      message: 'An unexpected error occurred',
      requestId: request.meta.requestId,
    };

    if (!this.isProduction && error instanceof Error && error.stack !== undefined) {
      return { ...base, stack: error.stack };
    }

    return base;
  }
}
