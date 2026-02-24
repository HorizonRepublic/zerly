import { NestFastifyApplication } from '@nestjs/platform-fastify';

import { Observable } from 'rxjs';

import { AppState } from '@zerly/config';

/**
 * Service for managing application lifecycle states and executing callbacks
 * at specific initialization phases.
 *
 * This service allows modules to register callbacks that execute during
 * key application lifecycle events:
 * - `Created`: App instance created but not listening yet
 * - `Listening`: App is ready and listening for requests.
 *
 * Callbacks are executed in priority order, allowing fine-grained control
 * over an initialization sequence.
 */
export interface IAppStateService {
  /**
   * Register a callback to execute when the application has been created,
   * but before it starts listening for requests.
   *
   * This is the ideal place for:
   * - Module initialization
   * - Database connections
   * - Cache warming
   * - Service configuration.
   *
   * @param cb Function returning sync/async operation or Observable.
   * @param priority Execution priority (lower numbers = higher priority)
   * Default: 0
   * Range: -Infinity to +Infinity.
   *
   * @example
   * ```TypeScript
   * // High priority (executes first)
   * appState.onCreated(() => initDatabase(), -10);
   *
   * // Normal priority
   * appState.onCreated(() => warmCache());
   *
   * // Low priority (executes last)
   * appState.onCreated(() => logInitComplete(), 100);
   * ```
   */
  onCreated(cb: IStateCallback, priority?: number): void;

  /**
   * Register a callback to execute when the application is ready
   * and listening for incoming requests.
   *
   * This is the ideal place for:
   * - Final health checks
   * - Logging ready status
   * - Notifying external services
   * - Starting background jobs.
   *
   * @param cb Function returning sync/async operation or Observable.
   * @param priority Execution priority (lower numbers = higher priority)
   * Default: 0
   * Range: -Infinity to +Infinity.
   *
   * @example
   * ```TypeScript
   * // Critical check (executes first)
   * appState.onListening(() => healthCheck(), -100);
   *
   * // Normal logging
   * appState.onListening(() => logger.log('Server ready'));
   *
   * // Background tasks (executes last)
   * appState.onListening(() => startCronJobs(), 50);
   * ```
   */
  onListening(cb: IStateCallback, priority?: number): void;

  /**
   * Transition the application to a new state and execute all
   * registered callbacks for that state in priority order.
   *
   * Callbacks are executed sequentially, waiting for each to complete
   * before proceeding to the next. If any callback fails, the error
   * is logged but execution continues.
   *
   * @internal
   * @param state New application state.
   * @returns Observable that completes when all callbacks finish.
   */
  setState$(state: AppState): Observable<void>;

  /**
   * Current application state.
   *
   * States:
   * - `NotReady`: Initial state, nothing initialized yet
   * - `Created`: App created, modules can initialize
   * - `Listening`: App ready and accepting requests.
   */
  readonly state: AppState;
}

export interface IPrioritizedCallback {
  callback: IStateCallback;

  readonly priority: number;
}

/**
 * Type definition for application state callback functions.
 *
 * This callback type is used throughout the application lifecycle management system
 * to define functions that execute during key application state transitions.
 *
 * The callback receives a NestJS application instance and can return:
 * - `void` for synchronous operations
 * - `Promise<void>` for asynchronous operations
 * - `Observable<void>` for reactive operations.
 *
 * @param app The NestJS application instance, fully configured and ready for use.
 *
 * @returns One of:
 * - `void` - For immediate synchronous execution
 * - `Promise<void>` - For asynchronous operations (async/await compatible)
 * - `Observable<void>` - For reactive programming patterns using RxJS.
 *
 * @example
 * ```TypeScript
 * // Synchronous callback
 * const syncCallback: IStateCallback = (app) => {
 *   console.log('Application ready');
 * };
 *
 * // Asynchronous callback with Promise
 * const asyncCallback: IStateCallback = async (app) => {
 *   await initializeDatabase();
 *   console.log('Database initialized');
 * };
 *
 * // Observable-based callback
 * const observableCallback: IStateCallback = (app) => {
 *   return from(setupHealthChecks()).pipe(
 *     tap(() => console.log('Health checks configured'))
 *   );
 * };
 *
 * // Usage with AppStateService
 * appStateService.onCreated(syncCallback);
 * appStateService.onListening(asyncCallback, -10); // High priority
 * ```
 *
 * @see IAppStateService - Service that uses these callbacks
 * @see AppState - Application states during which callbacks execute
 *
 * @since 1.0.0
 */
export type IStateCallback = (
  app: NestFastifyApplication,
) => Observable<void> | Promise<void> | void;
