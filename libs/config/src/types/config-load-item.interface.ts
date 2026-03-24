import { ConfigFactory } from './config-factory.type';

/**
 * A config factory with a specific YAML file path override.
 * Used in `ConfigModule.forRoot({ load })` when a config needs its own file.
 * @example
 * ```typescript
 * { path: 'config/database.yaml', config: dbConfig }
 * ```
 */
export interface IConfigLoadItem {
  /** Absolute or relative path to the YAML file for this config. */
  readonly path: string;

  /** The config factory returned by `ConfigBuilder.build()`. */
  readonly config: ConfigFactory;
}
