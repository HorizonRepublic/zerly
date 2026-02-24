import { randomUUID } from 'node:crypto';

import { Injectable } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';

import { Params as PinoParams } from 'nestjs-pino/params';
import * as pino from 'pino';

import { APP_CONFIG, Environment, IAppConfig } from '@zerly/config';

import { REDACTED_MSG, redactedPaths } from './const';

import type { LoggerOptions } from 'pino';

@Injectable()
export class LoggerConfigFactory {
  private readonly config: IAppConfig;

  public constructor(private readonly configService: ConfigService) {
    this.config = configService.getOrThrow<IAppConfig>(APP_CONFIG);
  }

  public get(): PinoParams {
    const isProduction = this.config.env === Environment.Production;

    const baseConfig: LoggerOptions = {
      level: isProduction ? 'info' : 'debug',
      name: this.config.name,
      redact: {
        censor: REDACTED_MSG,
        paths: redactedPaths,
      },
      serializers: {
        err: pino.stdSerializers.err,
        error: pino.stdSerializers.err,
        req: pino.stdSerializers.req,
        res: pino.stdSerializers.res,
      },
      timestamp: pino.stdTimeFunctions.isoTime,
      mixin: () => ({ traceId: randomUUID() }), // todo: to cls
    };

    const productionConfig: Partial<LoggerOptions> = isProduction
      ? {
          formatters: {
            level: (label: string) => ({ level: label }),
            log: (object: Record<string, unknown>) => ({
              ...object,
              service: this.config.name,
              env: this.config.env,
            }),
          },
        }
      : {};

    const developmentConfig: Partial<LoggerOptions> = !isProduction
      ? {
          hooks: {
            // silent route explorer logs to redefine it
            logMethod(inputArgs: unknown[], method: (...args: unknown[]) => void): void {
              if (inputArgs.length >= 1) {
                const arg = inputArgs[0];

                if (arg && typeof arg === 'object' && !Array.isArray(arg) && 'context' in arg) {
                  const context = (arg as Record<string, unknown>)['context'];

                  if (context === 'RouterExplorer' || context === 'RoutesResolver') return;
                }
              }

              method.apply(this, inputArgs);
            },
          },
          transport: {
            target: 'pino-pretty',
            options: {
              colorize: true,
              errorLikeObjectKeys: ['err', 'error'],
              errorProps: 'stack,name,message,type',
              ignore: 'pid,hostname,req,res,service,env',
              messageFormat: '[{context}] {msg}',
              singleLine: true,
              translateTime: 'SYS:dd.mm.yyyy HH:MM:ss.l',
            },
          },
        }
      : {};

    return {
      pinoHttp: {
        autoLogging: false,
        ...baseConfig,
        ...productionConfig,
        ...developmentConfig,
      },
    };
  }
}
