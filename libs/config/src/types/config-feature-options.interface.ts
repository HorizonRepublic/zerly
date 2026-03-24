import { ConfigFactory } from './config-factory.type';

/**
 * Options for `ConfigModule.forFeature()` with an optional YAML file path.
 * @example
 * ```typescript
 * ConfigModule.forFeature({
 *   path: 'config/notifications.yaml',
 *   config: notificationsConfig,
 * })
 * ```
 */
export interface IConfigFeatureOptions {
  /** Path to the YAML file for this feature config. */
  readonly path?: string;

  /** The config factory returned by `ConfigBuilder.build()`. */
  readonly config: ConfigFactory;
}
