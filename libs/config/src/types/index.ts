export * from './app-config.interface';

/**
 * Maps TypeScript constructor types to their corresponding primitive types.
 */
export type ConstructorToType<T> = T extends typeof String
  ? string
  : T extends typeof Number
    ? number
    : T extends typeof Boolean
      ? boolean
      : never;

/**
 * Represents an enum-like object with string or number keys and values.
 */
export type EnumType = Record<number | string, number | string>;

/**
 * Supported primitive constructor types for environment variable conversion.
 */
export type EnvTypeConstructor = typeof Boolean | typeof Number | typeof String;

/**
 * Metadata stored for each environment variable field decorated with Env decorator.
 */
export interface IEnvFieldMetadata {
  /** Environment variable key name. */
  key: string;

  /** Configuration options for the environment variable. */
  options:
    | IEnvOptions<EnumType>
    | IEnvOptions<EnumType | EnvTypeConstructor>
    | IEnvOptions<EnvTypeConstructor>;

  /** Property key on the target class. */
  propertyKey: string | symbol;
}

/**
 * Configuration options for environment variables.
 *
 * @template TType The type constructor or enum type for the environment variable.
 */
export interface IEnvOptions<TType extends EnumType | EnvTypeConstructor = typeof String> {
  /** Default value to use if the environment variable is not set. */
  default?: InferTypeFromConstructor<TType>;

  /** Human-readable description of the environment variable. */
  description?: string;

  /** Example value for documentation purposes. */
  example?: InferTypeFromConstructor<TType>;

  /** Type constructor or enum for value conversion. */
  type?: TType;

  /** Optional comment to be appended to the generated .env.example line. */
  comment?: string;
}

/**
 * Infers the actual type from a constructor type or enum.
 *
 * @template TType The constructor type or enum to infer from.
 */
export type InferTypeFromConstructor<TType extends EnumType | EnvTypeConstructor> =
  TType extends EnumType
    ? TType[keyof TType]
    : TType extends EnvTypeConstructor
      ? ConstructorToType<TType>
      : never;
