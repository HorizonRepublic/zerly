import { createHash } from 'crypto';
import { existsSync } from 'fs';
import * as fs from 'fs/promises';
import { dirname, join, normalize, resolve, sep } from 'path';

import { Injectable, Logger, OnModuleInit } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';

import { catchError, defer, EMPTY, from, map, Observable, of, switchMap, tap } from 'rxjs';

import { APP_CONFIG, ENV_METADATA_KEY } from '../tokens';
import { EnumType, EnvTypeConstructor, IAppConfig, IEnvFieldMetadata } from '../types';

/**
 * Automatically generates a `.env.example` file on module init by scanning all
 * registered configurations for `@Env()` decorated fields.
 *
 * Generation is skipped unless `AppConfig.generateEnvExample` is `true`.
 * The file is written only when its content has changed (SHA-256 comparison),
 * so repeated restarts in development do not cause unnecessary disk writes.
 *
 * @example
 * ```
 * # -- app
 * APP_PORT="3000"
 * APP_SECRET=""        # REQUIRED. JWT signing secret.
 * APP_ENV="production" # App environment. Possible values: development, production
 * ```
 */
@Injectable()
export class EnvExampleProvider implements OnModuleInit {
  private static readonly header = `###
#
# This file is auto-generated based on all registered configurations.
# Do not edit it manually.
#
###` as const;

  private readonly logger = new Logger(EnvExampleProvider.name);

  public constructor(private readonly configService: ConfigService) {}

  /** Triggers `.env.example` generation. Errors are caught and logged as warnings. */
  public onModuleInit(): void {
    this.generateEnvironmentExample()
      .pipe(
        catchError((error) => {
          this.logger.warn(
            `Failed to generate environment example file: ${error instanceof Error ? error.message : String(error)}`,
          );
          return EMPTY;
        }),
      )
      .subscribe();
  }

  /**
   * Core generation pipeline.
   *
   * Reads all config instances from `ConfigService.internalConfig` (both symbol-keyed
   * and string-keyed), extracts `@Env()` metadata from each, and writes the result
   * to `<appRoot>/.env.example`.
   */
  private generateEnvironmentExample(): Observable<void> {
    return defer(() => {
      let appConfig: IAppConfig | undefined;

      try {
        appConfig = this.configService.get<IAppConfig>(APP_CONFIG);
      } catch {
        this.logger.warn('Error retrieving AppConfig during env generation.');
        return EMPTY;
      }

      if (!appConfig?.generateEnvExample) {
        if (!appConfig) {
          this.logger.debug('AppConfig not found, skipping .env.example generation.');
        }

        return EMPTY;
      }

      const configs = // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (this.configService as any)['internalConfig'] as Record<string | symbol, any> | undefined;

      if (!configs) return EMPTY;

      const symbolKeys = Object.getOwnPropertySymbols(configs).map((k) => ({
        key: k as string | symbol,
        title: k.description,
      }));
      const stringKeys = Object.keys(configs).map((k) => ({ key: k as string | symbol, title: k }));
      const allKeys = [...symbolKeys, ...stringKeys];

      const configSections = allKeys
        .map(({ key, title }) => {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          const instance = (configs as any)[key];

          if (!instance || !title) return undefined;

          const fields: IEnvFieldMetadata[] = Reflect.getMetadata(ENV_METADATA_KEY, instance) ?? [];

          if (!fields.length) return undefined;

          return `# -- ${title}\n${this.formatEnvVariables(fields, instance).join('\n')}`;
        })
        .filter((s): s is string => Boolean(s));

      if (configSections.length === 0) {
        this.logger.debug('No configurations with @Env decorators found.');
        return EMPTY;
      }

      const templateContent = `${EnvExampleProvider.header}\n\n${configSections.join('\n\n')}\n`;
      const outputPath = join(this.resolveAppRoot(), '.env.example');

      return this.writeIfChanged$(outputPath, templateContent).pipe(
        tap(() => {
          this.logger.log(`Environment example generated: ${outputPath}`);
        }),
      );
    });
  }

  /**
   * Formats a list of `@Env()` fields into aligned `KEY="value"  # comment` lines.
   *
   * Declaration widths are padded to the longest entry so inline comments line up.
   * Comment parts order: `REQUIRED` → `description` → `comment` → enum values → default.
   *
   * @param fields - `@Env()` metadata collected from the config class.
   * @param instance - Frozen config instance used as a value fallback.
   * @returns Array of formatted env lines, one per field.
   * @example -
   */
  private formatEnvVariables(
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
   * Resolves the display value for a field in priority order:
   * `example` → `default` → runtime fallback from the config instance.
   *
   * @param opts - Field options from the `@Env()` decorator.
   * @param fallback - Current value on the config instance.
   * @returns String representation of the resolved value, or `""` if none.
   * @example -
   */
  private resolveValue(opts: IEnvFieldMetadata['options'], fallback: unknown): string {
    if (opts.example !== undefined) return String(opts.example);
    if (opts.default !== undefined) return String(opts.default);

    return fallback != null && fallback !== '' ? String(fallback) : '';
  }

  /**
   * Returns a `(Default: X)` annotation only when both `example` and `default` are
   * defined — i.e. the displayed value differs from the actual default.
   *
   * @param opts - Field options from the `@Env()` decorator.
   * @returns Comment fragment or `undefined`.
   * @example -
   */
  private defaultComment(opts: IEnvFieldMetadata['options']): string | undefined {
    if (opts.example !== undefined && opts.default !== undefined) {
      return `(Default: ${opts.default})`;
    }

    return undefined;
  }

  /**
   * Builds a `Possible values: a, b, c` comment from an enum type.
   * Numeric reverse-mappings (TypeScript `const enum` artifacts) are deduplicated.
   *
   * @param type - Enum object or primitive constructor passed to `@Env({ type })`.
   * @returns Comment fragment or `undefined` if `type` is not an enum.
   * @example -
   */
  private enumComment(type?: EnumType | EnvTypeConstructor): string | undefined {
    if (!type || typeof type !== 'object') return undefined;

    const unique = [
      ...new Set(Object.values(type).filter((v) => typeof v === 'string' || typeof v === 'number')),
    ];

    return unique.length ? `Possible values: ${unique.join(', ')}` : undefined;
  }

  /**
   * Resolves the project root directory by inspecting `process.argv[1]`.
   *
   * Strips known build output directories (`dist/`, `build/`, `.next/`) to find
   * the source root. Falls back to {@link fallbackRoot} if heuristics fail.
   *
   * @returns Absolute path to the application root.
   * @example -
   */
  private resolveAppRoot(): string {
    const entryPath = process.argv[1];

    if (!entryPath) return process.cwd();

    const normalizedPath = normalize(entryPath);
    const buildDirs = [`${sep}dist${sep}`, `${sep}build${sep}`, `${sep}.next${sep}`];

    for (const buildDir of buildDirs) {
      if (normalizedPath.includes(buildDir)) {
        const potentialSourcePath = normalizedPath.replace(buildDir, sep);
        const potentialRoot = resolve(dirname(potentialSourcePath));
        const appRoot = resolve(potentialRoot, '..');

        if (this.isValidProjectDir(appRoot)) return appRoot;
        if (this.isValidProjectDir(potentialRoot)) return potentialRoot;
      }
    }

    return this.fallbackRoot();
  }

  /**
   * Secondary root resolution strategy: uses `AppConfig.name` to locate the app
   * directory inside an Nx monorepo (`apps/<name>`) or a standalone project root.
   *
   * @returns Absolute path to the application root, or `process.cwd()` as a last resort.
   * @example -
   */
  private fallbackRoot(): string {
    try {
      const appConfig = this.configService.get<IAppConfig>(APP_CONFIG);

      if (appConfig?.name) {
        const mono = join(process.cwd(), 'apps', appConfig.name);

        if (this.isValidProjectDir(mono)) return mono;

        const root = join(process.cwd(), appConfig.name);

        if (this.isValidProjectDir(root)) return root;
      }
    } catch {
      // ignore
    }

    return process.cwd();
  }

  /**
   * Returns `true` if `dirPath` exists and contains at least one of
   * `package.json`, `project.json`, or `tsconfig.json`.
   *
   * @param dirPath - Absolute path to check.
   * @example -
   */
  private isValidProjectDir(dirPath: string): boolean {
    return (
      existsSync(dirPath) &&
      (existsSync(join(dirPath, 'package.json')) ||
        existsSync(join(dirPath, 'project.json')) ||
        existsSync(join(dirPath, 'tsconfig.json')))
    );
  }

  /**
   * Writes `content` to `filePath` only when the SHA-256 hash differs from the
   * existing file, avoiding unnecessary writes on repeated restarts.
   *
   * Parent directories are created recursively if they do not exist.
   *
   * @param filePath - Destination file path.
   * @param content - New file content.
   * @returns Observable that completes after a successful write, or `EMPTY` when unchanged.
   * @example -
   */
  private writeIfChanged$(filePath: string, content: string): Observable<void> {
    const newHash = createHash('sha256').update(content, 'utf8').digest('hex');

    return from(fs.readFile(filePath, { encoding: 'utf8' })).pipe(
      map((existing) => createHash('sha256').update(existing, 'utf8').digest('hex')),
      catchError(() => of(null)),
      switchMap((existingHash) => {
        if (newHash === existingHash) return EMPTY;

        return from(fs.mkdir(dirname(filePath), { recursive: true })).pipe(
          switchMap(() => from(fs.writeFile(filePath, content, { encoding: 'utf8' }))),
        );
      }),
    );
  }
}
