import {
  BeforeApplicationShutdown,
  Inject,
  Injectable,
  Logger,
  OnApplicationShutdown,
} from '@nestjs/common';
import { NestFastifyApplication } from '@nestjs/platform-fastify';

import * as compression from '@fastify/compress';
import { FastifyCompressOptions } from '@fastify/compress';
import * as helmet from '@fastify/helmet';
import { FastifyHelmetOptions } from '@fastify/helmet';
import { FastifyPluginCallback } from 'fastify';

import { getRuntime, getRuntimeVersion } from '../helpers/get-runtime.helper';
import { APP_STATE_SERVICE } from '../tokens';
import { IAppStateService } from '../types';

import { RoutesInspectorProvider } from './routes-inspector.provider';

@Injectable()
export class KernelProvider implements OnApplicationShutdown, BeforeApplicationShutdown {
  private readonly logger = new Logger(KernelProvider.name);
  private shutdownTimer?: NodeJS.Timeout;
  private readonly shutdownTimeoutMs = 25_000;

  public constructor(
    @Inject(APP_STATE_SERVICE)
    private readonly appStateService: IAppStateService,
    private readonly routesInspector: RoutesInspectorProvider,
  ) {
    this.appStateService.onCreated(async (app: NestFastifyApplication) => {
      app.enableCors();

      await Promise.all([
        app.register(helmet as unknown as FastifyPluginCallback<FastifyHelmetOptions>),
        app.register(compression as unknown as FastifyPluginCallback<FastifyCompressOptions>, {
          encodings: ['gzip', 'deflate'],
        }),
      ]);
    });

    this.appStateService.onListening(async (app: NestFastifyApplication) => {
      const used = process.memoryUsage();
      const runtime = getRuntime();
      const url = await app.getUrl();

      this.routesInspector.inspect();

      this.logger.log(`Application is listening on ${url}`);

      this.logger.debug(
        `Runtime: ${runtime.charAt(0).toUpperCase() + runtime.slice(1)} ${getRuntimeVersion()}`,
      );

      this.logger.debug(this.formatMemoryUsage(used));
    });
  }

  public beforeApplicationShutdown(signal?: string): void {
    this.logger.log(`Received signal: ${signal}. Starting graceful shutdown sequence...`);

    this.shutdownTimer = setTimeout(() => {
      this.logger.error(
        `Shutdown timed out after ${this.shutdownTimeoutMs}ms. Forcing exit (exit code 1).`,
      );
      process.exit(1);
    }, this.shutdownTimeoutMs);

    this.shutdownTimer.unref();
  }

  public onApplicationShutdown(signal?: string): void {
    if (this.shutdownTimer) {
      clearTimeout(this.shutdownTimer);
    }

    const used = process.memoryUsage();

    this.logger.log(
      `Application shutdown complete (${signal ?? 'none'}). ${this.formatMemoryUsage(used)}`,
    );
  }

  private formatMemoryUsage(used: NodeJS.MemoryUsage): string {
    return `Memory usage: ${Math.round((used.rss / 1024 / 1024) * 100) / 100} MB`;
  }
}
