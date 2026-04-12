import { IncomingMessage } from 'node:http';

import {
  ArgumentsHost,
  Catch,
  ExceptionFilter,
  HttpException,
  HttpStatus,
  Inject,
  Injectable,
  Logger,
  Optional,
} from '@nestjs/common';
import { HttpAdapterHost } from '@nestjs/core';

import { IErrorContext } from '../interfaces/error-context.interface';
import { IErrorReporter } from '../interfaces/error-reporter.interface';
import { ERROR_REPORTER } from '../tokens';

/**
 * Lowest HTTP status that is logged at `error` level and forwarded to the
 * optional `IErrorReporter`. Anything strictly below this is a deliberate
 * client-facing signal (4xx) and gets emitted silently so legitimate
 * `NotFoundException`, `UnauthorizedException`, etc. do not flood the logs
 * or the error reporter.
 */
const SERVER_ERROR_THRESHOLD = 500;

/**
 * Global HTTP exception filter wired in via `ErrorsModule.forRoot()`.
 * @remarks
 * **Scope:** HTTP transport only. RPC handlers are not covered here —
 * gateway-facing RPC endpoints are served by
 * `GatewayExceptionFilter` (from `@zerly/gateway-sdk`, attached locally
 * via `@ApiGateway`), and service-to-service RPC uses vanilla NestJS
 * `RpcException` handling. Consolidating those paths into this filter
 * would just replay what Nest already does out of the box.
 *
 * **Wire contract:** whatever Nest's own `HttpException` exposes. This
 * filter deliberately does not invent a custom body shape — it reads
 * `exception.getStatus()` and `exception.getResponse()` and forwards
 * them verbatim, so a `NotFoundException('User not found')` yields the
 * exact body a consumer would see from a Nest app without this filter.
 * Unknown throws collapse to `500 Internal Server Error` with the
 * Nest-default body shape.
 *
 * **Why this filter exists at all**, given that Nest already has a
 * built-in HTTP exception filter: logging and the optional error
 * reporter hook. Nest's default filter is silent for everything except
 * uncaught errors, and it has no extension point for Sentry/Datadog.
 * This filter adds:
 *   1. Structured error-level logging for every 5xx response with
 *      method/url context, so ops can correlate failures without
 *      reaching for request IDs.
 *   2. Optional `IErrorReporter` invocation on 5xx for integrations
 *      that forward crashes to an external APM.
 * Both hooks are skipped for 4xx so deliberate client-facing rejections
 * stay quiet.
 *
 * **SOLID notes:** the filter collaborates with two abstractions only —
 * `HttpAdapterHost` (supplied by Nest) and the optional
 * `IErrorReporter` port. Status/body resolution is a pure function of
 * the exception (`resolve(exception)`) and emits no side effects;
 * logging and reporting are orchestrated once at the top of `catch`.
 * Adding a new reporter, renaming a log field, or changing the
 * 5xx threshold each touches exactly one location.
 */
@Injectable()
@Catch()
export class AllExceptionsFilter implements ExceptionFilter {
  private readonly logger = new Logger(AllExceptionsFilter.name);

  public constructor(
    private readonly httpAdapterHost: HttpAdapterHost,
    @Optional() @Inject(ERROR_REPORTER) private readonly errorReporter?: IErrorReporter,
  ) {}

  /**
   * Entry point Nest invokes for every uncaught exception. RPC and WS
   * contexts are ignored — those transports have their own filters and
   * are served by different layers in this stack (see class docstring).
   */
  public catch(exception: unknown, host: ArgumentsHost): void {
    if (host.getType() !== 'http') {
      return;
    }

    const { status, body } = this.resolve(exception);
    const ctx = host.switchToHttp();
    const request = ctx.getRequest<IncomingMessage>();
    const { httpAdapter } = this.httpAdapterHost;
    const method = httpAdapter.getRequestMethod(request);
    const url = httpAdapter.getRequestUrl(request);

    if (status >= SERVER_ERROR_THRESHOLD) {
      this.reportServerError(exception, { type: 'http', method, url });
    }

    httpAdapter.reply(ctx.getResponse(), body, status);
  }

  /**
   * Maps an arbitrary thrown value to the `(status, body)` pair that
   * will be written back to the client. Split out so `catch` reads as
   * an orchestration flow and so unit tests can assert the mapping in
   * isolation without mocking `HttpAdapterHost`.
   * @remarks
   * Two cases:
   *   - `HttpException`: trust Nest completely. Status comes from
   *     `getStatus()` and the body from `getResponse()`. When the user
   *     passed a string (e.g. `new NotFoundException('gone')`), Nest
   *     wraps it in its standard `{statusCode, message, error}` envelope
   *     via `getResponse()`'s internal normalization — but only when
   *     the exception was constructed that way. We re-normalize string
   *     returns into the same shape so callers always see a JSON object
   *     regardless of construction style.
   *   - Anything else: `500 Internal Server Error` with Nest's default
   *     body shape. The original error's message is intentionally NOT
   *     leaked — that prevents stack-trace / internal-detail exposure
   *     on unexpected failures. Operators still see the full exception
   *     via the logger hook.
   */
  private resolve(exception: unknown): { status: number; body: object } {
    if (exception instanceof HttpException) {
      const status = exception.getStatus();
      const response = exception.getResponse();
      const body =
        typeof response === 'string' ? { statusCode: status, message: response } : response;

      return { status, body };
    }

    return {
      status: HttpStatus.INTERNAL_SERVER_ERROR,
      body: {
        statusCode: HttpStatus.INTERNAL_SERVER_ERROR,
        message: 'Internal server error',
      },
    };
  }

  /**
   * Logs a 5xx at error level and forwards it to the optional reporter.
   * Kept as a single method so the two side effects share one call
   * site — neither can accidentally fire without the other, and the
   * threshold check stays in `catch` where it can be reasoned about
   * next to the response write.
   */
  private reportServerError(exception: unknown, context: IErrorContext): void {
    this.logger.error({
      msg: 'HTTP Server Error',
      err: exception,
      method: context.method,
      url: context.url,
    });

    this.errorReporter?.report(exception, context);
  }
}
