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
import { ConfigService } from '@nestjs/config';
import { HttpAdapterHost } from '@nestjs/core';

import { Observable, throwError } from 'rxjs';

import { APP_CONFIG, Environment, IAppConfig } from '@zerly/config';

import { adaptTypiaError } from '../adapters/typia-validation.adapter';
import { HTTP_ERROR_CODES } from '../constants/http-error-codes.constant';
import { ApiException } from '../exceptions/api.exception';
import { DomainException } from '../exceptions/domain.exception';
import { IErrorContext } from '../interfaces/error-context.interface';
import { IErrorReporter } from '../interfaces/error-reporter.interface';
import { IErrorResponse } from '../interfaces/error-response.interface';
import { ERROR_REPORTER } from '../tokens';

import type { RpcException as RpcExceptionBase } from '@nestjs/microservices';

type RpcExceptionCtor = typeof RpcExceptionBase;

// Optional peer dep — loaded at module init, undefined if not installed
let rpcExceptionCtor: RpcExceptionCtor | undefined;

try {
  // eslint-disable-next-line @typescript-eslint/no-require-imports,@typescript-eslint/naming-convention
  rpcExceptionCtor = (require('@nestjs/microservices') as { RpcException: RpcExceptionCtor })
    .RpcException;
} catch {
  // @nestjs/microservices is not installed — RPC wrapping unavailable
}

interface IResolvedError {
  code: Uppercase<string>;
  details?: Record<string, unknown>;
  internalDetails?: Record<string, unknown>;
  httpStatus: number;
}

@Injectable()
@Catch()
export class AllExceptionsFilter implements ExceptionFilter {
  private readonly logger = new Logger(AllExceptionsFilter.name);
  private readonly isProd: boolean;

  public constructor(
    private readonly httpAdapterHost: HttpAdapterHost,
    configService: ConfigService,
    @Optional() @Inject(ERROR_REPORTER) private readonly errorReporter?: IErrorReporter,
  ) {
    const config = configService.get<IAppConfig>(APP_CONFIG);

    this.isProd = config !== undefined ? config.env === Environment.Production : true;
  }

  public catch(exception: unknown, host: ArgumentsHost): void | Observable<unknown> {
    const type = host.getType();

    if (type === 'http') {
      this.handleHttp(exception, host);
      return;
    }

    if (type === 'rpc') {
      return this.handleRpc(exception);
    }

    this.logger.error({ msg: `Unhandled exception context type: ${type}`, err: exception });
  }

  private handleHttp(exception: unknown, host: ArgumentsHost): void {
    const { httpAdapter } = this.httpAdapterHost;
    const ctx = host.switchToHttp();
    const req = ctx.getRequest<IncomingMessage>();
    const method = httpAdapter.getRequestMethod(req);
    const url = httpAdapter.getRequestUrl(req);

    const resolved = this.resolveException(exception);

    if (resolved.httpStatus >= 500) {
      this.logger.error({ msg: 'HTTP Server Error', err: exception, req: { method, url } });
      const context: IErrorContext = { type: 'http', method, url };

      this.errorReporter?.report(exception, context);
    }

    const body: IErrorResponse = {
      code: resolved.code,
      timestamp: new Date().toISOString(),
      requestId: null,
    };

    if (resolved.details !== undefined) body.details = resolved.details;
    if (!this.isProd && resolved.internalDetails !== undefined)
      body.internal = resolved.internalDetails;

    httpAdapter.reply(ctx.getResponse(), body, resolved.httpStatus);
  }

  private handleRpc(exception: unknown): Observable<unknown> {
    if (exception instanceof DomainException) {
      if (exception.httpStatus >= 500) {
        this.logger.error({ msg: 'RPC Domain Error', err: exception });
        const context: IErrorContext = { type: 'rpc' };

        this.errorReporter?.report(exception, context);
      }

      const payload = exception.toRpcPayload();

      return throwError(() =>
        rpcExceptionCtor !== undefined ? new rpcExceptionCtor(payload) : exception,
      );
    }

    if (rpcExceptionCtor !== undefined && exception instanceof rpcExceptionCtor) {
      return throwError(() => exception);
    }

    this.logger.error({ msg: 'RPC Unknown Error', err: exception });
    const context: IErrorContext = { type: 'rpc' };

    this.errorReporter?.report(exception, context);
    const payload = { code: 'INTERNAL_SERVER_ERROR' as Uppercase<string> };

    return throwError(() =>
      rpcExceptionCtor !== undefined ? new rpcExceptionCtor(payload) : exception,
    );
  }

  private resolveFromDomain(exception: DomainException): IResolvedError {
    const result: IResolvedError = { code: exception.code, httpStatus: exception.httpStatus };

    if (exception.details !== undefined) result.details = exception.details;
    if (exception.internalDetails !== undefined) result.internalDetails = exception.internalDetails;

    return result;
  }

  private resolveException(exception: unknown): IResolvedError {
    if (exception instanceof DomainException) {
      return this.resolveFromDomain(exception);
    }

    const typiaException = adaptTypiaError(exception);

    if (typiaException !== null) {
      const result: IResolvedError = {
        code: typiaException.code,
        httpStatus: typiaException.httpStatus,
      };

      if (typiaException.details !== undefined) result.details = typiaException.details;
      return result;
    }

    if (rpcExceptionCtor !== undefined && exception instanceof rpcExceptionCtor) {
      const rpcPayload = exception.getError();
      const domainException = ApiException.fromRpcPayload(rpcPayload);

      if (domainException !== null) {
        const result: IResolvedError = {
          code: domainException.code,
          httpStatus: domainException.httpStatus,
        };

        if (domainException.details !== undefined) result.details = domainException.details;
        return result;
      }

      return { code: 'INTERNAL_SERVER_ERROR', httpStatus: HttpStatus.INTERNAL_SERVER_ERROR };
    }

    if (exception instanceof HttpException) {
      const status = exception.getStatus();
      const code: Uppercase<string> = HTTP_ERROR_CODES[status] ?? 'INTERNAL_SERVER_ERROR';

      return { code, httpStatus: status };
    }

    const message = exception instanceof Error ? exception.message : String(exception);

    return {
      code: 'INTERNAL_SERVER_ERROR',
      httpStatus: HttpStatus.INTERNAL_SERVER_ERROR,
      internalDetails: { message },
    };
  }
}
