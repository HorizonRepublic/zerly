import { ConfigFormat } from '../enums/config-format.enum';

import { IConfigResolver } from './config-resolver.interface';

/**
 * Resolves configuration values from `process.env`.
 *
 * All values are returned as strings — type conversion is handled
 * downstream by `ConfigBuilder`.
 */
export class EnvResolver implements IConfigResolver {
  /** @inheritdoc */
  public readonly format = ConfigFormat.Dotenv;

  /**
   * Reads a value from `process.env`.
   * @param key - Environment variable name (e.g. `'APP_PORT'`).
   * @param _filePath - Ignored. Present for interface compatibility.
   * @returns The env var value as a string, or `undefined` if not set.
   */
  public get(key: string, _filePath?: string): string | undefined {
    return process.env[key];
  }

  /**
   * Checks whether an environment variable is defined.
   * @param key - Environment variable name.
   * @param _filePath - Ignored.
   * @returns `true` if the env var exists in `process.env`.
   */
  public has(key: string, _filePath?: string): boolean {
    return key in process.env;
  }
}
