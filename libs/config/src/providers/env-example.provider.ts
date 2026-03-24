import { createHash } from 'node:crypto';
import * as fs from 'node:fs/promises';
import { dirname, join } from 'node:path';

import { Injectable, Logger, OnModuleInit } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';

import { catchError, defer, EMPTY, from, map, Observable, of, switchMap, tap } from 'rxjs';

import { EnvExampleFormatter } from '../formatters/env-example.formatter';
import { IConfigSection } from '../formatters/example-formatter.interface';
import { resolveAppRoot } from '../helpers/resolve-app-root.helper';
import { APP_CONFIG, ENV_METADATA_KEY } from '../tokens';
import { IAppConfig, IEnvFieldMetadata } from '../types';

/**
 * Secondary `.env.example` generator that runs post-DI.
 *
 * Enhances the baseline example (generated pre-DI by `ConfigModule.forRoot()`)
 * with resolved runtime values from `ConfigService`. Only active when
 * `AppConfig.generateEnvExample` is `true`.
 */
@Injectable()
export class EnvExampleProvider implements OnModuleInit {
  private readonly logger = new Logger(EnvExampleProvider.name);
  private readonly formatter = new EnvExampleFormatter();

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
   * Reads all config instances from `ConfigService.internalConfig`,
   * extracts `@Env()` metadata, formats via `EnvExampleFormatter`,
   * and writes to `<appRoot>/.env.example`.
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

      const sections = this.collectSections(configs);

      if (sections.length === 0) {
        this.logger.debug('No configurations with @Env decorators found.');
        return EMPTY;
      }

      const templateContent = this.formatter.format(sections);
      const outputPath = join(resolveAppRoot(), '.env.example');

      return this.writeIfChanged$(outputPath, templateContent).pipe(
        tap(() => {
          this.logger.log(`Environment example generated: ${outputPath}`);
        }),
      );
    });
  }

  /**
   * Collects config sections from ConfigService's internal config map.
   */
  private collectSections(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    configs: Record<string | symbol, any>,
  ): IConfigSection[] {
    const symbolKeys = Object.getOwnPropertySymbols(configs).map((k) => ({
      key: k as string | symbol,
      title: k.description,
    }));
    const stringKeys = Object.keys(configs).map((k) => ({ key: k as string | symbol, title: k }));
    const allKeys = [...symbolKeys, ...stringKeys];

    return allKeys
      .map(({ key, title }) => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const instance = (configs as any)[key] as Record<string, unknown> | undefined;

        if (!instance || !title) return undefined;

        const fields: IEnvFieldMetadata[] = Reflect.getMetadata(ENV_METADATA_KEY, instance) ?? [];

        if (!fields.length) return undefined;

        return { title, fields, instance } satisfies IConfigSection;
      })
      .filter((s): s is IConfigSection => Boolean(s));
  }

  /**
   * Writes `content` to `filePath` only when the SHA-256 hash differs from the
   * existing file, avoiding unnecessary writes on repeated restarts.
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
