import { existsSync, readFileSync } from 'node:fs';

import { parse } from 'yaml';

import { ConfigFormat } from '../enums/config-format.enum';

import { IConfigResolver } from './config-resolver.interface';

/**
 * Resolves configuration values from YAML files using dot-path notation.
 *
 * Parsed YAML content is cached per file path. Files are read synchronously
 * at first access — acceptable for startup-only configuration loading.
 * @example
 * ```typescript
 * const resolver = new YamlResolver('config/app.yaml');
 * resolver.get('database.port');       // → 5432 (number from YAML)
 * resolver.get('database.replicas');   // → [{ host: '...', port: 5432 }, ...]
 * ```
 */
export class YamlResolver implements IConfigResolver {
  /** @inheritdoc */
  public readonly format = ConfigFormat.Yaml;

  /** Cache of parsed YAML content, keyed by absolute file path. */
  private readonly cache = new Map<string, Record<string, unknown>>();

  /**
   * @param defaultPath - Default YAML file path. Used when `get()`/`has()`
   *   are called without a `filePath` override.
   */
  public constructor(private readonly defaultPath: string) {}

  /**
   * Retrieves a value from parsed YAML using dot-path notation.
   * @param key - Dot-separated path (e.g. `'database.replicas'`).
   * @param filePath - Optional file path override. Falls back to `defaultPath`.
   * @returns The resolved value (scalar, array, or object), or `undefined` if not found.
   * @throws {Error} If the YAML file does not exist or contains invalid syntax.
   */
  public get(key: string, filePath?: string): unknown {
    const parsed = this.loadFile(filePath ?? this.defaultPath);

    if (!parsed) return undefined;

    return this.resolveByDotPath(parsed, key);
  }

  /**
   * Checks whether a dot-path key exists in the YAML file.
   * @param key - Dot-separated path.
   * @param filePath - Optional file path override.
   * @returns `true` if the key resolves to a defined value.
   */
  public has(key: string, filePath?: string): boolean {
    return this.get(key, filePath) !== undefined;
  }

  /**
   * Loads and parses a YAML file, caching the result.
   * @param path - Absolute or relative file path.
   * @returns Parsed YAML as a plain object, or `undefined` for empty files.
   * @throws {Error} If the file does not exist or contains invalid YAML.
   */
  private loadFile(path: string): Record<string, unknown> | undefined {
    if (this.cache.has(path)) {
      return this.cache.get(path);
    }

    if (!existsSync(path)) {
      throw new Error(`YAML config file not found: ${path}`);
    }

    const content = readFileSync(path, 'utf8');

    if (!content.trim()) {
      console.warn(`[YamlResolver] YAML config file is empty: ${path}`);
      this.cache.set(path, {});
      return undefined;
    }

    try {
      const parsed = parse(content) as Record<string, unknown>;

      this.cache.set(path, parsed);

      return parsed;
    } catch (error) {
      throw new Error(
        `Failed to parse YAML file ${path}: ${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }

  /**
   * Traverses a nested object using a dot-separated path.
   * @param obj - The root object to traverse.
   * @param dotPath - Dot-separated key (e.g. `'database.replicas'`).
   * @returns The value at the path, or `undefined` if any segment is missing.
   */
  private resolveByDotPath(obj: Record<string, unknown>, dotPath: string): unknown {
    const segments = dotPath.split('.');
    let current: unknown = obj;

    for (const segment of segments) {
      if (current === null || current === undefined || typeof current !== 'object') {
        return undefined;
      }

      current = (current as Record<string, unknown>)[segment];
    }

    return current;
  }
}
