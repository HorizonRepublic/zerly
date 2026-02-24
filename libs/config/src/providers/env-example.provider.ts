import { createHash } from 'crypto';
import { existsSync } from 'fs';
import * as fs from 'fs/promises';
import { dirname, join, normalize, resolve, sep } from 'path';

import { Injectable, Logger, OnModuleInit } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';

import { catchError, defer, EMPTY, from, map, Observable, of, switchMap, tap } from 'rxjs';

import { ENV_METADATA_KEY } from '../tokens';
import { APP_CONFIG } from '../tokens/index';
import { EnumType, EnvTypeConstructor, IAppConfig, IEnvFieldMetadata } from '../types';

interface IFormattedLine {
  declaration: string;
  comment?: string;
}

@Injectable()
export class EnvExampleProvider implements OnModuleInit {
  private static readonly fileEncoding = 'utf8' as const;
  private static readonly fileExtension = '.env.example' as const;
  private static readonly hashAlgorithm = 'sha256' as const;

  private static readonly templateHeader = `###
#
# This file is auto-generated based on all registered configurations.
# Do not edit it manually.
#
###` as const;

  private readonly logger = new Logger(EnvExampleProvider.name);

  public constructor(private readonly configService: ConfigService) {}

  public onModuleInit(): void {
    // Fire-and-forget subscription to not block application bootstrap
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
      try {
        const appConfig = this.configService.get<IAppConfig>(APP_CONFIG);

        if (!appConfig) {
          this.logger.debug('AppConfig not found, skipping .env.example generation.');
          return of(null);
        }

        return of(appConfig);
      } catch {
        this.logger.warn('Error retrieving AppConfig during env generation.');
        return of(null);
      }
    }).pipe(
      switchMap((appConfig) => {
        if (!appConfig?.generateEnvExample) {
          return EMPTY;
        }

        const configSections = this.extractConfigurationSections();

        if (configSections.length === 0) {
          this.logger.debug('No configurations with @Env decorators found.');
          return EMPTY;
        }

        const templateContent = this.buildTemplate(configSections);
        const outputPath = this.buildOutputPath();

        return this.processFileGeneration(outputPath, templateContent).pipe(
          tap(() => {
            this.logger.log(`Environment example generated: ${outputPath}`);
          }),
        );
      }),
    );
  }

  // --- Core Logic ---

  private extractConfigurationSections(): string[] {
    const internalConfigs = this.getInternalConfigurations();

    if (!internalConfigs) return [];

    const configSymbols = Object.getOwnPropertySymbols(internalConfigs);

    return configSymbols
      .map((symbol) => this.processConfigurationSymbol(symbol, internalConfigs))
      .filter((section): section is string => Boolean(section));
  }

  private processConfigurationSymbol(
    symbolKey: symbol,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    configs: Record<symbol, any>,
  ): string | undefined {
    const configTitle = symbolKey.description;
    const configInstance = configs[symbolKey];

    if (!configInstance || !configTitle) return undefined;

    const envFields = this.extractEnvFields(configInstance);

    if (!envFields.length) return undefined;

    const sectionVariables = this.formatEnvironmentVariables(envFields, configInstance);

    return this.formatConfigurationSection(configTitle, sectionVariables);
  }

  private formatEnvironmentVariables(
    envFields: IEnvFieldMetadata[],
    configInstance: Record<string, unknown>,
  ): string[] {
    const lines = envFields.map(({ key, options, propertyKey }) => {
      const instanceValue = configInstance[propertyKey as string];

      const value = this.determineVariableValue(options, instanceValue);

      const defaultValueForComment = options.default ?? instanceValue;

      const commentsParts = [
        options.comment,
        this.extractEnumComment(options.type),
        this.extractDefaultComment(defaultValueForComment),
      ].filter(Boolean);

      const combinedComment = commentsParts.length > 0 ? commentsParts.join('. ') : undefined;

      return this.prepareLineData(key, value, combinedComment);
    });

    const maxDeclLength = lines.reduce((max, line) => Math.max(max, line.declaration.length), 0);

    return lines.map(({ declaration, comment }) => {
      if (!comment) return declaration;
      return `${declaration.padEnd(maxDeclLength + 1)}# ${comment}`;
    });
  }

  // --- Values & Comments ---

  private determineVariableValue(
    options: IEnvFieldMetadata['options'],
    instanceValue: unknown,
  ): string {
    // Priority 1: Example value provided in decorator
    if (options.example !== undefined) return String(options.example);

    // Priority 2: Default value provided in decorator
    if (options.default !== undefined) return String(options.default);

    // Priority 3: Runtime value (fallback, potentially dirty from .env)
    if (this.isValidValue(instanceValue)) return String(instanceValue);

    return '';
  }

  private extractDefaultComment(value: unknown): string | undefined {
    if (this.isValidValue(value)) return `(Default: ${value})`;
    return undefined;
  }

  private isValidValue(value: unknown): boolean {
    return value !== undefined && value !== null && value !== '';
  }

  private extractEnumComment(type?: EnumType | EnvTypeConstructor): string | undefined {
    if (!type || typeof type !== 'object') return undefined;

    const values = Object.values(type).filter(
      (v) => typeof v === 'string' || typeof v === 'number',
    );
    const uniqueValues = [...new Set(values)];

    if (!uniqueValues.length) return undefined;
    return `Possible values: ${uniqueValues.join(', ')}`;
  }

  // --- Helpers ---

  private extractEnvFields(instance: object): IEnvFieldMetadata[] {
    return Reflect.getMetadata(ENV_METADATA_KEY, instance) ?? [];
  }

  private getInternalConfigurations(): Record<symbol, unknown> | undefined {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    return (this.configService as any)['internalConfig'];
  }

  private prepareLineData(key: string, value: string, comment?: string): IFormattedLine {
    const declaration = `${key}="${value}"`;

    if (comment) {
      return { declaration, comment };
    }

    return { declaration };
  }

  private formatConfigurationSection(title: string, lines: string[]): string {
    return `# -- ${title}\n${lines.join('\n')}`;
  }

  private buildTemplate(sections: string[]): string {
    return `${EnvExampleProvider.templateHeader}\n\n${sections.join('\n\n')}\n`;
  }

  // --- File System & Paths ---

  private buildOutputPath(): string {
    const fileName = EnvExampleProvider.fileExtension;
    const appRoot = this.resolveAppRoot();

    return join(appRoot, fileName);
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

        if (this.isValidProjectDirectory(appRoot)) return appRoot;
        if (this.isValidProjectDirectory(potentialRoot)) return potentialRoot;
      }
    }

    return this.resolveFallbackRoot();
  }

  private resolveFallbackRoot(): string {
    try {
      const appConfig = this.configService.get<IAppConfig>(APP_CONFIG);

      if (appConfig?.name) {
        // Check standard monorepo structure: root/apps/<name>
        const monorepoPath = join(process.cwd(), 'apps', appConfig.name);

        if (this.isValidProjectDirectory(monorepoPath)) return monorepoPath;

        // Check if the app is in the root (standalone) or other structure
        const rootPath = join(process.cwd(), appConfig.name);

        if (this.isValidProjectDirectory(rootPath)) return rootPath;
      }
    } catch {
      // ignore
    }

    return process.cwd();
  }

  private isValidProjectDirectory(dirPath: string): boolean {
    return (
      existsSync(dirPath) &&
      (existsSync(join(dirPath, 'package.json')) ||
        existsSync(join(dirPath, 'project.json')) ||
        existsSync(join(dirPath, 'tsconfig.json')))
    );
  }

  // --- IO Operations ---

  private processFileGeneration(outputPath: string, templateContent: string): Observable<void> {
    return this.shouldUpdateFile$(outputPath, templateContent).pipe(
      switchMap((shouldUpdate) => {
        if (!shouldUpdate) return EMPTY;
        return this.writeExampleFile$(outputPath, templateContent);
      }),
    );
  }

  private shouldUpdateFile$(filePath: string, newContent: string): Observable<boolean> {
    const newContentHash = this.generateContentHash(newContent);

    return this.readExistingFileHash$(filePath).pipe(
      map((existingHash) => newContentHash !== existingHash),
    );
  }

  private readExistingFileHash$(filePath: string): Observable<string | null> {
    return from(fs.readFile(filePath, { encoding: EnvExampleProvider.fileEncoding })).pipe(
      map((content) => this.generateContentHash(content)),
      catchError(() => of(null)),
    );
  }

  private writeExampleFile$(filePath: string, content: string): Observable<void> {
    return this.ensureDirectoryExists$(filePath).pipe(
      switchMap(() =>
        from(fs.writeFile(filePath, content, { encoding: EnvExampleProvider.fileEncoding })),
      ),
    );
  }

  private ensureDirectoryExists$(filePath: string): Observable<void> {
    const dir = dirname(filePath);

    return from(fs.mkdir(dir, { recursive: true })).pipe(map(() => void 0));
  }

  private generateContentHash(content: string): string {
    return createHash(EnvExampleProvider.hashAlgorithm)
      .update(content, EnvExampleProvider.fileEncoding)
      .digest('hex');
  }
}
