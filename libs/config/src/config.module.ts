import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, isAbsolute, join, resolve } from 'node:path';

import { DynamicModule, Logger, Module } from '@nestjs/common';
import { ConfigFactory, ConfigModule as BaseConfigModule } from '@nestjs/config';

import { ConfigRegistry } from './config.registry';
import { ConfigFormat } from './enums/config-format.enum';
import { EnvExampleFormatter } from './formatters/env-example.formatter';
import { IConfigSection, IExampleFormatter } from './formatters/example-formatter.interface';
import { YamlExampleFormatter } from './formatters/yaml-example.formatter';
import { CONFIG_CLASS_KEY } from './helpers/config-builder.helper';
import { resolveAppRoot } from './helpers/resolve-app-root.helper';
import { EnvExampleProvider } from './providers/env-example.provider';
import { ConfigResolverFactory } from './resolvers/config-resolver.factory';
import { ENV_METADATA_KEY } from './tokens';
import { IEnvFieldMetadata } from './types';
import { IConfigFeatureOptions } from './types/config-feature-options.interface';
import { IConfigLoadItem } from './types/config-load-item.interface';
import { IConfigModuleOptions } from './types/config-module-options.interface';

/**
 * NestJS dynamic module for typed configuration management.
 *
 * Supports two formats:
 * - `dotenv` (default) — reads from `process.env`
 * - `yaml` — reads from YAML files with dot-path notation
 *
 * The format is selected once at module level — no mixing allowed.
 * @example
 * ```typescript
 * // Dotenv mode (default, backward compatible)
 * ConfigModule.forRoot({ load: [appConfig] })
 *
 * // YAML mode
 * ConfigModule.forRoot({
 *   format: ConfigFormat.Yaml,
 *   path: 'config/app.yaml',
 *   load: [appConfig, { path: 'config/db.yaml', config: dbConfig }],
 * })
 * ```
 */
@Module({})
export class ConfigModule {
  private static readonly logger = new Logger(ConfigModule.name);

  /**
   * Registers configuration globally with the specified format and sources.
   *
   * This method:
   * 1. Creates and registers the appropriate resolver in `ConfigRegistry`
   * 2. Processes load items — extracts factories and registers file path overrides
   * 3. Generates an example config file synchronously (before DI resolution)
   * 4. Delegates to `@nestjs/config` for DI integration
   * @param options - Module options including format, default path, and config factories.
   * @returns A global dynamic module.
   */
  public static forRoot(options: IConfigModuleOptions = {}): DynamicModule {
    const { format = ConfigFormat.Dotenv, path, load = [], outputDir } = options;

    // 1. Create and register resolver — resolve relative paths against app root
    const appRoot = resolveAppRoot();
    const resolvedPath = this.resolveConfigPath(path, format, appRoot);
    const resolver = ConfigResolverFactory.create(format, resolvedPath);

    ConfigRegistry.setResolver(resolver);

    // 2. Process load items — extract factories and register file paths
    const factories: ConfigFactory[] = [];

    for (const item of load) {
      if (this.isConfigLoadItem(item)) {
        const itemPath = isAbsolute(item.path) ? item.path : join(appRoot, item.path);

        ConfigRegistry.setFilePath(this.extractToken(item.config), itemPath);
        factories.push(item.config);
      } else {
        factories.push(item);
      }
    }

    // 3. Generate example file (pre-DI, sync) — use appRoot as default output dir
    this.generateExample(format, load, outputDir ? resolve(process.cwd(), outputDir) : appRoot);

    // 4. Delegate to @nestjs/config
    return {
      module: ConfigModule,
      global: true,
      imports: [
        BaseConfigModule.forRoot({
          cache: true,
          isGlobal: true,
          expandVariables: format === ConfigFormat.Dotenv,
          load: factories,
        }),
      ],
      providers: format === ConfigFormat.Dotenv ? [EnvExampleProvider] : [],
      exports: [BaseConfigModule],
    };
  }

  /**
   * Registers a feature-specific configuration.
   *
   * Accepts either a bare `ConfigFactory` (backward compatible) or
   * an options object with an optional YAML file path override.
   * @param configOrOptions - A bare `ConfigFactory` or options with path override.
   * @returns A dynamic module for the feature.
   */
  public static forFeature(configOrOptions: ConfigFactory | IConfigFeatureOptions): DynamicModule {
    let config: ConfigFactory;

    if (typeof configOrOptions === 'function') {
      config = configOrOptions;
    } else {
      config = configOrOptions.config;

      if (configOrOptions.path) {
        ConfigRegistry.setFilePath(this.extractToken(config), configOrOptions.path);
      }
    }

    return {
      module: ConfigModule,
      imports: [BaseConfigModule.forFeature(config)],
      exports: [BaseConfigModule],
    };
  }

  /**
   * Resolves the YAML config file path to an absolute path.
   * Uses `env.yaml` in the app root as default when format is YAML and no path given.
   */
  private static resolveConfigPath(
    path: string | undefined,
    format: ConfigFormat,
    appRoot: string,
  ): string | undefined {
    if (path) {
      return isAbsolute(path) ? path : join(appRoot, path);
    }

    return format === ConfigFormat.Yaml ? join(appRoot, 'env.yaml') : undefined;
  }

  /**
   * Type guard for `IConfigLoadItem` vs bare `ConfigFactory`.
   */
  private static isConfigLoadItem(item: ConfigFactory | IConfigLoadItem): item is IConfigLoadItem {
    return typeof item === 'object' && 'config' in item && 'path' in item;
  }

  /**
   * Extracts the config token from a `ConfigFactory`.
   * `registerAs` attaches the token as `KEY` property on the factory function.
   */
  // eslint-disable-next-line sonarjs/function-return-type
  private static extractToken(factory: ConfigFactory): string | symbol {
    // eslint-disable-next-line @typescript-eslint/naming-convention
    return (factory as unknown as { KEY: string | symbol }).KEY;
  }

  /**
   * Generates an example configuration file synchronously before DI resolution.
   * Errors are caught and logged — they never block app startup.
   * @param format - The config format (determines formatter and file name).
   * @param load - The load items to extract metadata from.
   * @param outputDir - Absolute output directory path.
   */
  private static generateExample(
    format: ConfigFormat,
    load: (ConfigFactory | IConfigLoadItem)[],
    outputDir: string,
  ): void {
    try {
      const formatter: IExampleFormatter =
        format === ConfigFormat.Yaml ? new YamlExampleFormatter() : new EnvExampleFormatter();

      const sections = this.collectSections(load);

      if (sections.length === 0) return;

      const content = formatter.format(sections);
      const outputPath = join(outputDir, formatter.fileName);

      // Write only if content changed (SHA-256 comparison)
      if (existsSync(outputPath)) {
        const existing = readFileSync(outputPath, 'utf8');
        const existingHash = createHash('sha256').update(existing).digest('hex');
        const newHash = createHash('sha256').update(content).digest('hex');

        if (existingHash === newHash) return;
      }

      const dir = dirname(outputPath);

      if (!existsSync(dir)) {
        mkdirSync(dir, { recursive: true });
      }

      writeFileSync(outputPath, content, 'utf8');
      this.logger.log(`Example config generated: ${outputPath}`);
    } catch (error) {
      this.logger.warn(
        `Failed to generate example config: ${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }

  /**
   * Collects config sections by scanning `@Env()` metadata from config classes.
   * Uses the `CONFIG_CLASS_KEY` symbol attached by `ConfigBuilder.build()`.
   */
  private static collectSections(load: (ConfigFactory | IConfigLoadItem)[]): IConfigSection[] {
    const sections: IConfigSection[] = [];

    for (const item of load) {
      const factory = this.isConfigLoadItem(item) ? item.config : item;
      const configClass = (factory as unknown as Record<symbol, unknown>)[CONFIG_CLASS_KEY] as
        | (new () => unknown)
        | undefined;

      if (!configClass) {
        this.logger.debug(
          'Config class not found on factory — skipping example generation for this config.',
        );
        continue;
      }

      const instance = new configClass() as Record<string, unknown>;
      const fields: IEnvFieldMetadata[] = Reflect.getMetadata(ENV_METADATA_KEY, instance) ?? [];

      if (fields.length === 0) continue;

      const token = this.extractToken(factory);
      const title = typeof token === 'symbol' ? (token.description ?? 'unknown') : token;

      sections.push({ title, fields, instance });
    }

    return sections;
  }
}
