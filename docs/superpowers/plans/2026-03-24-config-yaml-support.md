# YAML Config Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `@zerly/config` to support YAML files as a config source alongside dotenv, with lazy evaluation, resolver abstraction, and pre-DI example generation.

**Architecture:** ConfigResolver interface with EnvResolver/YamlResolver implementations. ConfigRegistry static singleton bridges `forRoot()` setup with lazy `ConfigBuilder.build()` factory closures. Example generation runs synchronously in `forRoot()` before DI.

**Tech Stack:** TypeScript, NestJS 11+, `@nestjs/config`, `yaml` npm package, Jest, Reflect metadata API

**Spec:** `docs/superpowers/specs/2026-03-24-config-yaml-support-design.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|---------------|
| NEW | `libs/config/src/enums/config-format.enum.ts` | `ConfigFormat` enum (`dotenv`, `yaml`) |
| NEW | `libs/config/src/types/config-module-options.interface.ts` | `IConfigModuleOptions` interface |
| NEW | `libs/config/src/types/config-load-item.interface.ts` | `IConfigLoadItem` interface |
| NEW | `libs/config/src/types/config-feature-options.interface.ts` | `IConfigFeatureOptions` interface |
| NEW | `libs/config/src/resolvers/config-resolver.interface.ts` | `IConfigResolver` interface |
| NEW | `libs/config/src/resolvers/env.resolver.ts` | `EnvResolver` — reads `process.env` |
| NEW | `libs/config/src/resolvers/yaml.resolver.ts` | `YamlResolver` — parses YAML, dot-path resolution |
| NEW | `libs/config/src/resolvers/config-resolver.factory.ts` | `ConfigResolverFactory` — creates resolver by format |
| NEW | `libs/config/src/config.registry.ts` | `ConfigRegistry` static singleton |
| NEW | `libs/config/src/formatters/example-formatter.interface.ts` | `IExampleFormatter` interface |
| NEW | `libs/config/src/formatters/env-example.formatter.ts` | `EnvExampleFormatter` — `.env.example` formatting |
| NEW | `libs/config/src/formatters/yaml-example.formatter.ts` | `YamlExampleFormatter` — `.env.example.yaml` formatting |
| MODIFY | `libs/config/src/types/index.ts` | Add `Array` to `EnvTypeConstructor`, update `InferTypeFromConstructor` |
| MODIFY | `libs/config/src/enums/index.ts` | Re-export `ConfigFormat` |
| MODIFY | `libs/config/src/tokens/index.ts` | Add `CONFIG_RESOLVER` token |
| MODIFY | `libs/config/src/helpers/config-builder.helper.ts` | Lazy `build()`, resolver delegation |
| MODIFY | `libs/config/src/providers/env-example.provider.ts` | Delegate to formatters |
| MODIFY | `libs/config/src/config.module.ts` | New `forRoot`/`forFeature` signatures, registry setup, pre-DI example gen |
| MODIFY | `libs/config/src/index.ts` | Export new modules |
| MODIFY | `libs/config/package.json` | Add `yaml` dependency |
| MODIFY | `libs/kernel/src/kernel.module.ts` | Update `forRoot()` call to new object signature |
| NEW | `libs/config/src/__tests__/config-registry.spec.ts` | ConfigRegistry unit tests |
| NEW | `libs/config/src/__tests__/env.resolver.spec.ts` | EnvResolver unit tests |
| NEW | `libs/config/src/__tests__/yaml.resolver.spec.ts` | YamlResolver unit tests |
| NEW | `libs/config/src/__tests__/config-resolver.factory.spec.ts` | Factory unit tests |
| NEW | `libs/config/src/__tests__/config-builder.spec.ts` | ConfigBuilder lazy eval tests |
| NEW | `libs/config/src/__tests__/env-example.formatter.spec.ts` | Env formatter tests |
| NEW | `libs/config/src/__tests__/yaml-example.formatter.spec.ts` | YAML formatter tests |
| NEW | `libs/config/src/__tests__/config-module.integration.spec.ts` | Integration tests |

---

### Task 1: Add `yaml` dependency and types foundation

**Files:**
- Modify: `libs/config/package.json`
- Create: `libs/config/src/enums/config-format.enum.ts`
- Modify: `libs/config/src/enums/index.ts`
- Create: `libs/config/src/types/config-module-options.interface.ts`
- Create: `libs/config/src/types/config-load-item.interface.ts`
- Create: `libs/config/src/types/config-feature-options.interface.ts`
- Modify: `libs/config/src/types/index.ts`
- Modify: `libs/config/src/tokens/index.ts`
- Modify: `libs/config/src/index.ts`

- [ ] **Step 1: Add `yaml` to config package dependencies**

In `libs/config/package.json`, add `"yaml": "^2.7.0"` to `dependencies`.

- [ ] **Step 2: Install dependency**

Run: `pnpm install`

- [ ] **Step 3: Create ConfigFormat enum**

Create `libs/config/src/enums/config-format.enum.ts`:

```typescript
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
```

- [ ] **Step 4: Re-export ConfigFormat from enums index**

In `libs/config/src/enums/index.ts`, add:

```typescript
export * from './config-format.enum';
```

- [ ] **Step 5: Update type system — add Array support**

In `libs/config/src/types/index.ts`, update `EnvTypeConstructor`:

```typescript
export type EnvTypeConstructor = typeof Boolean | typeof Number | typeof String | typeof Array;
```

Update `ConstructorToType` to handle Array:

```typescript
export type ConstructorToType<T> = T extends typeof String
  ? string
  : T extends typeof Number
    ? number
    : T extends typeof Boolean
      ? boolean
      : T extends typeof Array
        ? unknown[]
        : never;
```

Update `InferTypeFromConstructor` — Array must be checked before `EnvTypeConstructor` since `typeof Array` is now in the union:

```typescript
export type InferTypeFromConstructor<TType extends EnumType | EnvTypeConstructor> =
  TType extends typeof Array
    ? unknown[]
    : TType extends EnumType
      ? TType[keyof TType]
      : TType extends EnvTypeConstructor
        ? ConstructorToType<TType>
        : never;
```

- [ ] **Step 6: Create IConfigLoadItem interface**

Create `libs/config/src/types/config-load-item.interface.ts`:

```typescript
import { ConfigFactory } from './config-factory.type';

/**
 * A config factory with a specific YAML file path override.
 * Used in `ConfigModule.forRoot({ load })` when a config needs its own file.
 * @example
 * ```typescript
 * { path: 'config/database.yaml', config: dbConfig }
 * ```
 */
export interface IConfigLoadItem {
  /** Absolute or relative path to the YAML file for this config. */
  readonly path: string;

  /** The config factory returned by `ConfigBuilder.build()`. */
  readonly config: ConfigFactory;
}
```

- [ ] **Step 7: Create IConfigModuleOptions interface**

Create `libs/config/src/types/config-module-options.interface.ts`:

```typescript
import { ConfigFormat } from '../enums/config-format.enum';
import { IConfigLoadItem } from './config-load-item.interface';
import { ConfigFactory } from './config-factory.type';

/**
 * Options for `ConfigModule.forRoot()`.
 * @example
 * ```typescript
 * ConfigModule.forRoot({
 *   format: ConfigFormat.Yaml,
 *   path: 'config/app.yaml',
 *   load: [appConfig, { path: 'config/db.yaml', config: dbConfig }],
 * })
 * ```
 */
export interface IConfigModuleOptions {
  /**
   * Config source format. Determines how `@Env()` keys are interpreted.
   * @default ConfigFormat.Dotenv
   */
  readonly format?: ConfigFormat;

  /**
   * Default YAML file path. Used by configs that don't specify their own path.
   * Only applicable when `format` is `ConfigFormat.Yaml`.
   */
  readonly path?: string;

  /** Config factories to load. Accepts bare `ConfigFactory` or `{ path, config }` objects. */
  readonly load?: Array<ConfigFactory | IConfigLoadItem>;
}
```

- [ ] **Step 8: Create IConfigFeatureOptions interface**

Create `libs/config/src/types/config-feature-options.interface.ts`:

```typescript
import { ConfigFactory } from './config-factory.type';

/**
 * Options for `ConfigModule.forFeature()` with an optional YAML file path.
 * @example
 * ```typescript
 * ConfigModule.forFeature({
 *   path: 'config/notifications.yaml',
 *   config: notificationsConfig,
 * })
 * ```
 */
export interface IConfigFeatureOptions {
  /** Path to the YAML file for this feature config. */
  readonly path?: string;

  /** The config factory returned by `ConfigBuilder.build()`. */
  readonly config: ConfigFactory;
}
```

- [ ] **Step 9: Add CONFIG_RESOLVER token**

In `libs/config/src/tokens/index.ts`, add:

```typescript
/** DI token for the active config resolver instance. */
export const CONFIG_RESOLVER = Symbol('config-resolver');
```

- [ ] **Step 10: Update main index.ts with new exports**

In `libs/config/src/index.ts`, add exports for new types:

```typescript
export * from './types/config-module-options.interface';
export * from './types/config-load-item.interface';
export * from './types/config-feature-options.interface';
```

- [ ] **Step 11: Verify build passes**

Run: `pnpm nx build config`
Expected: Successful compilation

- [ ] **Step 12: Commit**

```bash
git add libs/config/
git commit -m "feat(config): add types, enums, and yaml dependency for YAML config support"
```

---

### Task 2: Implement ConfigResolver interface and EnvResolver

**Files:**
- Create: `libs/config/src/resolvers/config-resolver.interface.ts`
- Create: `libs/config/src/resolvers/env.resolver.ts`
- Test: `libs/config/src/__tests__/env.resolver.spec.ts`

- [ ] **Step 1: Write EnvResolver tests**

Create `libs/config/src/__tests__/env.resolver.spec.ts`:

```typescript
import { ConfigFormat } from '../enums/config-format.enum';
import { EnvResolver } from '../resolvers/env.resolver';

describe('EnvResolver', () => {
  let resolver: EnvResolver;

  beforeEach(() => {
    resolver = new EnvResolver();
  });

  afterEach(() => {
    delete process.env['TEST_KEY'];
    delete process.env['TEST_ARRAY'];
  });

  it('should have dotenv format', () => {
    expect(resolver.format).toBe(ConfigFormat.Dotenv);
  });

  it('should return env var value as string', () => {
    process.env['TEST_KEY'] = '3000';
    expect(resolver.get('TEST_KEY')).toBe('3000');
  });

  it('should return undefined for missing key', () => {
    expect(resolver.get('NONEXISTENT_KEY')).toBeUndefined();
  });

  it('should return true for existing key via has()', () => {
    process.env['TEST_KEY'] = 'value';
    expect(resolver.has('TEST_KEY')).toBe(true);
  });

  it('should return false for missing key via has()', () => {
    expect(resolver.has('NONEXISTENT_KEY')).toBe(false);
  });

  it('should ignore filePath parameter', () => {
    process.env['TEST_KEY'] = 'value';
    expect(resolver.get('TEST_KEY', '/some/path.yaml')).toBe('value');
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm nx test config -- --testPathPattern=env.resolver`
Expected: FAIL — modules not found

- [ ] **Step 3: Create IConfigResolver interface**

Create `libs/config/src/resolvers/config-resolver.interface.ts`:

```typescript
import { ConfigFormat } from '../enums/config-format.enum';

/**
 * Abstraction for resolving configuration values from different sources.
 *
 * Implementations read from a specific source (environment variables, YAML files, etc.)
 * and return values by key. The key format depends on the source:
 * - `EnvResolver`: flat env var names (e.g. `'APP_PORT'`)
 * - `YamlResolver`: dot-separated paths (e.g. `'database.port'`)
 */
export interface IConfigResolver {
  /**
   * Retrieves a configuration value by key.
   * @param key - The configuration key to resolve.
   * @param filePath - Optional file path override. Used by YAML resolver to
   *   select a specific file instead of the default. Ignored by EnvResolver.
   * @returns The resolved value, or `undefined` if the key does not exist.
   */
  get(key: string, filePath?: string): unknown;

  /**
   * Checks whether a configuration key exists in the source.
   * @param key - The configuration key to check.
   * @param filePath - Optional file path override (YAML only).
   * @returns `true` if the key exists, `false` otherwise.
   */
  has(key: string, filePath?: string): boolean;

  /** The configuration format this resolver handles. */
  readonly format: ConfigFormat;
}
```

- [ ] **Step 4: Create EnvResolver**

Create `libs/config/src/resolvers/env.resolver.ts`:

```typescript
import { ConfigFormat } from '../enums/config-format.enum';
import { IConfigResolver } from './config-resolver.interface';

/**
 * Resolves configuration values from `process.env`.
 *
 * All values are returned as strings — type conversion is handled
 * downstream by `ConfigBuilder`.
 */
export class EnvResolver implements IConfigResolver {
  /** @inheritdoc */
  public readonly format = ConfigFormat.Dotenv;

  /**
   * Reads a value from `process.env`.
   * @param key - Environment variable name (e.g. `'APP_PORT'`).
   * @param _filePath - Ignored. Present for interface compatibility.
   * @returns The env var value as a string, or `undefined` if not set.
   */
  public get(key: string, _filePath?: string): string | undefined {
    return process.env[key];
  }

  /**
   * Checks whether an environment variable is defined.
   * @param key - Environment variable name.
   * @param _filePath - Ignored.
   * @returns `true` if the env var exists in `process.env`.
   */
  public has(key: string, _filePath?: string): boolean {
    return key in process.env;
  }
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `pnpm nx test config -- --testPathPattern=env.resolver`
Expected: PASS (all 6 tests)

- [ ] **Step 6: Commit**

```bash
git add libs/config/src/resolvers/ libs/config/src/__tests__/env.resolver.spec.ts
git commit -m "feat(config): add IConfigResolver interface and EnvResolver"
```

---

### Task 3: Implement YamlResolver

**Files:**
- Create: `libs/config/src/resolvers/yaml.resolver.ts`
- Test: `libs/config/src/__tests__/yaml.resolver.spec.ts`
- Test fixtures: `libs/config/src/__tests__/fixtures/valid.yaml`, `libs/config/src/__tests__/fixtures/invalid.yaml`, `libs/config/src/__tests__/fixtures/empty.yaml`

- [ ] **Step 1: Create test fixtures**

Create `libs/config/src/__tests__/fixtures/valid.yaml`:

```yaml
app:
  name: test-app
  port: 3000
  debug: true

database:
  host: localhost
  port: 5432
  replicas:
    - host: replica1.db.com
      port: 5432
    - host: replica2.db.com
      port: 5433

allowed_origins:
  - https://app.com
  - https://admin.app.com
```

Create `libs/config/src/__tests__/fixtures/invalid.yaml`:

```
this: is: not: valid: yaml: [
```

Create `libs/config/src/__tests__/fixtures/empty.yaml` (empty file).

- [ ] **Step 2: Write YamlResolver tests**

Create `libs/config/src/__tests__/yaml.resolver.spec.ts`:

```typescript
import { join } from 'node:path';

import { ConfigFormat } from '../enums/config-format.enum';
import { YamlResolver } from '../resolvers/yaml.resolver';

const FIXTURES = join(__dirname, 'fixtures');
const VALID_YAML = join(FIXTURES, 'valid.yaml');
const INVALID_YAML = join(FIXTURES, 'invalid.yaml');
const EMPTY_YAML = join(FIXTURES, 'empty.yaml');

describe('YamlResolver', () => {
  it('should have yaml format', () => {
    const resolver = new YamlResolver(VALID_YAML);
    expect(resolver.format).toBe(ConfigFormat.Yaml);
  });

  describe('scalar values', () => {
    it('should resolve string by dot-path', () => {
      const resolver = new YamlResolver(VALID_YAML);
      expect(resolver.get('app.name')).toBe('test-app');
    });

    it('should resolve number by dot-path', () => {
      const resolver = new YamlResolver(VALID_YAML);
      expect(resolver.get('app.port')).toBe(3000);
    });

    it('should resolve boolean by dot-path', () => {
      const resolver = new YamlResolver(VALID_YAML);
      expect(resolver.get('app.debug')).toBe(true);
    });

    it('should return undefined for missing key', () => {
      const resolver = new YamlResolver(VALID_YAML);
      expect(resolver.get('app.nonexistent')).toBeUndefined();
    });

    it('should return undefined for partially matching path', () => {
      const resolver = new YamlResolver(VALID_YAML);
      expect(resolver.get('app.name.extra')).toBeUndefined();
    });
  });

  describe('structured values', () => {
    it('should resolve array of objects', () => {
      const resolver = new YamlResolver(VALID_YAML);
      expect(resolver.get('database.replicas')).toEqual([
        { host: 'replica1.db.com', port: 5432 },
        { host: 'replica2.db.com', port: 5433 },
      ]);
    });

    it('should resolve array of strings', () => {
      const resolver = new YamlResolver(VALID_YAML);
      expect(resolver.get('allowed_origins')).toEqual([
        'https://app.com',
        'https://admin.app.com',
      ]);
    });

    it('should resolve nested object', () => {
      const resolver = new YamlResolver(VALID_YAML);
      expect(resolver.get('database')).toEqual({
        host: 'localhost',
        port: 5432,
        replicas: [
          { host: 'replica1.db.com', port: 5432 },
          { host: 'replica2.db.com', port: 5433 },
        ],
      });
    });
  });

  describe('has()', () => {
    it('should return true for existing path', () => {
      const resolver = new YamlResolver(VALID_YAML);
      expect(resolver.has('app.name')).toBe(true);
    });

    it('should return false for missing path', () => {
      const resolver = new YamlResolver(VALID_YAML);
      expect(resolver.has('app.missing')).toBe(false);
    });
  });

  describe('file path override', () => {
    it('should use override path instead of default', () => {
      const resolver = new YamlResolver('/nonexistent/default.yaml');
      expect(resolver.get('app.name', VALID_YAML)).toBe('test-app');
    });
  });

  describe('caching', () => {
    it('should cache parsed YAML per file path', () => {
      const resolver = new YamlResolver(VALID_YAML);
      const result1 = resolver.get('app.name');
      const result2 = resolver.get('app.port');
      expect(result1).toBe('test-app');
      expect(result2).toBe(3000);
      // If caching didn't work, each call would re-parse — tested via no errors on repeated calls
    });
  });

  describe('error handling', () => {
    it('should throw on missing file', () => {
      const resolver = new YamlResolver('/nonexistent/file.yaml');
      expect(() => resolver.get('any.key')).toThrow(/not found/i);
    });

    it('should throw on invalid YAML', () => {
      const resolver = new YamlResolver(INVALID_YAML);
      expect(() => resolver.get('any.key')).toThrow(/parse/i);
    });

    it('should return undefined for keys in empty file', () => {
      const resolver = new YamlResolver(EMPTY_YAML);
      expect(resolver.get('any.key')).toBeUndefined();
    });
  });
});
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `pnpm nx test config -- --testPathPattern=yaml.resolver`
Expected: FAIL — module not found

- [ ] **Step 4: Implement YamlResolver**

Create `libs/config/src/resolvers/yaml.resolver.ts`:

```typescript
import { existsSync, readFileSync } from 'node:fs';

import { Logger } from '@nestjs/common';
import { parse } from 'yaml';

import { ConfigFormat } from '../enums/config-format.enum';
import { IConfigResolver } from './config-resolver.interface';

/**
 * Resolves configuration values from YAML files using dot-path notation.
 *
 * Parsed YAML content is cached per file path. Files are read synchronously
 * at first access — acceptable for startup-only configuration loading.
 *
 * @example
 * ```typescript
 * const resolver = new YamlResolver('config/app.yaml');
 * resolver.get('database.port');       // → 5432 (number from YAML)
 * resolver.get('database.replicas');   // → [{ host: '...', port: 5432 }, ...]
 * ```
 */
export class YamlResolver implements IConfigResolver {
  /** @inheritdoc */
  public readonly format = ConfigFormat.Yaml;

  private readonly logger = new Logger(YamlResolver.name);

  /** Cache of parsed YAML content, keyed by absolute file path. */
  private readonly cache = new Map<string, Record<string, unknown>>();

  /**
   * @param defaultPath - Default YAML file path. Used when `get()`/`has()`
   *   are called without a `filePath` override.
   */
  public constructor(private readonly defaultPath: string) {}

  /**
   * Retrieves a value from parsed YAML using dot-path notation.
   * @param key - Dot-separated path (e.g. `'database.replicas'`).
   * @param filePath - Optional file path override. Falls back to `defaultPath`.
   * @returns The resolved value (scalar, array, or object), or `undefined` if not found.
   * @throws {Error} If the YAML file does not exist or contains invalid syntax.
   */
  public get(key: string, filePath?: string): unknown {
    const parsed = this.loadFile(filePath ?? this.defaultPath);

    if (!parsed) return undefined;

    return this.resolveByDotPath(parsed, key);
  }

  /**
   * Checks whether a dot-path key exists in the YAML file.
   * @param key - Dot-separated path.
   * @param filePath - Optional file path override.
   * @returns `true` if the key resolves to a defined value.
   */
  public has(key: string, filePath?: string): boolean {
    return this.get(key, filePath) !== undefined;
  }

  /**
   * Loads and parses a YAML file, caching the result.
   * @param path - Absolute or relative file path.
   * @returns Parsed YAML as a plain object, or `undefined` for empty files.
   * @throws {Error} If the file does not exist or contains invalid YAML.
   */
  private loadFile(path: string): Record<string, unknown> | undefined {
    if (this.cache.has(path)) {
      return this.cache.get(path);
    }

    if (!existsSync(path)) {
      throw new Error(`YAML config file not found: ${path}`);
    }

    const content = readFileSync(path, 'utf8');

    if (!content.trim()) {
      this.logger.warn(`YAML config file is empty: ${path}`);
      this.cache.set(path, {});
      return undefined;
    }

    try {
      const parsed = parse(content) as Record<string, unknown>;

      this.cache.set(path, parsed);

      return parsed;
    } catch (error) {
      throw new Error(
        `Failed to parse YAML file ${path}: ${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }

  /**
   * Traverses a nested object using a dot-separated path.
   * @param obj - The root object to traverse.
   * @param dotPath - Dot-separated key (e.g. `'database.replicas'`).
   * @returns The value at the path, or `undefined` if any segment is missing.
   */
  private resolveByDotPath(obj: Record<string, unknown>, dotPath: string): unknown {
    const segments = dotPath.split('.');
    let current: unknown = obj;

    for (const segment of segments) {
      if (current === null || current === undefined || typeof current !== 'object') {
        return undefined;
      }

      current = (current as Record<string, unknown>)[segment];
    }

    return current;
  }
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `pnpm nx test config -- --testPathPattern=yaml.resolver`
Expected: PASS (all tests)

- [ ] **Step 6: Commit**

```bash
git add libs/config/src/resolvers/yaml.resolver.ts libs/config/src/__tests__/
git commit -m "feat(config): add YamlResolver with dot-path resolution and caching"
```

---

### Task 4: Implement ConfigResolverFactory

**Files:**
- Create: `libs/config/src/resolvers/config-resolver.factory.ts`
- Test: `libs/config/src/__tests__/config-resolver.factory.spec.ts`

- [ ] **Step 1: Write factory tests**

Create `libs/config/src/__tests__/config-resolver.factory.spec.ts`:

```typescript
import { ConfigFormat } from '../enums/config-format.enum';
import { ConfigResolverFactory } from '../resolvers/config-resolver.factory';
import { EnvResolver } from '../resolvers/env.resolver';
import { YamlResolver } from '../resolvers/yaml.resolver';

describe('ConfigResolverFactory', () => {
  it('should create EnvResolver for dotenv format', () => {
    const resolver = ConfigResolverFactory.create(ConfigFormat.Dotenv);
    expect(resolver).toBeInstanceOf(EnvResolver);
  });

  it('should create YamlResolver for yaml format', () => {
    const resolver = ConfigResolverFactory.create(ConfigFormat.Yaml, '/some/path.yaml');
    expect(resolver).toBeInstanceOf(YamlResolver);
  });

  it('should create EnvResolver when no format specified', () => {
    const resolver = ConfigResolverFactory.create();
    expect(resolver).toBeInstanceOf(EnvResolver);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm nx test config -- --testPathPattern=config-resolver.factory`
Expected: FAIL

- [ ] **Step 3: Implement ConfigResolverFactory**

Create `libs/config/src/resolvers/config-resolver.factory.ts`:

```typescript
import { ConfigFormat } from '../enums/config-format.enum';
import { IConfigResolver } from './config-resolver.interface';
import { EnvResolver } from './env.resolver';
import { YamlResolver } from './yaml.resolver';

/**
 * Factory for creating the appropriate `IConfigResolver` based on format.
 */
export class ConfigResolverFactory {
  /**
   * Creates a config resolver instance.
   * @param format - The configuration format. Defaults to `ConfigFormat.Dotenv`.
   * @param defaultPath - Default YAML file path. Required when `format` is `Yaml`.
   * @returns A resolver instance matching the requested format.
   */
  public static create(
    format: ConfigFormat = ConfigFormat.Dotenv,
    defaultPath?: string,
  ): IConfigResolver {
    switch (format) {
      case ConfigFormat.Yaml: {
        return new YamlResolver(defaultPath ?? '');
      }

      case ConfigFormat.Dotenv:
      default: {
        return new EnvResolver();
      }
    }
  }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm nx test config -- --testPathPattern=config-resolver.factory`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add libs/config/src/resolvers/config-resolver.factory.ts libs/config/src/__tests__/config-resolver.factory.spec.ts
git commit -m "feat(config): add ConfigResolverFactory"
```

---

### Task 5: Implement ConfigRegistry

**Files:**
- Create: `libs/config/src/config.registry.ts`
- Test: `libs/config/src/__tests__/config-registry.spec.ts`

- [ ] **Step 1: Write ConfigRegistry tests**

Create `libs/config/src/__tests__/config-registry.spec.ts`:

```typescript
import { ConfigRegistry } from '../config.registry';
import { EnvResolver } from '../resolvers/env.resolver';

describe('ConfigRegistry', () => {
  afterEach(() => {
    ConfigRegistry.reset();
  });

  it('should store and retrieve resolver', () => {
    const resolver = new EnvResolver();
    ConfigRegistry.setResolver(resolver);
    expect(ConfigRegistry.getResolver()).toBe(resolver);
  });

  it('should throw when getting resolver before setting', () => {
    expect(() => ConfigRegistry.getResolver()).toThrow(/not initialized/i);
  });

  it('should store and retrieve file paths by token', () => {
    const token = Symbol('test');
    ConfigRegistry.setFilePath(token, '/path/to/config.yaml');
    expect(ConfigRegistry.getFilePath(token)).toBe('/path/to/config.yaml');
  });

  it('should return undefined for unregistered token file path', () => {
    expect(ConfigRegistry.getFilePath(Symbol('unknown'))).toBeUndefined();
  });

  it('should clear all state on reset', () => {
    const resolver = new EnvResolver();
    const token = Symbol('test');
    ConfigRegistry.setResolver(resolver);
    ConfigRegistry.setFilePath(token, '/path');
    ConfigRegistry.reset();
    expect(() => ConfigRegistry.getResolver()).toThrow();
    expect(ConfigRegistry.getFilePath(token)).toBeUndefined();
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm nx test config -- --testPathPattern=config-registry`
Expected: FAIL

- [ ] **Step 3: Implement ConfigRegistry**

Create `libs/config/src/config.registry.ts`:

```typescript
import { IConfigResolver } from './resolvers/config-resolver.interface';

/**
 * Static registry that bridges `ConfigModule.forRoot()` (which creates the resolver)
 * and `ConfigBuilder.build()` factory closures (which consume it).
 *
 * Lifecycle:
 * 1. `ConfigModule.forRoot()` calls `setResolver()` and optionally `setFilePath()`.
 * 2. NestJS DI resolves providers, invoking `registerAs` factories.
 * 3. Each factory calls `getResolver()` — guaranteed to exist at this point.
 *
 * This static singleton is acceptable because configuration is inherently
 * application-global state with a well-defined initialization order.
 */
export class ConfigRegistry {
  private static resolver: IConfigResolver | undefined;
  private static filePaths = new Map<string | symbol, string>();

  /**
   * Registers the active config resolver.
   * Called once by `ConfigModule.forRoot()`.
   * @param resolver - The resolver instance to use for all config resolution.
   */
  public static setResolver(resolver: IConfigResolver): void {
    this.resolver = resolver;
  }

  /**
   * Returns the registered config resolver.
   * @returns The active resolver instance.
   * @throws {Error} If called before `setResolver()` — indicates `ConfigModule.forRoot()`
   *   was not imported or has not executed yet.
   */
  public static getResolver(): IConfigResolver {
    if (!this.resolver) {
      throw new Error(
        'ConfigRegistry: resolver not initialized. Ensure ConfigModule.forRoot() is imported before any config is resolved.',
      );
    }

    return this.resolver;
  }

  /**
   * Registers a YAML file path override for a specific config token.
   * @param token - The config token (string or symbol) from `ConfigBuilder`.
   * @param path - The YAML file path for this config.
   */
  public static setFilePath(token: string | symbol, path: string): void {
    this.filePaths.set(token, path);
  }

  /**
   * Retrieves the YAML file path override for a config token.
   * @param token - The config token to look up.
   * @returns The file path, or `undefined` if no override was registered.
   */
  public static getFilePath(token: string | symbol): string | undefined {
    return this.filePaths.get(token);
  }

  /**
   * Clears all registered state. Intended for testing only.
   * @internal
   */
  public static reset(): void {
    this.resolver = undefined;
    this.filePaths.clear();
  }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm nx test config -- --testPathPattern=config-registry`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add libs/config/src/config.registry.ts libs/config/src/__tests__/config-registry.spec.ts
git commit -m "feat(config): add ConfigRegistry static singleton"
```

---

### Task 6: Refactor ConfigBuilder to lazy evaluation with resolver delegation

**Files:**
- Modify: `libs/config/src/helpers/config-builder.helper.ts`
- Test: `libs/config/src/__tests__/config-builder.spec.ts`

- [ ] **Step 1: Write ConfigBuilder tests**

Create `libs/config/src/__tests__/config-builder.spec.ts`:

```typescript
import 'reflect-metadata';

import { ConfigRegistry } from '../config.registry';
import { Env } from '../decorators/env.decorator';
import { ConfigFormat } from '../enums/config-format.enum';
import { ConfigBuilder } from '../helpers/config-builder.helper';
import { IConfigResolver } from '../resolvers/config-resolver.interface';

const TEST_TOKEN = Symbol('test-config');

class TestConfig {
  @Env('app.host', { default: 'localhost' })
  host!: string;

  @Env('app.port', { type: Number, default: 3000 })
  port!: number;

  @Env('app.debug', { type: Boolean, default: false })
  debug!: boolean;
}

class ArrayConfig {
  @Env('app.origins', { type: Array, default: [] })
  origins!: unknown[];
}

function createMockResolver(
  data: Record<string, unknown>,
  format: ConfigFormat = ConfigFormat.Yaml,
): IConfigResolver {
  return {
    format,
    get: (key: string) => data[key],
    has: (key: string) => key in data,
  };
}

describe('ConfigBuilder', () => {
  afterEach(() => {
    ConfigRegistry.reset();
  });

  describe('lazy evaluation', () => {
    it('should not call initializeConfig at build() time', () => {
      // build() should return a factory without resolving values
      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      expect(typeof factory).toBe('function');
      // No error thrown — resolver not needed yet
    });

    it('should resolve values when factory is invoked', () => {
      ConfigRegistry.setResolver(
        createMockResolver({ 'app.host': 'example.com', 'app.port': 8080, 'app.debug': true }),
      );

      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      const result = factory() as TestConfig;

      expect(result.host).toBe('example.com');
      expect(result.port).toBe(8080);
      expect(result.debug).toBe(true);
    });

    it('should use defaults when resolver returns undefined', () => {
      ConfigRegistry.setResolver(createMockResolver({}));

      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      const result = factory() as TestConfig;

      expect(result.host).toBe('localhost');
      expect(result.port).toBe(3000);
      expect(result.debug).toBe(false);
    });
  });

  describe('dotenv format', () => {
    it('should convert string to number', () => {
      ConfigRegistry.setResolver(
        createMockResolver({ 'app.host': 'h', 'app.port': '8080', 'app.debug': 'false' }, ConfigFormat.Dotenv),
      );

      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      const result = factory() as TestConfig;

      expect(result.port).toBe(8080);
      expect(typeof result.port).toBe('number');
    });
  });

  describe('yaml format — arrays', () => {
    it('should accept array values as-is from yaml resolver', () => {
      ConfigRegistry.setResolver(
        createMockResolver({ 'app.origins': ['https://a.com', 'https://b.com'] }),
      );

      const factory = ConfigBuilder.from(ArrayConfig, Symbol('arr')).build();
      const result = factory() as ArrayConfig;

      expect(result.origins).toEqual(['https://a.com', 'https://b.com']);
    });
  });

  describe('validation', () => {
    it('should run validator on resolved config', () => {
      ConfigRegistry.setResolver(createMockResolver({}));

      const validator = jest.fn((c: TestConfig) => c);
      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).validate(validator).build();
      factory();

      expect(validator).toHaveBeenCalledTimes(1);
    });
  });

  describe('frozen output', () => {
    it('should return a frozen object', () => {
      ConfigRegistry.setResolver(createMockResolver({}));

      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      const result = factory();

      expect(Object.isFrozen(result)).toBe(true);
    });
  });

  describe('file path from registry', () => {
    it('should pass file path from registry to resolver', () => {
      const mockResolver = createMockResolver({});
      const getSpy = jest.spyOn(mockResolver, 'get');

      ConfigRegistry.setResolver(mockResolver);
      ConfigRegistry.setFilePath(TEST_TOKEN, '/custom/path.yaml');

      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      factory();

      // Each @Env field should receive the file path
      expect(getSpy).toHaveBeenCalledWith('app.host', '/custom/path.yaml');
    });
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm nx test config -- --testPathPattern=config-builder.spec`
Expected: FAIL — current ConfigBuilder doesn't use registry

- [ ] **Step 3: Refactor ConfigBuilder**

Rewrite `libs/config/src/helpers/config-builder.helper.ts`. Key changes:

1. `build()` wraps all logic inside `registerAs` factory closure (lazy)
2. `initializeConfig()` accepts `IConfigResolver` and optional `filePath`
3. `convertValue()` only runs for dotenv format
4. Array handling: `type === Array` in dotenv → `JSON.parse()`, in yaml → pass-through
5. Store `configClass` reference on factory function for example generation
6. JSDoc on all public and non-trivial private methods

```typescript
import 'reflect-metadata';
import { Logger, Type } from '@nestjs/common';
import { registerAs } from '@nestjs/config';

import { ConfigRegistry } from '../config.registry';
import { ConfigFormat } from '../enums/config-format.enum';
import { ENV_METADATA_KEY } from '../tokens';
import { EnumType, EnvTypeConstructor, IEnvFieldMetadata } from '../types';
import { ConfigFactory, ConfigFactoryKeyHost } from '../types/config-factory.type';

/** Symbol used to attach the config class reference to a factory function. */
export const CONFIG_CLASS_KEY = Symbol('config-class');

/**
 * Fluent builder for creating typed, validated, frozen configuration objects.
 *
 * Configuration values are resolved lazily — `build()` returns a factory function
 * that is invoked by NestJS during DI resolution. At that point, `ConfigModule.forRoot()`
 * has already registered the appropriate resolver in `ConfigRegistry`.
 *
 * @example
 * ```typescript
 * export const appConfig = ConfigBuilder
 *   .from(AppConfig, APP_CONFIG)
 *   .validate(typia.assertEquals<IAppConfig>)
 *   .build();
 * ```
 */
export class ConfigBuilder<T extends object> {
  private readonly logger = new Logger(ConfigBuilder.name);
  private validator?: (config: T) => T;

  private constructor(
    private readonly configClass: Type<T>,
    private readonly token: string | symbol,
  ) {}

  /**
   * Creates a configuration builder from a configuration class and DI token.
   * @param configClass - Configuration class constructor decorated with `@Env()`.
   * @param token - Unique string or symbol token for dependency injection.
   * @returns A new `ConfigBuilder` instance for chaining.
   */
  public static from<T extends object>(
    configClass: Type<T>,
    token: string | symbol,
  ): ConfigBuilder<T> {
    return new ConfigBuilder(configClass, token);
  }

  /**
   * Builds the configuration factory. Resolution is deferred until the factory
   * is invoked by NestJS during DI provider resolution.
   * @returns A `ConfigFactory` compatible with `@nestjs/config`'s `registerAs`.
   */
  public build(): ConfigFactory & ConfigFactoryKeyHost<T> {
    const factory = registerAs(this.token, () => {
      const resolver = ConfigRegistry.getResolver();
      const filePath = ConfigRegistry.getFilePath(this.token);
      const instance = this.initializeConfig(this.configClass, resolver, filePath);

      try {
        const validated = this.validator ? this.validator(instance) : instance;

        return Object.freeze(validated);
      } catch (error) {
        this.logger.error('Validation failed for configuration object:');
        this.logger.error((error as Error).message);

        process.exit(1);
      }
    });

    // Attach class reference for example generation (pre-DI metadata scan)
    (factory as Record<symbol, unknown>)[CONFIG_CLASS_KEY] = this.configClass;

    return factory;
  }

  /**
   * Adds a validation step. The validator runs after all values are resolved
   * and before the config object is frozen.
   * @param validator - Function that validates and optionally transforms the config.
   *   Should throw on validation failure.
   * @returns This builder instance for chaining.
   */
  public validate(validator: (config: T) => T): ConfigBuilder<T> {
    this.validator = validator;
    return this;
  }

  /**
   * Converts a string value from an environment variable to the target type.
   * Only used in dotenv mode — YAML values already have native types.
   * @param value - The raw string value from `process.env`.
   * @param type - The target type constructor or enum.
   * @returns The converted value.
   * @throws {Error} If the value cannot be converted (e.g. non-numeric string to Number).
   */
  private convertValue(
    value: string,
    type?: EnumType | EnvTypeConstructor,
  ): boolean | number | string | unknown[] {
    if (!type || type === String) return value;

    if (type === Number) {
      const num = Number(value);

      if (isNaN(num)) throw new Error(`Cannot convert "${value}" to number`);

      return num;
    }

    if (type === Boolean) return value === 'true' || value === '1';

    if (type === Array) {
      try {
        const parsed: unknown = JSON.parse(value);

        if (!globalThis.Array.isArray(parsed)) {
          throw new Error(`Expected JSON array, got ${typeof parsed}`);
        }

        return parsed as unknown[];
      } catch (error) {
        throw new Error(
          `Cannot parse array from env var: ${error instanceof Error ? error.message : String(error)}`,
        );
      }
    }

    return value;
  }

  /**
   * Initializes a configuration instance by resolving each `@Env()` decorated field.
   * @param configClass - The config class constructor.
   * @param resolver - The config resolver to read values from.
   * @param filePath - Optional YAML file path override for this config.
   * @returns A fully populated config instance.
   */
  private initializeConfig(
    configClass: new () => T,
    resolver: IConfigResolver,
    filePath?: string,
  ): T {
    const instance = new configClass();
    const metadata: IEnvFieldMetadata[] = Reflect.getMetadata(ENV_METADATA_KEY, instance) ?? [];
    const errors: string[] = [];

    for (const { key, options, propertyKey } of metadata) {
      const rawValue = resolver.get(key, filePath);
      const classValue = Reflect.get(instance, propertyKey) as unknown;

      if (rawValue === undefined) {
        if (options.default !== undefined) {
          Reflect.set(instance, propertyKey, options.default);
          continue;
        }

        if (classValue !== undefined) continue;

        errors.push(`Missing required configuration key: ${key}`);
        continue;
      }

      try {
        // In dotenv mode, raw values are strings that need type conversion.
        // In yaml mode, values are already typed — pass through as-is.
        const value =
          resolver.format === ConfigFormat.Dotenv && typeof rawValue === 'string'
            ? this.convertValue(rawValue, options.type)
            : rawValue;

        Reflect.set(instance, propertyKey, value);
      } catch (error) {
        errors.push(`Invalid value for ${key}: ${(error as Error).message}`);
      }
    }

    if (errors.length > 0) {
      this.logger.error('Configuration initialization failed:');

      errors.forEach((error) => {
        this.logger.error(`- ${error}`);
      });

      process.exit(1);
    }

    return instance;
  }
}
```

Note: Add `import { IConfigResolver } from '../resolvers/config-resolver.interface';` and `import { ConfigFormat } from '../enums/config-format.enum';` at the top.

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm nx test config -- --testPathPattern=config-builder.spec`
Expected: PASS

- [ ] **Step 5: Verify build**

Run: `pnpm nx build config`
Expected: Successful compilation

- [ ] **Step 6: Commit**

```bash
git add libs/config/src/helpers/config-builder.helper.ts libs/config/src/__tests__/config-builder.spec.ts
git commit -m "refactor(config): make ConfigBuilder lazy with resolver delegation"
```

---

### Task 7: Implement example formatters

**Files:**
- Create: `libs/config/src/formatters/example-formatter.interface.ts`
- Create: `libs/config/src/formatters/env-example.formatter.ts`
- Create: `libs/config/src/formatters/yaml-example.formatter.ts`
- Test: `libs/config/src/__tests__/env-example.formatter.spec.ts`
- Test: `libs/config/src/__tests__/yaml-example.formatter.spec.ts`

- [ ] **Step 1: Create IExampleFormatter interface**

Create `libs/config/src/formatters/example-formatter.interface.ts`:

```typescript
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
 * Implementations produce format-specific output (`.env.example`, `.env.example.yaml`).
 */
export interface IExampleFormatter {
  /**
   * Formats config sections into a complete example file string.
   * @param sections - Grouped config metadata.
   * @returns The formatted file content.
   */
  format(sections: IConfigSection[]): string;

  /** The output file name (e.g. `.env.example`, `.env.example.yaml`). */
  readonly fileName: string;
}
```

- [ ] **Step 2: Write EnvExampleFormatter tests**

Create `libs/config/src/__tests__/env-example.formatter.spec.ts`:

```typescript
import { EnvExampleFormatter } from '../formatters/env-example.formatter';
import { IConfigSection } from '../formatters/example-formatter.interface';

describe('EnvExampleFormatter', () => {
  const formatter = new EnvExampleFormatter();

  it('should have .env.example as file name', () => {
    expect(formatter.fileName).toBe('.env.example');
  });

  it('should format a simple section', () => {
    const sections: IConfigSection[] = [
      {
        title: 'app',
        fields: [
          { key: 'APP_PORT', propertyKey: 'port', options: { default: 3000, type: Number } },
          { key: 'APP_NAME', propertyKey: 'name', options: { description: 'Application name' } },
        ],
        instance: { port: 3000, name: 'my-app' },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('# -- app');
    expect(result).toContain('APP_PORT="3000"');
    expect(result).toContain('APP_NAME="my-app"');
    expect(result).toContain('Application name');
  });

  it('should mark required fields', () => {
    const sections: IConfigSection[] = [
      {
        title: 'db',
        fields: [
          { key: 'DB_HOST', propertyKey: 'host', options: {} },
        ],
        instance: { host: undefined },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('REQUIRED');
  });

  it('should include auto-generated header', () => {
    const result = formatter.format([]);
    expect(result).toContain('auto-generated');
  });
});
```

- [ ] **Step 3: Write YamlExampleFormatter tests**

Create `libs/config/src/__tests__/yaml-example.formatter.spec.ts`:

```typescript
import { YamlExampleFormatter } from '../formatters/yaml-example.formatter';
import { IConfigSection } from '../formatters/example-formatter.interface';

describe('YamlExampleFormatter', () => {
  const formatter = new YamlExampleFormatter();

  it('should have .env.example.yaml as file name', () => {
    expect(formatter.fileName).toBe('.env.example.yaml');
  });

  it('should reconstruct nesting from dot-paths', () => {
    const sections: IConfigSection[] = [
      {
        title: 'app',
        fields: [
          { key: 'app.name', propertyKey: 'name', options: { default: 'my-app' } },
          { key: 'app.port', propertyKey: 'port', options: { default: 3000, type: Number } },
        ],
        instance: { name: 'my-app', port: 3000 },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('app:');
    expect(result).toContain('  name: "my-app"');
    expect(result).toContain('  port: 3000');
  });

  it('should handle array examples', () => {
    const sections: IConfigSection[] = [
      {
        title: 'db',
        fields: [
          {
            key: 'database.replicas',
            propertyKey: 'replicas',
            options: {
              type: Array,
              example: [{ host: 'localhost', port: 5432 }],
            },
          },
        ],
        instance: { replicas: [] },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('database:');
    expect(result).toContain('replicas:');
    expect(result).toContain('host:');
  });

  it('should output empty array when no example provided for Array type', () => {
    const sections: IConfigSection[] = [
      {
        title: 'db',
        fields: [
          {
            key: 'database.replicas',
            propertyKey: 'replicas',
            options: { type: Array },
          },
        ],
        instance: { replicas: [] },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('replicas: []');
  });

  it('should include comments for descriptions', () => {
    const sections: IConfigSection[] = [
      {
        title: 'app',
        fields: [
          { key: 'app.name', propertyKey: 'name', options: { default: 'app', description: 'App name' } },
        ],
        instance: { name: 'app' },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('# App name');
  });

  it('should include auto-generated header', () => {
    const result = formatter.format([]);
    expect(result).toContain('auto-generated');
  });
});
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `pnpm nx test config -- --testPathPattern=formatter`
Expected: FAIL

- [ ] **Step 5: Implement EnvExampleFormatter**

Create `libs/config/src/formatters/env-example.formatter.ts`. Extract the formatting logic from the existing `EnvExampleProvider` (lines 134-205 of `env-example.provider.ts`) into this standalone class:

```typescript
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
}
```

- [ ] **Step 6: Implement YamlExampleFormatter**

Create `libs/config/src/formatters/yaml-example.formatter.ts`:

```typescript
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
  /** @inheritdoc */
  public readonly fileName = '.env.example.yaml';

  private static readonly header = `###
#
# This file is auto-generated based on all registered configurations.
# Do not edit it manually.
#
###` as const;

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

      const yamlStr = stringify(tree, { lineWidth: 0 }).trimEnd();
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
   * Priority: `example` → `default` → instance fallback → empty string / empty array.
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
   */
  private setByDotPath(obj: Record<string, unknown>, dotPath: string, value: unknown): void {
    const segments = dotPath.split('.');
    let current: Record<string, unknown> = obj;

    for (let i = 0; i < segments.length - 1; i++) {
      const seg = segments[i]!;

      if (!(seg in current) || typeof current[seg] !== 'object' || current[seg] === null) {
        current[seg] = {};
      }

      current = current[seg] as Record<string, unknown>;
    }

    current[segments[segments.length - 1]!] = value;
  }

  /**
   * Adds inline comments to YAML output for fields that have descriptions.
   * Matches the first occurrence of each leaf key in the YAML string.
   */
  private addFieldComments(yamlStr: string, fields: IEnvFieldMetadata[]): string {
    let result = yamlStr;

    for (const { key, options } of fields) {
      const comment = options.description ?? options.comment;

      if (!comment) continue;

      const leafKey = key.split('.').pop()!;
      // Add comment after the first line containing this key
      result = result.replace(
        new RegExp(`^(\\s*${leafKey}:.*)$`, 'm'),
        `$1 # ${comment}`,
      );
    }

    return result;
  }
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `pnpm nx test config -- --testPathPattern=formatter`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add libs/config/src/formatters/ libs/config/src/__tests__/*formatter*
git commit -m "feat(config): add EnvExampleFormatter and YamlExampleFormatter"
```

---

### Task 8: Refactor ConfigModule and EnvExampleProvider

**Files:**
- Modify: `libs/config/src/config.module.ts`
- Modify: `libs/config/src/providers/env-example.provider.ts`
- Modify: `libs/config/src/index.ts`
- Test: `libs/config/src/__tests__/config-module.integration.spec.ts`

- [ ] **Step 1: Rewrite ConfigModule**

Rewrite `libs/config/src/config.module.ts` with new `forRoot`/`forFeature` signatures:

```typescript
import { createHash } from 'node:crypto';
import { existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import * as fs from 'node:fs';

import { DynamicModule, Logger, Module } from '@nestjs/common';
import { ConfigFactory, ConfigModule as BaseConfigModule } from '@nestjs/config';

import { ConfigRegistry } from './config.registry';
import { ConfigFormat } from './enums/config-format.enum';
import { EnvExampleFormatter } from './formatters/env-example.formatter';
import { IExampleFormatter } from './formatters/example-formatter.interface';
import { YamlExampleFormatter } from './formatters/yaml-example.formatter';
import { CONFIG_CLASS_KEY } from './helpers/config-builder.helper';
import { EnvExampleProvider } from './providers/env-example.provider';
import { ConfigResolverFactory } from './resolvers/config-resolver.factory';
import { ENV_METADATA_KEY } from './tokens';
import { IEnvFieldMetadata } from './types';
import { IConfigFeatureOptions } from './types/config-feature-options.interface';
import { IConfigLoadItem } from './types/config-load-item.interface';
import { IConfigModuleOptions } from './types/config-module-options.interface';

/**
 * NestJS dynamic module for typed configuration management.
 *
 * Supports two formats:
 * - `dotenv` (default) — reads from `process.env`
 * - `yaml` — reads from YAML files with dot-path notation
 *
 * @example
 * ```typescript
 * // Dotenv mode (default)
 * ConfigModule.forRoot({ load: [appConfig] })
 *
 * // YAML mode
 * ConfigModule.forRoot({
 *   format: 'yaml',
 *   path: 'config/app.yaml',
 *   load: [appConfig, { path: 'config/db.yaml', config: dbConfig }],
 * })
 * ```
 */
@Module({})
export class ConfigModule {
  private static readonly logger = new Logger(ConfigModule.name);

  /**
   * Registers configuration globally with the specified format and sources.
   * @param options - Module options including format, default path, and config factories.
   * @returns A global dynamic module.
   */
  public static forRoot(options: IConfigModuleOptions = {}): DynamicModule {
    const { format = ConfigFormat.Dotenv, path, load = [] } = options;

    // 1. Create and register resolver
    const resolver = ConfigResolverFactory.create(format, path);
    ConfigRegistry.setResolver(resolver);

    // 2. Process load items — extract factories and register file paths
    const factories: ConfigFactory[] = [];

    for (const item of load) {
      if (this.isConfigLoadItem(item)) {
        ConfigRegistry.setFilePath(this.extractToken(item.config), item.path);
        factories.push(item.config);
      } else {
        factories.push(item);
      }
    }

    // 3. Generate example file (pre-DI, sync)
    this.generateExample(format, load);

    // 4. Delegate to @nestjs/config
    return {
      module: ConfigModule,
      global: true,
      imports: [
        BaseConfigModule.forRoot({
          cache: true,
          isGlobal: true,
          expandVariables: format === ConfigFormat.Dotenv,
          load: factories,
        }),
      ],
      providers: [EnvExampleProvider],
      exports: [BaseConfigModule],
    };
  }

  /**
   * Registers a feature-specific configuration.
   * @param configOrOptions - A bare `ConfigFactory` or options with path override.
   * @returns A dynamic module for the feature.
   */
  public static forFeature(configOrOptions: ConfigFactory | IConfigFeatureOptions): DynamicModule {
    let config: ConfigFactory;

    if (typeof configOrOptions === 'function') {
      config = configOrOptions;
    } else {
      config = configOrOptions.config;

      if (configOrOptions.path) {
        ConfigRegistry.setFilePath(this.extractToken(config), configOrOptions.path);
      }
    }

    return {
      module: ConfigModule,
      imports: [BaseConfigModule.forFeature(config)],
      exports: [BaseConfigModule],
    };
  }

  /**
   * Type guard for `IConfigLoadItem` vs bare `ConfigFactory`.
   */
  private static isConfigLoadItem(
    item: ConfigFactory | IConfigLoadItem,
  ): item is IConfigLoadItem {
    return typeof item === 'object' && 'config' in item && 'path' in item;
  }

  /**
   * Extracts the config token from a `ConfigFactory`.
   * `registerAs` attaches the token as `KEY` property on the factory function.
   */
  private static extractToken(factory: ConfigFactory): string | symbol {
    return (factory as unknown as { KEY: string | symbol }).KEY;
  }

  /**
   * Generates an example configuration file synchronously before DI resolution.
   * Errors are caught and logged — they never block app startup.
   */
  private static generateExample(
    format: ConfigFormat,
    load: Array<ConfigFactory | IConfigLoadItem>,
  ): void {
    try {
      const formatter: IExampleFormatter =
        format === ConfigFormat.Yaml
          ? new YamlExampleFormatter()
          : new EnvExampleFormatter();

      const sections = this.collectSections(load);

      if (sections.length === 0) return;

      const content = formatter.format(sections);
      const outputPath = join(process.cwd(), formatter.fileName);

      // Write only if content changed (SHA-256 comparison)
      if (existsSync(outputPath)) {
        const existing = fs.readFileSync(outputPath, 'utf8');
        const existingHash = createHash('sha256').update(existing).digest('hex');
        const newHash = createHash('sha256').update(content).digest('hex');

        if (existingHash === newHash) return;
      }

      const dir = dirname(outputPath);

      if (!existsSync(dir)) {
        fs.mkdirSync(dir, { recursive: true });
      }

      fs.writeFileSync(outputPath, content, 'utf8');
      this.logger.log(`Example config generated: ${outputPath}`);
    } catch (error) {
      this.logger.warn(
        `Failed to generate example config: ${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }

  /**
   * Collects config sections by scanning `@Env()` metadata from config classes.
   * Uses the `CONFIG_CLASS_KEY` symbol attached by `ConfigBuilder.build()`.
   */
  private static collectSections(
    load: Array<ConfigFactory | IConfigLoadItem>,
  ): Array<{ title: string; fields: IEnvFieldMetadata[]; instance: Record<string, unknown> }> {
    const sections: Array<{
      title: string;
      fields: IEnvFieldMetadata[];
      instance: Record<string, unknown>;
    }> = [];

    for (const item of load) {
      const factory = this.isConfigLoadItem(item) ? item.config : item;
      const configClass = (factory as Record<symbol, unknown>)[CONFIG_CLASS_KEY] as
        | (new () => unknown)
        | undefined;

      if (!configClass) {
        this.logger.debug(
          'Config class not found on factory — skipping example generation for this config.',
        );
        continue;
      }

      const instance = new configClass() as Record<string, unknown>;
      const fields: IEnvFieldMetadata[] =
        Reflect.getMetadata(ENV_METADATA_KEY, instance) ?? [];

      if (fields.length === 0) continue;

      const token = this.extractToken(factory);
      const title = typeof token === 'symbol' ? (token.description ?? 'unknown') : token;

      sections.push({ title, fields, instance });
    }

    return sections;
  }
}
```

- [ ] **Step 2: Simplify EnvExampleProvider**

Refactor `libs/config/src/providers/env-example.provider.ts` to become a thin wrapper. The provider still runs in `OnModuleInit` as a secondary pass with runtime values, but delegates formatting to `EnvExampleFormatter`. Since the main example generation now happens in `forRoot()`, this provider can be simplified significantly. Keep the existing behavior for backward compat in dotenv mode — it enhances the baseline example with resolved runtime values.

Key changes:
- Remove the formatting methods (moved to `EnvExampleFormatter`)
- Import and use `EnvExampleFormatter` for output formatting
- Keep `resolveAppRoot`, `writeIfChanged$`, and the RxJS pipeline
- Keep `generateEnvironmentExample` but use formatter for output

- [ ] **Step 3: Update index.ts exports**

Add to `libs/config/src/index.ts`:

```typescript
export * from './config.registry';
export * from './resolvers/config-resolver.interface';
export * from './resolvers/env.resolver';
export * from './resolvers/yaml.resolver';
export * from './resolvers/config-resolver.factory';
export * from './formatters/example-formatter.interface';
export * from './formatters/env-example.formatter';
export * from './formatters/yaml-example.formatter';
```

- [ ] **Step 4: Verify build**

Run: `pnpm nx build config`
Expected: Successful compilation

- [ ] **Step 5: Verify lint**

Run: `pnpm nx lint config`
Expected: PASS (fix any issues)

- [ ] **Step 6: Write integration test**

Create `libs/config/src/__tests__/config-module.integration.spec.ts`:

```typescript
import 'reflect-metadata';
import { join } from 'node:path';
import { existsSync, unlinkSync } from 'node:fs';

import { Test } from '@nestjs/testing';
import { ConfigService } from '@nestjs/config';

import { ConfigModule } from '../config.module';
import { ConfigRegistry } from '../config.registry';
import { ConfigBuilder } from '../helpers/config-builder.helper';
import { Env } from '../decorators/env.decorator';
import { ConfigFormat } from '../enums/config-format.enum';

const FIXTURES = join(__dirname, 'fixtures');
const YAML_PATH = join(FIXTURES, 'valid.yaml');
const TEST_TOKEN = Symbol('integration-test');

class YamlTestConfig {
  @Env('app.name')
  name!: string;

  @Env('app.port', { type: Number })
  port!: number;
}

const yamlTestConfig = ConfigBuilder.from(YamlTestConfig, TEST_TOKEN).build();

describe('ConfigModule integration', () => {
  afterEach(() => {
    ConfigRegistry.reset();
    // Clean up example files if created
    const envExample = join(process.cwd(), '.env.example');
    const yamlExample = join(process.cwd(), '.env.example.yaml');
    if (existsSync(envExample)) unlinkSync(envExample);
    if (existsSync(yamlExample)) unlinkSync(yamlExample);
  });

  it('should resolve YAML config values via ConfigService', async () => {
    const module = await Test.createTestingModule({
      imports: [
        ConfigModule.forRoot({
          format: ConfigFormat.Yaml,
          path: YAML_PATH,
          load: [yamlTestConfig],
        }),
      ],
    }).compile();

    const configService = module.get(ConfigService);
    const config = configService.get<YamlTestConfig>(TEST_TOKEN);

    expect(config).toBeDefined();
    expect(config!.name).toBe('test-app');
    expect(config!.port).toBe(3000);
  });

  it('should work in dotenv mode (backward compat)', async () => {
    process.env['COMPAT_KEY'] = 'compat-value';

    class DotenvTestConfig {
      @Env('COMPAT_KEY')
      key!: string;
    }

    const TOKEN = Symbol('compat-test');
    const dotenvConfig = ConfigBuilder.from(DotenvTestConfig, TOKEN).build();

    const module = await Test.createTestingModule({
      imports: [
        ConfigModule.forRoot({ load: [dotenvConfig] }),
      ],
    }).compile();

    const configService = module.get(ConfigService);
    const config = configService.get<DotenvTestConfig>(TOKEN);

    expect(config).toBeDefined();
    expect(config!.key).toBe('compat-value');

    delete process.env['COMPAT_KEY'];
  });
});
```

- [ ] **Step 7: Run all tests**

Run: `pnpm nx test config`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add libs/config/src/
git commit -m "feat(config): add YAML support to ConfigModule with pre-DI example generation"
```

---

### Task 9: Update consumers, final cleanup, and full verification

**Files:**
- Modify: `libs/kernel/src/kernel.module.ts`
- Modify: `libs/config/src/index.ts` (final review)
- Modify: `libs/config/package.json` (verify yaml dep)

- [ ] **Step 1: Update KernelModule.forServe to new forRoot signature**

The current call passes an array directly:
```typescript
imports: [ConfigModule.forRoot([appConfig]), appModule],
```
Update to object signature:
```typescript
imports: [ConfigModule.forRoot({ load: [appConfig] }), appModule],
```

- [ ] **Step 2: Verify all exports in index.ts**

Read `libs/config/src/index.ts` and ensure all new public APIs are exported.

- [ ] **Step 3: Full build**

Run: `pnpm nx build config`
Expected: PASS

- [ ] **Step 4: Full lint**

Run: `pnpm nx lint config --fix`
Expected: PASS

- [ ] **Step 5: Full test suite**

Run: `pnpm nx test config`
Expected: ALL PASS

- [ ] **Step 6: Build entire monorepo to verify no breakage**

Run: `pnpm nx run-many -t build`
Expected: ALL projects PASS.

- [ ] **Step 7: Lint entire monorepo**

Run: `pnpm nx affected -t lint --fix`
Expected: PASS

- [ ] **Step 8: Final commit**

```bash
git add -A
git commit -m "chore(config): finalize YAML config support — build and lint clean"
```

- [ ] **Step 9: Push branch**

```bash
git push origin feature/config-yaml
```
