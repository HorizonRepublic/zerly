import { createHash } from 'crypto';
import { existsSync } from 'fs';
import * as fs from 'fs/promises';
import { dirname, join, normalize, resolve, sep } from 'path';

import { Injectable, Logger, OnModuleInit } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';

import { catchError, defer, EMPTY, from, map, Observable, of, switchMap, tap } from 'rxjs';

import { APP_CONFIG, ENV_METADATA_KEY } from '../tokens/index';
import { EnumType, EnvTypeConstructor, IAppConfig, IEnvFieldMetadata } from '../types';

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

  private resolveValue(opts: IEnvFieldMetadata['options'], fallback: unknown): string {
    if (opts.example !== undefined) return String(opts.example);
    if (opts.default !== undefined) return String(opts.default);
    return fallback != null && fallback !== '' ? String(fallback) : '';
  }

  private defaultComment(opts: IEnvFieldMetadata['options']): string | undefined {
    if (opts.example !== undefined && opts.default !== undefined) {
      return `(Default: ${opts.default})`;
    }

    return undefined;
  }

  private enumComment(type?: EnumType | EnvTypeConstructor): string | undefined {
    if (!type || typeof type !== 'object') return undefined;

    const unique = [
      ...new Set(Object.values(type).filter((v) => typeof v === 'string' || typeof v === 'number')),
    ];

    return unique.length ? `Possible values: ${unique.join(', ')}` : undefined;
  }

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

  private isValidProjectDir(dirPath: string): boolean {
    return (
      existsSync(dirPath) &&
      (existsSync(join(dirPath, 'package.json')) ||
        existsSync(join(dirPath, 'project.json')) ||
        existsSync(join(dirPath, 'tsconfig.json')))
    );
  }

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
