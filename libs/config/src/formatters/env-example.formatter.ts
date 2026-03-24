import { EnumType, EnvTypeConstructor, IEnvFieldMetadata } from '../types';
import { IConfigSection, IExampleFormatter } from './example-formatter.interface';

/**
 * Formats configuration metadata into a `.env.example` file.
 *
 * Output format:
 * ```
 * # -- section-title
 * KEY="value"  # REQUIRED. Description. Possible values: a, b. (Default: x)
 * ```
 */
export class EnvExampleFormatter implements IExampleFormatter {
  /** @inheritdoc */
  public readonly fileName = '.env.example';

  private static readonly header = `###
#
# This file is auto-generated based on all registered configurations.
# Do not edit it manually.
#
###` as const;

  /**
   * Formats config sections into a complete `.env.example` string.
   * @param sections - Grouped config metadata.
   * @returns The formatted file content.
   */
  public format(sections: IConfigSection[]): string {
    const formatted = sections
      .map(({ title, fields, instance }) => {
        const lines = this.formatFields(fields, instance);

        return lines.length > 0 ? `# -- ${title}\n${lines.join('\n')}` : undefined;
      })
      .filter((s): s is string => Boolean(s));

    return `${EnvExampleFormatter.header}\n\n${formatted.join('\n\n')}\n`;
  }

  /**
   * Formats individual fields into `.env.example` lines with aligned comments.
   * @param fields - The `@Env()` metadata fields.
   * @param instance - Config class instance for fallback value resolution.
   * @returns Array of formatted lines.
   */
  private formatFields(
    fields: IEnvFieldMetadata[],
    instance: Record<string, unknown>,
  ): string[] {
    const lines = fields.map(({ key, options, propertyKey }) => {
      const fallback = instance[propertyKey as string];
      const isRequired =
        options.default === undefined &&
        options.example === undefined &&
        (fallback == null || fallback === '');
      const decl = `${key}="${this.resolveValue(options, fallback)}"`;
      const parts = [
        isRequired ? 'REQUIRED' : undefined,
        options.description,
        options.comment,
        this.enumComment(options.type),
        this.defaultComment(options),
      ].filter(Boolean);

      return { decl, comment: parts.length > 0 ? parts.join('. ') : undefined };
    });

    const maxLen = lines.reduce((m, l) => Math.max(m, l.decl.length), 0);

    return lines.map(({ decl, comment }) =>
      comment ? `${decl.padEnd(maxLen + 1)}# ${comment}` : decl,
    );
  }

  /**
   * Resolves the display value for a field.
   * Priority: `example` -> `default` -> instance fallback -> empty string.
   * @param opts - The field options.
   * @param fallback - The fallback value from the config instance.
   * @returns The resolved string value.
   */
  private resolveValue(opts: IEnvFieldMetadata['options'], fallback: unknown): string {
    if (opts.example !== undefined) return String(opts.example);
    if (opts.default !== undefined) return String(opts.default);

    return fallback != null && fallback !== '' ? String(fallback) : '';
  }

  /**
   * Produces a default comment when both `example` and `default` are set.
   * @param opts - The field options.
   * @returns The default comment string, or undefined.
   */
  private defaultComment(opts: IEnvFieldMetadata['options']): string | undefined {
    if (opts.example !== undefined && opts.default !== undefined) {
      return `(Default: ${opts.default})`;
    }

    return undefined;
  }

  /**
   * Produces a comment listing possible enum values.
   * @param type - The type constructor or enum.
   * @returns The enum comment string, or undefined.
   */
  private enumComment(type?: EnumType | EnvTypeConstructor): string | undefined {
    if (!type || typeof type !== 'object') return undefined;

    const unique = [
      ...new Set(Object.values(type).filter((v) => typeof v === 'string' || typeof v === 'number')),
    ];

    return unique.length ? `Possible values: ${unique.join(', ')}` : undefined;
  }
}
