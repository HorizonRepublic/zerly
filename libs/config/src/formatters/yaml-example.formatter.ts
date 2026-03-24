import { stringify } from 'yaml';

import { IEnvFieldMetadata } from '../types';

import { IConfigSection, IExampleFormatter } from './example-formatter.interface';

/**
 * Formats configuration metadata into a `.env.example.yaml` file.
 *
 * Reconstructs YAML nesting from dot-path keys and outputs structured data
 * including arrays and nested objects.
 */
export class YamlExampleFormatter implements IExampleFormatter {
  private static readonly header = `###
#
# This file is auto-generated based on all registered configurations.
# Do not edit it manually.
#
###` as const;

  /** @inheritdoc */
  public readonly fileName = '.env.example.yaml';

  /**
   * Formats config sections into a complete `.env.example.yaml` string.
   * @param sections - Grouped config metadata.
   * @returns The formatted YAML content.
   */
  public format(sections: IConfigSection[]): string {
    const parts: string[] = [];

    for (const { title, fields, instance } of sections) {
      const tree = this.buildTree(fields, instance);

      if (Object.keys(tree).length === 0) continue;

      const yamlStr = stringify(tree, {
        lineWidth: 0,
        defaultStringType: 'QUOTE_DOUBLE',
        defaultKeyType: 'PLAIN',
      }).trimEnd();
      const commented = this.addFieldComments(yamlStr, fields);

      parts.push(`# -- ${title}\n${commented}`);
    }

    return `${YamlExampleFormatter.header}\n\n${parts.join('\n\n')}\n`;
  }

  /**
   * Builds a nested object tree from dot-path keys and their values.
   * @param fields - `@Env()` metadata fields.
   * @param instance - Config class instance for fallback values.
   * @returns A nested plain object representing the YAML structure.
   */
  private buildTree(
    fields: IEnvFieldMetadata[],
    instance: Record<string, unknown>,
  ): Record<string, unknown> {
    const tree: Record<string, unknown> = {};

    for (const { key, options, propertyKey } of fields) {
      const value = this.resolveValue(options, instance[propertyKey as string]);

      this.setByDotPath(tree, key, value);
    }

    return tree;
  }

  /**
   * Resolves the display value for a field.
   * Priority: `example` -> `default` -> instance fallback -> empty string / empty array.
   * @param opts - The field options.
   * @param fallback - The fallback value from the config instance.
   * @returns The resolved value.
   */
  private resolveValue(opts: IEnvFieldMetadata['options'], fallback: unknown): unknown {
    if (opts.example !== undefined) return opts.example;
    if (opts.default !== undefined) return opts.default;
    if (fallback != null && fallback !== '') return fallback;

    return opts.type === Array ? [] : '';
  }

  /**
   * Sets a value in a nested object using a dot-separated path.
   * Creates intermediate objects as needed.
   * @param obj - The root object to set the value in.
   * @param dotPath - The dot-separated key path.
   * @param value - The value to set.
   */
  private setByDotPath(obj: Record<string, unknown>, dotPath: string, value: unknown): void {
    const segments = dotPath.split('.');
    let current: Record<string, unknown> = obj;

    for (let i = 0; i < segments.length - 1; i++) {
      const seg = segments[i];

      if (seg === undefined) continue;

      if (!(seg in current) || typeof current[seg] !== 'object' || current[seg] === null) {
        current[seg] = {};
      }

      current = current[seg] as Record<string, unknown>;
    }

    const lastSegment = segments[segments.length - 1];

    if (lastSegment !== undefined) {
      current[lastSegment] = value;
    }
  }

  /**
   * Adds inline comments to YAML output for fields that have descriptions.
   * Matches the first occurrence of each leaf key in the YAML string.
   * @param yamlStr - The YAML string to annotate.
   * @param fields - The `@Env()` metadata fields.
   * @returns The YAML string with comments added.
   */
  private addFieldComments(yamlStr: string, fields: IEnvFieldMetadata[]): string {
    let result = yamlStr;

    for (const { key, options } of fields) {
      const comment = options.description ?? options.comment;

      if (!comment) continue;

      const leafKey = key.split('.').pop();

      if (!leafKey) continue;

      // Add comment after the first line containing this key
      result = result.replace(new RegExp(`^(\\s*${leafKey}:.*)$`, 'm'), `$1 # ${comment}`);
    }

    return result;
  }
}
