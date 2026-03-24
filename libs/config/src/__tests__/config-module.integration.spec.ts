import 'reflect-metadata';

import * as realFs from 'node:fs';
import { join } from 'node:path';

// --- Mocks must be declared before any imports that use them. ---

// Mock node:fs — delegate reads to real fs, intercept writes.
const writeFileSyncSpy = jest.fn();
const mkdirSyncSpy = jest.fn();

jest.mock('node:fs', () => ({
  ...jest.requireActual<typeof import('node:fs')>('node:fs'),
  writeFileSync: (...args: unknown[]): void => writeFileSyncSpy(...args),
  mkdirSync: (...args: unknown[]): void => mkdirSyncSpy(...args),
}));

// Mock @nestjs/common to avoid ESM-only import failures.
const logSpy = jest.fn();
const warnSpy = jest.fn();
const debugSpy = jest.fn();

jest.mock('@nestjs/common', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention
  Module: (): ClassDecorator => (target) => target,

  Logger: jest.fn().mockImplementation(() => ({
    log: logSpy,
    warn: warnSpy,
    debug: debugSpy,
  })),

  DynamicModule: {},
  // eslint-disable-next-line @typescript-eslint/naming-convention
  Injectable: (): ClassDecorator => (target) => target,

  OnModuleInit: {},
}));

// Mock @nestjs/config — `registerAs` attaches KEY and returns the factory.
// `BaseConfigModule.forRoot` and `BaseConfigModule.forFeature` return stubs.
jest.mock('@nestjs/config', () => ({
  registerAs: (token: string | symbol, factory: () => unknown): (() => unknown) => {
    const fn = factory as unknown as Record<string, unknown>;

    fn['KEY'] = token;

    return factory;
  },

  ConfigModule: {
    forRoot: jest.fn().mockReturnValue({ module: 'BaseConfigModule' }),
    forFeature: jest.fn().mockReturnValue({ module: 'BaseConfigModuleFeature' }),
  },

  ConfigService: jest.fn(),
}));

import { ConfigRegistry } from '../config.registry';
import { Env } from '../decorators/env.decorator';
import { ConfigFormat } from '../enums/config-format.enum';
import { ConfigBuilder } from '../helpers/config-builder.helper';
import { EnvResolver } from '../resolvers/env.resolver';
import { YamlResolver } from '../resolvers/yaml.resolver';

// Re-import ConfigModule after mocks are in place.
// eslint-disable-next-line @typescript-eslint/naming-convention
let ConfigModule: typeof import('../config.module').ConfigModule;

beforeAll(async () => {
  const mod = await import('../config.module');

  ConfigModule = mod.ConfigModule;
});

// --- Test fixtures ---

const APP_TOKEN = Symbol('app-config');
const DB_TOKEN = Symbol('db-config');

class AppConfig {
  @Env('app.name', { default: 'my-app', description: 'Application name' })
  public name!: string;

  @Env('app.port', { type: Number, default: 3000 })
  public port!: number;
}

class DbConfig {
  @Env('database.host', { default: 'localhost' })
  public host!: string;

  @Env('database.port', { type: Number, default: 5432 })
  public port!: number;
}

const FIXTURE_PATH = join(__dirname, 'fixtures', 'valid.yaml');

describe('ConfigModule integration', () => {
  beforeEach(() => {
    ConfigRegistry.reset();
    jest.clearAllMocks();
  });

  describe('forRoot', () => {
    it('should register EnvResolver for default (dotenv) format', () => {
      const appConfig = ConfigBuilder.from(AppConfig, APP_TOKEN).build();

      ConfigModule.forRoot({ load: [appConfig] });

      const resolver = ConfigRegistry.getResolver();

      expect(resolver).toBeInstanceOf(EnvResolver);
      expect(resolver.format).toBe(ConfigFormat.Dotenv);
    });

    it('should register YamlResolver for yaml format', () => {
      const appConfig = ConfigBuilder.from(AppConfig, APP_TOKEN).build();

      ConfigModule.forRoot({
        format: ConfigFormat.Yaml,
        path: FIXTURE_PATH,
        load: [appConfig],
      });

      const resolver = ConfigRegistry.getResolver();

      expect(resolver).toBeInstanceOf(YamlResolver);
      expect(resolver.format).toBe(ConfigFormat.Yaml);
    });

    it('should register file paths for IConfigLoadItem entries', () => {
      const dbConfig = ConfigBuilder.from(DbConfig, DB_TOKEN).build();

      ConfigModule.forRoot({
        format: ConfigFormat.Yaml,
        path: FIXTURE_PATH,
        load: [{ path: '/custom/db.yaml', config: dbConfig }],
      });

      expect(ConfigRegistry.getFilePath(DB_TOKEN)).toBe('/custom/db.yaml');
    });

    it('should resolve YAML values correctly when factory is invoked', () => {
      const appConfig = ConfigBuilder.from(AppConfig, APP_TOKEN).build();

      ConfigModule.forRoot({
        format: ConfigFormat.Yaml,
        path: FIXTURE_PATH,
        load: [appConfig],
      });

      // Invoke the factory — it reads from the YAML file via YamlResolver
      const result = appConfig() as AppConfig;

      expect(result.name).toBe('test-app');
      expect(result.port).toBe(3000);
      expect(Object.isFrozen(result)).toBe(true);
    });

    it('should work in dotenv backward-compat mode', () => {
      process.env['APP_NAME'] = 'from-env';
      process.env['APP_PORT'] = '9090';

      try {
        class DotenvAppConfig {
          @Env('APP_NAME', { default: 'fallback' })
          public name!: string;

          @Env('APP_PORT', { type: Number, default: 3000 })
          public port!: number;
        }

        const dotenvToken = Symbol('dotenv-app');
        const dotenvConfig = ConfigBuilder.from(DotenvAppConfig, dotenvToken).build();

        ConfigModule.forRoot({ load: [dotenvConfig] });

        const result = dotenvConfig() as DotenvAppConfig;

        expect(result.name).toBe('from-env');
        expect(result.port).toBe(9090);
      } finally {
        delete process.env['APP_NAME'];
        delete process.env['APP_PORT'];
      }
    });

    it('should return a global DynamicModule', () => {
      const appConfig = ConfigBuilder.from(AppConfig, APP_TOKEN).build();

      const result = ConfigModule.forRoot({ load: [appConfig] });

      expect(result.global).toBe(true);
      expect(result.module).toBeDefined();
    });

    it('should generate example file when sections are available', () => {
      const appConfig = ConfigBuilder.from(AppConfig, APP_TOKEN).build();

      ConfigModule.forRoot({ load: [appConfig] });

      // writeFileSync should have been called with .env.example content
      expect(writeFileSyncSpy).toHaveBeenCalledTimes(1);

      const [filePath, content] = writeFileSyncSpy.mock.calls[0] as [string, string, string];

      expect(filePath).toContain('.env.example');
      expect(content).toContain('app.name');
      expect(content).toContain('app.port');
    });

    it('should generate yaml example for yaml format', () => {
      const appConfig = ConfigBuilder.from(AppConfig, APP_TOKEN).build();

      ConfigModule.forRoot({
        format: ConfigFormat.Yaml,
        path: FIXTURE_PATH,
        load: [appConfig],
      });

      expect(writeFileSyncSpy).toHaveBeenCalledTimes(1);

      const [filePath] = writeFileSyncSpy.mock.calls[0] as [string, string, string];

      expect(filePath).toContain('env.example.yaml');
    });

    it('should skip writing if content hash matches existing file', () => {
      const appConfig = ConfigBuilder.from(AppConfig, APP_TOKEN).build();

      // First call to generate content
      ConfigModule.forRoot({ load: [appConfig] });

      const [writtenPath, firstContent] = writeFileSyncSpy.mock.calls[0] as [
        string,
        string,
        string,
      ];

      // Simulate the file now existing with the same content by spying on readFileSync.
      // We need to intercept existsSync for that specific path only.
      const origExistsSync = realFs.existsSync.bind(realFs);
      const existsSyncSpy = jest
        .spyOn(realFs, 'existsSync')
        .mockImplementation((p: realFs.PathLike) => {
          if (String(p) === writtenPath) return true;

          return origExistsSync(p);
        });

      const origReadFileSync = realFs.readFileSync.bind(realFs);
      const readFileSyncSpy = jest
        .spyOn(realFs, 'readFileSync')
        .mockImplementation((p: realFs.PathOrFileDescriptor, opts?: unknown) => {
          if (String(p) === writtenPath) return firstContent;

          return origReadFileSync(p, opts as BufferEncoding);
        });

      writeFileSyncSpy.mockClear();
      ConfigRegistry.reset();

      ConfigModule.forRoot({ load: [appConfig] });

      expect(writeFileSyncSpy).not.toHaveBeenCalled();

      existsSyncSpy.mockRestore();
      readFileSyncSpy.mockRestore();
    });

    it('should handle empty load array without errors', () => {
      expect(() => ConfigModule.forRoot({ load: [] })).not.toThrow();
      expect(writeFileSyncSpy).not.toHaveBeenCalled();
    });

    it('should handle forRoot with no options', () => {
      expect(() => ConfigModule.forRoot()).not.toThrow();

      const resolver = ConfigRegistry.getResolver();

      expect(resolver).toBeInstanceOf(EnvResolver);
    });
  });

  describe('forFeature', () => {
    it('should accept a bare ConfigFactory', () => {
      // Must call forRoot first to set up the resolver
      ConfigModule.forRoot();

      const dbConfig = ConfigBuilder.from(DbConfig, DB_TOKEN).build();
      const result = ConfigModule.forFeature(dbConfig);

      expect(result.module).toBeDefined();
      expect(result.imports).toBeDefined();
    });

    it('should register file path for IConfigFeatureOptions', () => {
      ConfigModule.forRoot({
        format: ConfigFormat.Yaml,
        path: FIXTURE_PATH,
      });

      const dbConfig = ConfigBuilder.from(DbConfig, DB_TOKEN).build();

      ConfigModule.forFeature({
        path: '/custom/db-feature.yaml',
        config: dbConfig,
      });

      expect(ConfigRegistry.getFilePath(DB_TOKEN)).toBe('/custom/db-feature.yaml');
    });

    it('should accept IConfigFeatureOptions without path', () => {
      ConfigModule.forRoot();

      const dbConfig = ConfigBuilder.from(DbConfig, DB_TOKEN).build();

      expect(() =>
        ConfigModule.forFeature({
          config: dbConfig,
        }),
      ).not.toThrow();
    });
  });

  describe('direct injection by token', () => {
    it('should expose KEY property on factory for @Inject(TOKEN) support', () => {
      const factory = ConfigBuilder.from(AppConfig, APP_TOKEN).build();

      // registerAs attaches KEY — this is what @nestjs/config uses to register
      // the config as a DI provider, enabling @Inject(TOKEN) direct injection.
      expect((factory as unknown as Record<string, unknown>)['KEY']).toBe(APP_TOKEN);
    });

    it('should produce a callable factory that returns the resolved config', () => {
      ConfigModule.forRoot({
        format: ConfigFormat.Yaml,
        path: FIXTURE_PATH,
        load: [],
      });

      const factory = ConfigBuilder.from(AppConfig, APP_TOKEN).build();
      const result = factory() as AppConfig;

      // Factory returns a frozen, resolved config object — same as what @Inject(TOKEN) would provide.
      expect(result.name).toBe('test-app');
      expect(result.port).toBe(3000);
      expect(Object.isFrozen(result)).toBe(true);
    });
  });
});
