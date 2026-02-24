import { IncomingMessage } from 'http';

import {
  ArgumentsHost,
  Catch,
  ExceptionFilter,
  HttpException,
  HttpStatus,
  Logger,
} from '@nestjs/common';
import { HttpAdapterHost } from '@nestjs/core';

import { Observable, throwError } from 'rxjs';

/**
 * // todo: extract to separated package
 * Global Exception Filter.
 *
 * This filter acts as a centralized error handling mechanism for the entire application.
 * It catches all unhandled exceptions from HTTP requests, RPC calls, and other contexts,
 * providing a consistent logging strategy and response format.
 *
 * ## Key Responsibilities
 *
 * 1. **Context Awareness**: Differentiates between HTTP and RPC execution contexts.
 * 2. **Centralized Logging**: Logs all errors (5xx) and warnings (4xx) in a structured format.
 * 3. **Response Normalization**: Ensures HTTP clients receive a consistent JSON error response.
 * 4. **Transport Transparency**: For RPC, it logs the error but re-throws it to let the transport layer handle the response.
 *
 * ## Usage
 *
 * Register this filter globally in your application bootstrap logic (e.g., in `LoggerProvider`).
 *
 * ```typescript
 * app.useGlobalFilters(new AllExceptionsFilter(httpAdapterHost));
 * ```
 */
@Catch()
export class AllExceptionsFilter implements ExceptionFilter {
  private readonly logger = new Logger(AllExceptionsFilter.name);

  public constructor(private readonly httpAdapterHost: HttpAdapterHost) {}

  /**
   * Catches exceptions and delegates handling based on the execution context type.
   *
   * @param exception - The thrown exception object.
   * @param host - ArgumentsHost providing access to the execution context (HTTP, RPC, etc.).
   * @returns void for HTTP (response sent directly), or Observable for RPC (re-thrown).
   */
  public catch(exception: unknown, host: ArgumentsHost): void | Observable<unknown> {
    const type = host.getType();

    if (type === 'http') {
      this.handleHttp(exception, host);
      return;
    } else if (type === 'rpc') {
      return this.handleRpc(exception, host);
    }

    // Fallback for unhandled context types (e.g., WebSocket, GraphQL)
    this.logger.error({
      msg: `Unhandled exception context type: ${type}`,
      err: exception,
    });
  }

  /**
   * Handles exceptions in HTTP context.
   *
   * - Determines appropriate HTTP status code.
   * - Constructs a standardized JSON response body.
   * - Logs the error (Level: Error for 5xx, Warn for 4xx).
   * - Sends the response to the client using the underlying HTTP adapter.
   */
  private handleHttp(exception: unknown, host: ArgumentsHost): void {
    const { httpAdapter } = this.httpAdapterHost;
    const ctx = host.switchToHttp();
    const req = ctx.getRequest<IncomingMessage>();

    const httpStatus =
      exception instanceof HttpException ? exception.getStatus() : HttpStatus.INTERNAL_SERVER_ERROR;

    const responseBody = {
      statusCode: httpStatus,
      timestamp: new Date().toISOString(),
      path: httpAdapter.getRequestUrl(req),
      message: exception instanceof HttpException ? exception.message : 'Internal Server Error',
    };

    // Logging Strategy:
    // 5xx errors are critical and should be logged with stack traces.
    // 4xx errors are client-side issues and are skipped
    if (httpStatus >= 500) {
      this.logger.error({
        msg: 'HTTP Server Error',
        err: exception,
        req: {
          method: httpAdapter.getRequestMethod(req),
          url: httpAdapter.getRequestUrl(req),
        },
      });
    }

    httpAdapter.reply(ctx.getResponse(), responseBody, httpStatus);
  }

  /**
   * Handles exceptions in RPC context (Microservices).
   *
   * - Logs the error with RPC-specific context (payload data).
   * - Re-throws the exception to allow the underlying transport layer (e.g., NATS, Redis)
   *   to handle the error propagation to the caller.
   *
   * @returns An Observable that errors out, propagating the exception.
   */
  private handleRpc(exception: unknown, host: ArgumentsHost): Observable<unknown> {
    const ctx = host.switchToRpc();
    const data = ctx.getData();

    // Note: host.getHandler() might not be available in ExceptionFilters depending on the scope.
    // We log the available context data.

    this.logger.error(
      {
        msg: 'RPC Error',
        rpc: { data },
        err: exception,
      },
      exception instanceof Error ? exception.stack : undefined,
    );

    // Re-throw the exception so the RPC transport can return it to the client.
    return throwError(() => exception);
  }
}
