import { ENV_METADATA_KEY } from '../tokens';
import { EnumType, EnvTypeConstructor, IEnvFieldMetadata, IEnvOptions } from '../types';

/**
 * Property decorator for mapping environment variables to class properties.
 * Stores metadata about the environment variable configuration that will be
 * processed during class initialization.
 *
 * @param key The environment variable name (e.g., 'PORT', 'NODE_ENV').
 * @param options Configuration options including default value, type, etc.
 * @returns PropertyDecorator function.
 *
 * @example
 * ```typescript
 * class Config {
 *   @Env('PORT', { default: 3000, type: Number })
 *   port!: number;
 *
 *   @Env('NODE_ENV', { default: Environment.Development, type: Environment })
 *   env!: Environment;
 * }
 * ```
 */

export const Env =
  <TType extends EnumType | EnvTypeConstructor = typeof String>(
    key: string,
    options: IEnvOptions<TType> = {},
  ): PropertyDecorator =>
  (target: object, propertyKey: string | symbol) => {
    const existingMetadata: IEnvFieldMetadata[] =
      Reflect.getMetadata(ENV_METADATA_KEY, target) ?? [];

    existingMetadata.push({
      key,
      options: options as unknown as IEnvOptions<EnumType | EnvTypeConstructor>,
      propertyKey,
    });

    Reflect.defineMetadata(ENV_METADATA_KEY, existingMetadata, target);
  };
