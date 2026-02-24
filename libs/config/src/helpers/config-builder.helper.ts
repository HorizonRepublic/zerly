import 'reflect-metadata';
import { Logger, Type } from '@nestjs/common';
import { registerAs } from '@nestjs/config';
import { RuntimeException } from '@nestjs/core/errors/exceptions';

import { ENV_METADATA_KEY } from '../tokens';
import { EnumType, EnvTypeConstructor, IEnvFieldMetadata } from '../types';
import { ConfigFactory, ConfigFactoryKeyHost } from '../types/config-factory.type';

/**
 * Builder class for fluent configuration creation.
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
  private readonly logger = new Logger(ConfigBuilder.name);
  private validator?: (config: T) => T;

  private constructor(
    private readonly configClass: Type<T>,
    private readonly token: symbol,
  ) {}

  /**
   * Creates a configuration builder from a configuration class and token.
   *
   * @param configClass Configuration class constructor.
   * @param token Unique symbol token for dependency injection.
   * @returns ConfigBuilder instance for chaining.
   *
   * @example
   * ```TypeScript
   * ConfigBuilder.from(AppConfig, APP_CONFIG)
   * ConfigBuilder.from(DatabaseConfig, DATABASE_CONFIG)
   * ```
   */
  public static from<T extends object>(configClass: Type<T>, token: symbol): ConfigBuilder<T> {
    return new ConfigBuilder(configClass, token);
  }

  /**
   * Builds the final configuration factory.
   *
   * @returns NestJS ConfigFactory with proper typing.
   *
   * @example
   * ```TypeScript
   * .build()
   * ```
   */

  public build(): ConfigFactory & ConfigFactoryKeyHost<T> {
    const instance = this.initializeConfig(this.configClass);

    try {
      const finalConfig = this.validator ? this.validator(instance) : instance;

      return registerAs(this.token, () => Object.freeze(finalConfig));
    } catch (error) {
      this.logger.error(`Validation failed for configuration object:`);
      this.logger.error((error as Error).message);

      process.exit(1);
    }
  }

  /**
   * Adds validation to the configuration.
   *
   * @param validator Function that validates and potentially transforms the config.
   * @returns ConfigBuilder instance for chaining.
   *
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
   * Converts environment variable string to the appropriate type.
   *
   * @param value The string value from process.env.
   * @param type The constructor type or enum to convert to.
   * @returns The converted value as string, number, or boolean.
   * @throws {RuntimeException} - If conversion fails.
   * @example -
   */
  private convertValue(
    value: string,
    type?: EnumType | EnvTypeConstructor,
  ): boolean | number | string {
    if (!type || type === String) {
      return value;
    }

    if (type === Number) {
      const num = Number(value);

      if (isNaN(num)) throw new RuntimeException(`Cannot convert "${value}" to number`);

      return num;
    }

    if (type === Boolean) return value === 'true' || value === '1';

    return value;
  }

  /**
   * Internal method to initialize configuration from environment variables.
   *
   * @param configClass Configuration class constructor.
   * @returns Fully initialized configuration instance.
   * @throws {never} Calls process.exit(1) If required environment variables are missing or invalid.
   * @example -
   */
  private initializeConfig(configClass: new () => T): T {
    const instance = new configClass();
    const metadata: IEnvFieldMetadata[] = Reflect.getMetadata(ENV_METADATA_KEY, instance) ?? [];
    const errors: string[] = [];

    for (const { key, options, propertyKey } of metadata) {
      const value = process.env[key];
      const classValue = Reflect.get(instance, propertyKey) as unknown;

      if (value === undefined) {
        if (options.default !== undefined) {
          Reflect.set(instance, propertyKey, options.default);
          continue;
        }

        if (classValue !== undefined) continue;

        errors.push(`Missing required environment variable: ${key}`);
        continue;
      }

      try {
        const convertedValue = this.convertValue(value, options.type);

        Reflect.set(instance, propertyKey, convertedValue);
      } catch (error) {
        errors.push(`Invalid value for ${key}: ${(error as Error).message}`);
      }
    }

    if (errors.length > 0) {
      this.logger.error('Configuration initialization failed:');

      errors.forEach((error) => {
        this.logger.error(`- ${error}`);
      });

      process.exit(1);
    }

    return instance;
  }
}
