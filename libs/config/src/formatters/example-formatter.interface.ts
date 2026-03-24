import { IEnvFieldMetadata } from '../types';

/**
 * Represents a section of configuration fields grouped by config token.
 */
export interface IConfigSection {
  /** Human-readable title for the section (e.g. config token description). */
  readonly title: string;

  /** The `@Env()` metadata fields for this config class. */
  readonly fields: IEnvFieldMetadata[];

  /** An instance of the config class for fallback value resolution. */
  readonly instance: Record<string, unknown>;
}

/**
 * Formats configuration metadata into an example file.
 * Implementations produce format-specific output (`.env.example`, `env.example.yaml`).
 */
export interface IExampleFormatter {
  /**
   * Formats config sections into a complete example file string.
   * @param sections - Grouped config metadata.
   * @returns The formatted file content.
   */
  format(sections: IConfigSection[]): string;

  /** The output file name (e.g. `.env.example`, `env.example.yaml`). */
  readonly fileName: string;
}
