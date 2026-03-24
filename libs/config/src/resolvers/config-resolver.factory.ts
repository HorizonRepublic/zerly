import { ConfigFormat } from '../enums/config-format.enum';

import { IConfigResolver } from './config-resolver.interface';
import { EnvResolver } from './env.resolver';
import { YamlResolver } from './yaml.resolver';

/**
 * Factory for creating the appropriate `IConfigResolver` based on format.
 */
export class ConfigResolverFactory {
  /**
   * Creates a config resolver instance.
   * @param format - The configuration format. Defaults to `ConfigFormat.Dotenv`.
   * @param defaultPath - Default YAML file path. Required when `format` is `Yaml`.
   * @returns A resolver instance matching the requested format.
   */
  public static create(
    format: ConfigFormat = ConfigFormat.Dotenv,
    defaultPath?: string,
  ): IConfigResolver {
    switch (format) {
      case ConfigFormat.Yaml: {
        return new YamlResolver(defaultPath ?? 'env.yaml');
      }

      case ConfigFormat.Dotenv:
      default: {
        return new EnvResolver();
      }
    }
  }
}
