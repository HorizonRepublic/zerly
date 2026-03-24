import { ConfigFormat } from '../enums/config-format.enum';

import { ConfigFactory } from './config-factory.type';
import { IConfigLoadItem } from './config-load-item.interface';

/**
 * Options for `ConfigModule.forRoot()`.
 * @example
 * ```typescript
 * ConfigModule.forRoot({
 *   format: ConfigFormat.Yaml,
 *   path: 'config/app.yaml',
 *   load: [appConfig, { path: 'config/db.yaml', config: dbConfig }],
 * })
 * ```
 */
export interface IConfigModuleOptions {
  /**
   * Config source format. Determines how `@Env()` keys are interpreted.
   * @default ConfigFormat.Dotenv
   */
  readonly format?: ConfigFormat;

  /**
   * Default YAML file path. Used by configs that don't specify their own path.
   * Only applicable when `format` is `ConfigFormat.Yaml`.
   */
  readonly path?: string;

  /** Config factories to load. Accepts bare `ConfigFactory` or `{ path, config }` objects. */
  readonly load?: (ConfigFactory | IConfigLoadItem)[];

  /**
   * Directory where the example config file (`.env.example` or `env.example.yaml`)
   * will be generated. Useful in monorepo setups to target a specific app directory
   * instead of the workspace root.
   *
   * If not specified, the module attempts to resolve the app root automatically
   * by inspecting `process.argv[1]` for known build output directories (`dist/`,
   * `build/`, `.next/`), falling back to `process.cwd()`.
   * @example
   * ```typescript
   * ConfigModule.forRoot({
   *   outputDir: 'apps/my-app',
   *   load: [appConfig],
   * })
   * ```
   */
  readonly outputDir?: string;
}
