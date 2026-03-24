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
}
