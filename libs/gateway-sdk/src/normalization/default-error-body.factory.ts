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
}

/**
 * Minimal local duck-type of NestJS's `HttpException` family.
 * @remarks
 * Recognizes any error exposing `getStatus()` and `getResponse()` methods —
 * the contract all NestJS built-in HTTP exceptions implement (e.g.
 * `NotFoundException`, `BadRequestException`, `UnauthorizedException`).
 *
 * Duck-typed to keep `@zerly/gateway-sdk` free of a hard value-level import
 * on `@nestjs/common`, mirroring the `IDomainExceptionLike` pattern used for
 * `@zerly/errors`. Consumers may throw any `HttpException` subclass from a
 * handler and the factory extracts status and body without the SDK needing
 * to reference `@nestjs/common` at runtime.
 *
 * Kept file-private by design: exposing it would invite consumers to extend
 * from it rather than from the real `HttpException` class.
 */
interface IHttpExceptionLike {
  readonly name: string;
  readonly message: string;
  getStatus(): number;
  getResponse(): string | Readonly<Record<string, unknown>>;
}

/**
 * Fields the factory projects directly into the top-level envelope and must
 * therefore strip from the `details` bag to avoid duplication.
 * @remarks
 * Centralized as a module-private constant so `extractHttpDetails` and any
 * future projection logic share the same source of truth. Declared as a
 * `Set<string>` because membership is the only operation performed on it.
 */
const HTTP_RESPONSE_PROJECTED_FIELDS: ReadonlySet<string> = new Set([
  'message',
  'error',
  'statusCode',
]);

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
 * Also recognizes NestJS's `HttpException` family (`NotFoundException`,
 * `BadRequestException`, `UnauthorizedException`, etc.) via the duck-typed
 * `IHttpExceptionLike` contract — any error exposing `getStatus()` and
 * `getResponse()` is treated as a first-class HTTP exception, with the
 * status extracted from `getStatus()` and the error code / message / details
 * extracted from `getResponse()`. This lets consumers throw plain Nest
 * exceptions without writing a `DomainException` adapter layer. Recognition
 * order is: `DomainException` first (Zerly-native path), then
 * `HttpException`, then the generic `500` fallback — so an exception
 * carrying both markers would be routed through the Zerly branch.
 *
 * Stack traces are NEVER included in the response body, regardless of
 * environment. Stack information stays server-side only — operators read
 * it from logs correlated by `requestId`, never from client-facing HTTP
 * responses. This removes an entire class of internal-path-leak bugs
 * (filesystem layouts, framework versions, vendor dir paths) that plague
 * dev-mode stack exposure in other HTTP frameworks.
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
  public build(error: unknown, request: IGatewayRequest): IErrorBodyBuildResult {
    if (this.isDomainException(error)) {
      return {
        status: error.status,
        body: this.buildFromDomain(error, request),
      };
    }

    if (this.isHttpException(error)) {
      return {
        status: error.getStatus(),
        body: this.buildFromHttpException(error, request),
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

  /**
   * Narrows `value` to the `IHttpExceptionLike` duck type.
   * @remarks
   * Checks the four properties that together identify an `HttpException`
   * subclass: `getStatus` and `getResponse` are the required contract methods
   * NestJS built-ins expose; `message` and `name` are inherited from `Error`
   * and must be strings to distinguish a real exception instance from a plain
   * object that accidentally exposes the two method keys (e.g. a mock, a
   * service stub, or a user-land value object). The check is intentionally
   * shallow — any value satisfying all four properties is accepted, which is
   * the desired open-closed behaviour for a contract-based duck type.
   */
  private isHttpException(value: unknown): value is IHttpExceptionLike {
    if (typeof value !== 'object' || value === null) {
      return false;
    }

    const candidate = value as Record<string, unknown>;

    return (
      typeof candidate['getStatus'] === 'function' &&
      typeof candidate['getResponse'] === 'function' &&
      typeof candidate['message'] === 'string' &&
      typeof candidate['name'] === 'string'
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

    if (error.details) {
      return { ...base, details: error.details };
    }

    return base;
  }

  /**
   * Assembles an `IGatewayErrorBody` from a duck-typed `HttpException`.
   * @remarks
   * Delegates error-code resolution, message extraction, and extra-field
   * projection to the three focused `extractHttp*` helpers so each concern
   * remains independently testable. Stack traces are intentionally NOT
   * attached — see the class-level godoc for the rationale.
   */
  private buildFromHttpException(
    error: IHttpExceptionLike,
    request: IGatewayRequest,
  ): IGatewayErrorBody {
    const response = error.getResponse();

    const base: IGatewayErrorBody = {
      error: this.extractHttpErrorCode(error, response),
      message: this.extractHttpMessage(error, response),
      requestId: request.meta.requestId,
    };

    return this.extractHttpDetails(response, base);
  }

  /**
   * Resolve a stable machine-readable error code for an HttpException.
   * @remarks
   * Prefers the `error` field NestJS built-in subclasses populate
   * (e.g. `NotFoundException` → `{ statusCode: 404, message, error: 'Not Found' }`)
   * and normalizes it to SCREAMING_SNAKE_CASE so clients can switch on a
   * stable identifier. When absent — e.g. custom `HttpException` subclasses
   * that emit only a string response body — falls back to deriving a code
   * from the exception class name (`CustomHttpException` → `CUSTOM_HTTP_EXCEPTION`).
   */
  private extractHttpErrorCode(
    error: IHttpExceptionLike,
    response: string | Readonly<Record<string, unknown>>,
  ): string {
    if (typeof response === 'object') {
      const candidate = (response as Record<string, unknown>)['error'];

      if (typeof candidate === 'string') {
        return candidate.trim().toUpperCase().replace(/\s+/gu, '_');
      }
    }

    return error.name
      .replace(/([A-Z])/gu, '_$1')
      .replace(/^_/u, '')
      .toUpperCase();
  }

  /**
   * Resolve a human-readable message for an HttpException.
   * @remarks
   * Handles the three shapes NestJS produces: a plain string response
   * (returned verbatim), an object with a `message: string` field (projected
   * directly), and an object with `message: string[]` — the shape
   * `ValidationPipe` emits for aggregated class-validator violations. Array
   * messages are joined with `", "` for wire display; richer clients that
   * want the original array should read it back from `details` (preserved
   * intact by `extractHttpDetails`). The `error.message` from the Error
   * prototype is used as a last resort.
   */
  private extractHttpMessage(
    error: IHttpExceptionLike,
    response: string | Readonly<Record<string, unknown>>,
  ): string {
    if (typeof response === 'string') {
      return response;
    }

    const candidate = (response as Record<string, unknown>)['message'];

    if (typeof candidate === 'string') {
      return candidate;
    }

    if (Array.isArray(candidate)) {
      return candidate.map(String).join(', ');
    }

    return error.message;
  }

  /**
   * Project extra response fields into the envelope's `details` bag.
   * @remarks
   * Strips `{ message, error, statusCode }` — already projected into the
   * top-level envelope — so `details` only carries EXTRA context the caller
   * attached to the response. When no extras remain, the base envelope is
   * returned unchanged to keep the wire shape minimal and satisfy
   * `exactOptionalPropertyTypes` (the `details` key must not appear with a
   * `{}` value).
   */
  private extractHttpDetails(
    response: string | Readonly<Record<string, unknown>>,
    base: IGatewayErrorBody,
  ): IGatewayErrorBody {
    if (typeof response !== 'object') {
      return base;
    }

    const extras: Record<string, unknown> = {};

    for (const [key, value] of Object.entries(response)) {
      if (!HTTP_RESPONSE_PROJECTED_FIELDS.has(key)) {
        extras[key] = value;
      }
    }

    if (Object.keys(extras).length === 0) {
      return base;
    }

    return { ...base, details: extras };
  }

  private buildFromUnknown(_error: unknown, request: IGatewayRequest): IGatewayErrorBody {
    return {
      error: 'INTERNAL_SERVER_ERROR',
      message: 'An unexpected error occurred',
      requestId: request.meta.requestId,
    };
  }
}
