import 'reflect-metadata';
import { registerAs } from '@nestjs/config';

import { ConfigRegistry } from '../config.registry';
import { ConfigFormat } from '../enums/config-format.enum';
import { IConfigResolver } from '../resolvers/config-resolver.interface';
import { ENV_METADATA_KEY } from '../tokens';
import { EnumType, EnvTypeConstructor, IEnvFieldMetadata } from '../types';
import { ConfigFactory, ConfigFactoryKeyHost } from '../types/config-factory.type';

/** Constructor type equivalent to NestJS `Type<T>`. Avoids ESM-only `@nestjs/common` import. */
type Type<T = unknown> = new (...args: unknown[]) => T;

/** Symbol used to attach the config class reference to a factory function. */
export const CONFIG_CLASS_KEY = Symbol('config-class');

/**
 * Builder class for fluent configuration creation.
 *
 * Uses lazy evaluation: `build()` returns a factory closure that defers
 * value resolution until NestJS DI invokes it. This allows the
 * `ConfigRegistry` resolver to be set up before any values are read.
 *
 * @example
 * ```TypeScript
 * export const appConfig = ConfigBuilder
 *   .from(AppConfig, APP_CONFIG)
 *   .validate(typia.assertEquals<IAppConfig>)
 *   .build();
 * ```
 */
export class ConfigBuilder<T extends object> {
  private validator?: (config: T) => T;

  private constructor(
    private readonly configClass: Type<T>,
    private readonly token: string | symbol,
  ) {}

  /**
   * Creates a configuration builder from a configuration class and token.
   * @param configClass Configuration class constructor.
   * @param token Unique string or symbol token for dependency injection.
   * @returns ConfigBuilder instance for chaining.
   * @example
   * ```TypeScript
   * ConfigBuilder.from(AppConfig, APP_CONFIG)
   * ConfigBuilder.from(DatabaseConfig, DATABASE_CONFIG)
   * ```
   */
  public static from<T extends object>(
    configClass: Type<T>,
    token: string | symbol,
  ): ConfigBuilder<T> {
    return new ConfigBuilder(configClass, token);
  }

  /**
   * Builds the final configuration factory.
   *
   * Returns a `registerAs` factory whose closure defers all resolution
   * to invocation time (lazy). The resolver is obtained from
   * `ConfigRegistry` when the factory is called by NestJS DI.
   *
   * @returns NestJS ConfigFactory with proper typing.
   * @example
   * ```TypeScript
   * .build()
   * ```
   */
  public build(): ConfigFactory & ConfigFactoryKeyHost<T> {
    const factory = registerAs(this.token, () => {
      const resolver = ConfigRegistry.getResolver();
      const filePath = ConfigRegistry.getFilePath(this.token);
      const instance = this.initializeConfig(this.configClass, resolver, filePath);

      try {
        const validated = this.validator ? this.validator(instance) : instance;
        return Object.freeze(validated);
      } catch (error) {
        console.error('[ConfigBuilder] Validation failed for configuration object:');
        console.error((error as Error).message);
        process.exit(1);
      }
    });

    // Attach class reference for example generation (pre-DI metadata scan)
    (factory as unknown as Record<symbol, unknown>)[CONFIG_CLASS_KEY] = this.configClass;

    return factory;
  }

  /**
   * Adds validation to the configuration.
   * @param validator Function that validates and potentially transforms the config.
   * @returns ConfigBuilder instance for chaining.
   * @example
   * ```TypeScript
   * .validate(typia.assertEquals<IAppConfig>)
   * .validate(validateSync) // class-validator
   * .validate(config => schema.parse(config)) // zod
   * ```
   */
  public validate(validator: (config: T) => T): ConfigBuilder<T> {
    this.validator = validator;
    return this;
  }

  /**
   * Converts a dotenv string value to the appropriate runtime type.
   *
   * Only called for `ConfigFormat.Dotenv` sources where all raw values
   * are strings. YAML sources return already-typed values and skip this.
   *
   * @param value The string value from the dotenv resolver.
   * @param type The constructor type or enum to convert to.
   * @returns The converted value.
   * @throws {Error} If conversion fails (e.g. non-numeric string to Number).
   */
  private convertValue(
    value: string,
    type?: EnumType | EnvTypeConstructor,
  ): boolean | number | string | unknown[] {
    if (!type || type === String) {
      return value;
    }

    if (type === Number) {
      const num = Number(value);

      if (isNaN(num)) throw new Error(`Cannot convert "${value}" to number`);

      return num;
    }

    if (type === Boolean) return value === 'true' || value === '1';

    if (type === Array) {
      try {
        const parsed: unknown = JSON.parse(value);
        if (!globalThis.Array.isArray(parsed)) {
          throw new Error(`Expected JSON array, got ${typeof parsed}`);
        }
        return parsed as unknown[];
      } catch (error) {
        throw new Error(
          `Cannot parse array from env var: ${error instanceof Error ? error.message : String(error)}`,
        );
      }
    }

    return value;
  }

  /**
   * Initializes a configuration instance by resolving each decorated field
   * through the provided resolver.
   *
   * @param configClass Configuration class constructor.
   * @param resolver The active config resolver (env or yaml).
   * @param filePath Optional file path override for YAML resolver.
   * @returns Fully initialized configuration instance.
   * @throws {never} Calls `process.exit(1)` if required keys are missing or invalid.
   */
  private initializeConfig(
    configClass: new () => T,
    resolver: IConfigResolver,
    filePath?: string,
  ): T {
    const instance = new configClass();
    const metadata: IEnvFieldMetadata[] = Reflect.getMetadata(ENV_METADATA_KEY, instance) ?? [];
    const errors: string[] = [];

    for (const { key, options, propertyKey } of metadata) {
      const rawValue = resolver.get(key, filePath);
      const classValue = Reflect.get(instance, propertyKey) as unknown;

      if (rawValue === undefined) {
        if (options.default !== undefined) {
          Reflect.set(instance, propertyKey, options.default);
          continue;
        }

        if (classValue !== undefined) continue;

        errors.push(`Missing required configuration key: ${key}`);
        continue;
      }

      try {
        // In dotenv mode, raw values are strings that need type conversion.
        // In yaml mode, values are already typed — pass through as-is.
        const value =
          resolver.format === ConfigFormat.Dotenv && typeof rawValue === 'string'
            ? this.convertValue(rawValue, options.type)
            : rawValue;

        Reflect.set(instance, propertyKey, value);
      } catch (error) {
        errors.push(`Invalid value for ${key}: ${(error as Error).message}`);
      }
    }

    if (errors.length > 0) {
      console.error('[ConfigBuilder] Configuration initialization failed:');

      errors.forEach((error) => {
        console.error(`  - ${error}`);
      });

      process.exit(1);
    }

    return instance;
  }
}
