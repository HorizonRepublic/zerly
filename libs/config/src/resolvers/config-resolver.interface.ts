import { ConfigFormat } from '../enums/config-format.enum';

/**
 * Abstraction for resolving configuration values from different sources.
 *
 * Implementations read from a specific source (environment variables, YAML files, etc.)
 * and return values by key. The key format depends on the source:
 * - `EnvResolver`: flat env var names (e.g. `'APP_PORT'`)
 * - `YamlResolver`: dot-separated paths (e.g. `'database.port'`)
 */
export interface IConfigResolver {
  /**
   * Retrieves a configuration value by key.
   * @param key - The configuration key to resolve.
   * @param filePath - Optional file path override. Used by YAML resolver to
   *   select a specific file instead of the default. Ignored by EnvResolver.
   * @returns The resolved value, or `undefined` if the key does not exist.
   */
  get(key: string, filePath?: string): unknown;

  /**
   * Checks whether a configuration key exists in the source.
   * @param key - The configuration key to check.
   * @param filePath - Optional file path override (YAML only).
   * @returns `true` if the key exists, `false` otherwise.
   */
  has(key: string, filePath?: string): boolean;

  /** The configuration format this resolver handles. */
  readonly format: ConfigFormat;
}
