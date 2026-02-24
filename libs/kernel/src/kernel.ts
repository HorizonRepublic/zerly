import 'reflect-metadata';
import { Logger, NestApplicationOptions, Type } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';
import { NestFactory } from '@nestjs/core';
import { FastifyAdapter, NestFastifyApplication } from '@nestjs/platform-fastify';

import { CommandFactory } from 'nest-commander';
import { CommandFactoryRunOptions } from 'nest-commander/src/command-factory.interface';
import * as qs from 'qs';
import {
  catchError,
  defer,
  from,
  map,
  mergeMap,
  Observable,
  of,
  shareReplay,
  tap,
  throwError,
} from 'rxjs';

import { APP_CONFIG, AppState, IAppConfig } from '@zerly/config';

import { AppMode } from './enum/app-mode.enum';
import { HeaderKeys } from './enum/header-keys.enum';
import { genReqId } from './helpers/trace-id.helper';
import { KernelModule } from './kernel.module';
import { APP_REF_SERVICE, APP_STATE_SERVICE } from './tokens';
import { IAppRefService, IAppStateService, IKernelInitOptions } from './types';

/**
 * The Kernel is the core entry point of the application.
 *
 * It is responsible for:
 * 1. Determining the runtime environment (Node.js vs Bun).
 * 2. dynamically loading the appropriate HTTP adapter (Fastify vs BunAdapter).
 * 3. Bootstrapping the NestJS IoC container.
 * 4. initializing core Kernel services (AppRef, AppState).
 * 5. Managing the application lifecycle state transitions.
 *
 * This class implements the Singleton pattern to ensure only one Kernel instance
 * exists during the application lifecycle.
 *
 * @class
 * @final
 */
export class Kernel {
  /**
   * Caches the observable of the bootstrap process to prevent multiple initializations.
   * @private
   * @static
   */
  private static bootstrapResult$?: Observable<Kernel>;

  /**
   * The singleton instance of the Kernel.
   * @private
   * @static
   */
  private static instance?: Kernel;

  private appRef!: IAppRefService;
  private appState!: IAppStateService;

  private readonly logger = new Logger(Kernel.name);

  /**
   * Default configuration options for the NestJS application instance.
   * @constant
   */
  private readonly defaultOptions: NestApplicationOptions = {
    abortOnError: false,
    autoFlushLogs: true,
    bufferLogs: true,
  };

  /**
   * Private constructor to enforce a Singleton pattern.
   * Use {@link Kernel.init} or {@link Kernel.standalone} instead.
   * @private
   */
  private constructor() {}

  /**
   * Initializes and bootstraps the main application (HTTP Server).
   *
   * This method handles the full lifecycle: Adapter Resolution -> App Creation ->
   * State Initialization -> Event Listening -> HTTP Server Start.
   *
   * @param {Type<unknown>} appModule - The root module of the application (usually AppModule).
   * @param options
   * @returns {Observable<Kernel>} An observable that emits the initialized Kernel instance.
   *
   * @example
   * ```typescript
   * import { Kernel } from '@zerly/kernel';
   * import { AppModule } from './app/app.module';
   *
   * Kernel.init(AppModule);
   * ```
   */
  public static init(
    appModule: Type<unknown>,
    options: IKernelInitOptions = {},
  ): Observable<Kernel> {
    const opts: IKernelInitOptions = {
      // default options
      mode: AppMode.Server,

      // override defaults with user-provided options
      ...options,
    };

    if (opts.mode === AppMode.Cli) return this.standalone(appModule);

    const kernel = (this.instance ??= new Kernel());

    if (this.bootstrapResult$) return this.bootstrapResult$;

    this.bootstrapResult$ = kernel.bootstrap$(appModule).pipe(
      map(() => kernel),
      shareReplay(1),
    );

    this.bootstrapResult$.subscribe({
      error: (err) => {
        kernel.handleBootstrapError(err, 'Standard');
      },
    });

    return this.bootstrapResult$;
  }

  /**
   * Initializes the application in Standalone mode (Application Context only).
   *
   * Useful for CLI commands, cron jobs, or microservices that do not require
   * an HTTP listener.
   *
   * @param {Type<unknown>} appModule - The root module of the application.
   * @returns {Observable<Kernel>} An observable that emits the initialized Kernel instance.
   *
   * @example
   * ```typescript
   * Kernel.standalone(WorkerModule).subscribe();
   * ```
   */
  private static standalone(appModule: Type<unknown>): Observable<Kernel> {
    const kernel = new Kernel();

    // Standalone does not share the global bootstrapResult$ to allow multiple contexts if needed
    const bootstrap$ = kernel.bootstrapStandalone$(appModule).pipe(
      map(() => kernel),
      shareReplay(1),
    );

    bootstrap$.subscribe({
      error: (err) => {
        kernel.handleBootstrapError(err, 'Standalone');
      },
    });

    return bootstrap$;
  }

  /**
   * Internal bootstrap pipeline for standard HTTP applications.
   *
   * Flow:
   * 1. Resolve HTTP Adapter (Async)
   * 2. Create Nest Application
   * 3. Register Kernel Services
   * 4. Update State -> Created
   * 5. Listen on Port
   * 6. Update State -> Listening
   *
   * @private
   * @param {Type<unknown>} appModule - The root module.
   * @returns {Observable<void>} Observable stream of the bootstrap process.
   */
  private bootstrap$(appModule: Type<unknown>): Observable<void> {
    const adapter = new FastifyAdapter({
      genReqId,
      requestIdHeader: HeaderKeys.TraceId,
      bodyLimit: 10 * 1024 * 1024,
      onProtoPoisoning: 'error',
      onConstructorPoisoning: 'error',
      trustProxy: true,
      keepAliveTimeout: 65000, // 65 sec
      disableRequestLogging: true,
      exposeHeadRoutes: true,
      forceCloseConnections: false,
      routerOptions: {
        ignoreDuplicateSlashes: true,
        ignoreTrailingSlash: true,
        querystringParser: (str): ReturnType<typeof qs.parse> => qs.parse(str),
      },
    });

    return from(
      NestFactory.create<NestFastifyApplication>(
        KernelModule.forServe(appModule),
        adapter,
        this.defaultOptions,
      ),
    ).pipe(
      tap((app) => {
        app.enableShutdownHooks();
        this.registerKernelServices(app);
      }),
      mergeMap(() => this.appState.setState$(AppState.Created)),
      mergeMap(() => this.startHttpServer$()),
      catchError((err) => throwError(() => new Error(`Bootstrap sequence failed: ${err.message}`))),
    );
  }

  /**
   * Internal bootstrap pipeline for standalone contexts.
   *
   * Uses nest-commander to handle CLI commands.
   *
   * @private
   * @param {Type<unknown>} standaloneModule - The root module.
   * @returns {Observable<void>} Observable stream of the context creation.
   */
  private bootstrapStandalone$(standaloneModule: Type<unknown>): Observable<void> {
    return defer(() => {
      // Remove the --cli flag from argv so the nest-commander doesn't complain about an unknown option
      process.argv = process.argv.filter((arg) => arg !== '--cli');

      const cliOptions: CommandFactoryRunOptions = {
        ...this.defaultOptions,
        bufferLogs: false,
        logger: ['error', 'warn'],
      };

      return CommandFactory.run(KernelModule.forStandalone(standaloneModule), cliOptions);
    }).pipe(
      mergeMap(() => of(void 0)),
      catchError((err) =>
        throwError(() => new Error(`Standalone bootstrap failed: ${err.message}`)),
      ),
    );
  }

  /**
   * Extracts and registers core kernel services (AppRef, AppState) from the IoC container.
   * Sets the global application reference.
   *
   * @private
   * @param {any} app - The NestJS application instance.
   */
  private registerKernelServices(app: NestFastifyApplication): void {
    this.appRef = app.get(APP_REF_SERVICE);
    this.appState = app.get(APP_STATE_SERVICE);
    this.appRef.set(app);
  }

  /**
   * Starts the HTTP server based on the configuration.
   *
   * @private
   * @returns {Observable<void>} Observable that completes when the server is listening.
   */
  private startHttpServer$(): Observable<void> {
    return defer(() => {
      const app = this.appRef.get();
      const config = app.get(ConfigService).getOrThrow<IAppConfig>(APP_CONFIG);

      return from(app.listen(config.port, config.host)).pipe(
        mergeMap(() => this.appState.setState$(AppState.Listening)),
      );
    });
  }

  /**
   * Centralized error handler for bootstrap failures.
   * Logs the error and terminates the process with a failure code.
   *
   * @private
   * @param {unknown} err - The error object.
   * @param {string} context - The bootstrap context (e.g., 'Standard', 'Standalone').
   */
  private handleBootstrapError(err: unknown, context: string): void {
    this.logger.error(`🚨 ${context} Kernel bootstrap failed!`);

    if (err instanceof Error) {
      this.logger.error(err.message, err.stack);
    } else {
      this.logger.error(String(err));
    }

    process.exit(1);
  }
}
