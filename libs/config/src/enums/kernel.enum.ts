import { LevelWithSilent } from 'pino';

/**
 * Represents various states of an application lifecycle.
 */
export enum AppState {
  /**
   * Indicates that the app was created from a factory, but not listened yet.
   */
  Created = 'created',
  /**
   * Indicates that the app in listening mode.
   */
  Listening = 'listening',
  /**
   * Indicates that app did nothing yet.
   */
  NotReady = 'not-ready',
}

/**
 * Represents the different possible environments for the application.
 *
 * This enum is used to specify and distinguish between various environments
 * where the application can be deployed or executed. It can be useful for
 * configuring environment-specific settings such as logging, API endpoints,
 * feature toggles, or other environment-dependent behaviors.
 *
 * Enum members:
 * - Local: Represents a local testing or development environment.
 * - Prod: Represents the production environment.
 * - Stage: Represents the staging environment, often used for pre-production testing.
 * - Test: Represents the testing environment, typically used for automated tests.
 */
export enum Environment {
  Local = 'local',
  Development = 'development',
  Staging = 'staging',
  Production = 'production',
  Test = 'test',
}

export enum LoadPriority {
  /**
   * Domain errors module, if connected, should register in top but always after logger.
   */
  Errors = -9_999,

  /**
   * Logger should be loaded as soon as possible to log the original error.
   */
  Logger = -10_000,
}

export const LogLevel = {
  Fatal: 'fatal',
  Error: 'error',
  Warn: 'warn',
  Info: 'info',
  Debug: 'debug',
  Trace: 'trace',
  Silent: 'silent',
} as const satisfies Record<string, LevelWithSilent>;

export type LogLevel = (typeof LogLevel)[keyof typeof LogLevel];
