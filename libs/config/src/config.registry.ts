import { IConfigResolver } from './resolvers/config-resolver.interface';

/**
 * Static registry that bridges `ConfigModule.forRoot()` (which creates the resolver)
 * and `ConfigBuilder.build()` factory closures (which consume it).
 *
 * Lifecycle:
 * 1. `ConfigModule.forRoot()` calls `setResolver()` and optionally `setFilePath()`.
 * 2. NestJS DI resolves providers, invoking `registerAs` factories.
 * 3. Each factory calls `getResolver()` — guaranteed to exist at this point.
 *
 * This static singleton is acceptable because configuration is inherently
 * application-global state with a well-defined initialization order.
 */
export class ConfigRegistry {
  private static resolver: IConfigResolver | undefined;
  private static readonly filePaths = new Map<string | symbol, string>();

  /**
   * Registers the active config resolver.
   * Called once by `ConfigModule.forRoot()`.
   * @param resolver - The resolver instance to use for all config resolution.
   */
  public static setResolver(resolver: IConfigResolver): void {
    this.resolver = resolver;
  }

  /**
   * Returns the registered config resolver.
   * @returns The active resolver instance.
   * @throws {Error} If called before `setResolver()` — indicates `ConfigModule.forRoot()`
   *   was not imported or has not executed yet.
   */
  public static getResolver(): IConfigResolver {
    if (!this.resolver) {
      throw new Error(
        'ConfigRegistry: resolver not initialized. Ensure ConfigModule.forRoot() is imported before any config is resolved.',
      );
    }

    return this.resolver;
  }

  /**
   * Registers a YAML file path override for a specific config token.
   * @param token - The config token (string or symbol) from `ConfigBuilder`.
   * @param path - The YAML file path for this config.
   */
  public static setFilePath(token: string | symbol, path: string): void {
    this.filePaths.set(token, path);
  }

  /**
   * Retrieves the YAML file path override for a config token.
   * @param token - The config token to look up.
   * @returns The file path, or `undefined` if no override was registered.
   */
  public static getFilePath(token: string | symbol): string | undefined {
    return this.filePaths.get(token);
  }

  /**
   * Clears all registered state. Intended for testing only.
   * @internal
   */
  public static reset(): void {
    this.resolver = undefined;
    this.filePaths.clear();
  }
}
