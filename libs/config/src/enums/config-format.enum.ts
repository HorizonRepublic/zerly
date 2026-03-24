/**
 * Supported configuration source formats.
 * Set once at module level via `ConfigModule.forRoot({ format })`.
 */
export enum ConfigFormat {
  /** Environment variables via `process.env`. Default format. */
  Dotenv = 'dotenv',

  /** YAML file(s) parsed at startup. Supports structured data (arrays, objects). */
  Yaml = 'yaml',
}
