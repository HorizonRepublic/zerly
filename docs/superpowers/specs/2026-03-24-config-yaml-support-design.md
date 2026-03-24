# Design: YAML Config Support for @zerly/config

**Date:** 2026-03-24
**Status:** Approved
**Scope:** `libs/config`

## Overview

Extend `@zerly/config` to support YAML files as a configuration source alongside the existing dotenv/`process.env` approach. The format is selected once at module level — no mixing allowed. YAML enables structured data (arrays of objects, nested configs) that flat env vars cannot express cleanly.

## Requirements

1. YAML files as config source with full structured data (arrays, nested objects)
2. Single format per application — `'dotenv' | 'yaml'`, set in `forRoot()`
3. `forRoot` accepts an optional default YAML path; individual configs can override with their own path
4. `forFeature` accepts a specific YAML file path for a specific config (useful for AIO modules)
5. `@Env()` decorator unchanged — key interpretation depends on format
6. Backward compatible — without `format` option, behaves exactly as before (`'dotenv'`)
7. Validation remains user-controlled via `.validate()` (typia, zod, custom — anything)
8. Example file auto-generation for both formats (`.env.example` / `.env.example.yaml`)
9. Example generation runs before DI resolution so the file exists even if the app crashes on config validation
10. No hot reload — read-once at startup. Architecture should not preclude adding it later.
11. OSS-quality JSDoc documentation on all public APIs, interfaces, types, and non-trivial internal methods.

## Public API

### ConfigModule.forRoot

```typescript
// Dotenv mode (default, backward compatible)
ConfigModule.forRoot({
  load: [appConfig],
});

// YAML mode with default path
ConfigModule.forRoot({
  format: 'yaml',
  path: 'config/app.yaml',
  load: [appConfig, cacheConfig],
});

// YAML mode without default path — each config specifies its own
ConfigModule.forRoot({
  format: 'yaml',
  load: [
    { path: 'config/app.yaml', config: appConfig },
    { path: 'config/db.yaml', config: dbConfig },
  ],
});

// YAML mode — mix of default and per-config paths
ConfigModule.forRoot({
  format: 'yaml',
  path: 'config/app.yaml',
  load: [
    appConfig,                                      // uses default path
    { path: 'config/db.yaml', config: dbConfig },   // own path
  ],
});
```

### ConfigModule.forFeature

```typescript
// YAML — with own path (inherits format from forRoot)
ConfigModule.forFeature({
  path: 'config/notifications.yaml',
  config: notificationsConfig,
});

// Dotenv — backward compatible
ConfigModule.forFeature(someConfig);
```

### @Env() decorator

No API changes. Key interpretation depends on the module-level format:

```typescript
// format: 'dotenv' → reads process.env.APP_PORT
@Env('APP_PORT', { type: Number, default: 3000 })
port!: number;

// format: 'yaml' → reads parsed.database.port via dot-path
@Env('database.port', { type: Number, default: 5432 })
port!: number;

// format: 'yaml' — arrays
@Env('database.replicas', {
  type: Array,
  example: [{ host: 'localhost', port: 5432 }],
})
replicas!: IReplica[];
```

### ConfigBuilder

No external API changes:

```typescript
export const dbConfig = ConfigBuilder
  .from(DbConfig, DB_CONFIG)
  .validate(typia.assertEquals<IDbConfig>)
  .build();
```

## Architecture

### Critical: Lazy Evaluation in ConfigBuilder

The current `ConfigBuilder.build()` calls `initializeConfig()` eagerly at import time — before `forRoot()` runs and before any resolver exists. This must change to lazy evaluation.

**Current (broken for resolver injection):**
```typescript
public build(): ConfigFactory & ConfigFactoryKeyHost<T> {
  const instance = this.initializeConfig(this.configClass); // ← runs at import time
  const finalConfig = this.validator ? this.validator(instance) : instance;
  return registerAs(this.token, () => Object.freeze(finalConfig));
}
```

**New (lazy — resolution deferred to factory invocation):**
```typescript
public build(): ConfigFactory & ConfigFactoryKeyHost<T> {
  return registerAs(this.token, () => {
    const resolver = ConfigRegistry.getResolver();
    const filePath = ConfigRegistry.getFilePath(this.token);
    const instance = this.initializeConfig(this.configClass, resolver, filePath);
    const validated = this.validator ? this.validator(instance) : instance;
    return Object.freeze(validated);
  });
}
```

`registerAs` from `@nestjs/config` already accepts a factory function. The factory is invoked by NestJS during DI resolution — at which point `forRoot()` has already run and the resolver is registered in `ConfigRegistry`. This preserves the external API (`ConfigBuilder.from().validate().build()`) while enabling resolver injection.

### ConfigRegistry (static singleton)

A lightweight static registry that bridges the gap between `ConfigModule.forRoot()` (which sets up the resolver) and `ConfigBuilder.build()` factories (which consume it):

```typescript
class ConfigRegistry {
  private static resolver: IConfigResolver;
  private static filePaths: Map<string | symbol, string>;

  static setResolver(resolver: IConfigResolver): void;
  static getResolver(): IConfigResolver;
  static setFilePath(token: string | symbol, path: string): void;
  static getFilePath(token: string | symbol): string | undefined;
}
```

**Lifecycle:**
1. `ConfigModule.forRoot()` calls `ConfigRegistry.setResolver()` and `ConfigRegistry.setFilePath()` for each config with a path override
2. Later, when NestJS resolves DI providers, `registerAs` factories execute
3. Each factory calls `ConfigRegistry.getResolver()` — resolver is guaranteed to exist at this point
4. If `getResolver()` is called before `setResolver()` → throw with clear error message

### Approach: ConfigResolver Abstraction

A `ConfigResolver` interface with two implementations (`EnvResolver`, `YamlResolver`). `ConfigBuilder.initializeConfig()` delegates to the resolver instead of accessing `process.env` directly.

### Components

```
ConfigRegistry (static)        — bridges forRoot() and ConfigBuilder factories
ConfigResolver (interface)
├── EnvResolver                — reads process.env[key], converts types (existing logic)
├── YamlResolver               — parses YAML file, resolves dot-path, caches per file
└── ConfigResolverFactory      — creates the appropriate resolver based on format
```

### IConfigResolver Interface

```typescript
interface IConfigResolver {
  /**
   * Retrieves a configuration value by key.
   * For EnvResolver: key is an env var name (e.g. 'APP_PORT').
   * For YamlResolver: key is a dot-path (e.g. 'database.port').
   * @param key - The configuration key to resolve.
   * @param filePath - Optional file path override (YAML only, ignored by EnvResolver).
   * @returns The resolved value, or undefined if not found.
   */
  get(key: string, filePath?: string): unknown;

  /**
   * Checks whether a configuration key exists in the source.
   * @param key - The configuration key to check.
   * @param filePath - Optional file path override (YAML only).
   */
  has(key: string, filePath?: string): boolean;

  /** The format this resolver handles. */
  readonly format: ConfigFormat;
}
```

`filePath` is optional on the interface — `EnvResolver` ignores it, `YamlResolver` uses it to select a file (falling back to default path).

- `EnvResolver.get('APP_PORT')` → `process.env['APP_PORT']` (string) → ConfigBuilder converts via `type`
- `YamlResolver.get('database.replicas')` → returns typed value from parsed YAML (number, string, array, object). Type conversion is skipped — YAML has native types.
- `YamlResolver.get('database.host', 'config/db.yaml')` → reads from specific file instead of default

### Resolution Flow

```
ConfigModule.forRoot({ format, path, load })
  │
  ├─ 1. Example generation (sync, before DI)
  │    ├─ For each config class in load:
  │    │    new ConfigClass() → Reflect.getMetadata() → collect @Env() fields
  │    ├─ Format with EnvExampleFormatter or YamlExampleFormatter
  │    └─ Write .env.example or .env.example.yaml (warn on failure, don't crash)
  │
  ├─ 2. ConfigResolverFactory.create(format, defaultPath)
  │    └─ EnvResolver | YamlResolver → ConfigRegistry.setResolver()
  │
  ├─ 3. Register file path overrides in ConfigRegistry
  │    └─ For each { path, config } in load → ConfigRegistry.setFilePath(token, path)
  │
  ├─ 4. Extract ConfigFactory[] from load items
  │    └─ Bare ConfigFactory passed through; { path, config } unwrapped to config
  │
  └─ 5. Delegate to @nestjs/config BaseConfigModule.forRoot({ load: factories })
       └─ NestJS DI resolves factories → each calls ConfigRegistry.getResolver()
          → initializeConfig(class, resolver, filePath) → validate → freeze
```

### Integration with @nestjs/config

`@nestjs/config`'s `BaseConfigModule` remains the underlying DI integration. `ConfigModule.forRoot()` preprocesses the `load` array:
- Bare `ConfigFactory` items pass through unchanged
- `{ path, config }` items: path is registered in `ConfigRegistry`, `config` (the `ConfigFactory`) is extracted

The final `ConfigFactory[]` array is passed to `BaseConfigModule.forRoot({ load })` as before. `ConfigService.get<T>(TOKEN)` continues to work unchanged for consumers.

### YamlResolver Details

- Accepts default file path at creation
- Caches parsed YAML content per file path (Map<string, unknown>)
- Dot-path resolution: splits key by `.`, traverses parsed object tree
- `'database.replicas'` → `parsed['database']['replicas']`
- Uses `yaml` npm package (`parse()` function) for parsing
- Reads files synchronously at first access (`readFileSync`) — acceptable for startup-only config loading

### ConfigBuilder Changes

- `build()` becomes lazy: `initializeConfig()` moves inside the `registerAs` factory closure
- `initializeConfig()` accepts `IConfigResolver` and optional `filePath`
- `convertValue()` is called only when resolver format is `'dotenv'` (string → primitive)
- For `'yaml'` format — value is returned as-is (YAML already has native types)
- Error accumulation and `process.exit(1)` behavior unchanged

### Type System Changes for Array Support

`EnvTypeConstructor` expands to include `typeof Array`:

```typescript
export type EnvTypeConstructor = typeof Boolean | typeof Number | typeof String | typeof Array;
```

`InferTypeFromConstructor` gains an Array branch:

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

For `type: Array`:
- **dotenv mode**: `ConfigBuilder` attempts `JSON.parse()` on the env var string; throws if invalid JSON
- **yaml mode**: value returned as-is from resolver (already a JS array)
- `example` and `default` accept `unknown[]`

### Example Generation

Runs in `ConfigModule.forRoot()` **synchronously, before returning the DynamicModule**. This ensures the example file exists even if the app crashes during DI/config validation.

Generation does not require DI — only `@Env()` metadata:
1. For each config in `load`, extract the config class (from `ConfigFactory` or `{ config }`)
2. `new ConfigClass()` → `Reflect.getMetadata(ENV_METADATA_KEY, instance)` → field metadata
3. Format with appropriate formatter and write to disk

**Extracting config class from ConfigFactory:** `ConfigBuilder` stores the class reference on the factory function (e.g. `factory.__configClass = configClass`). `forRoot()` reads it to instantiate for metadata scanning. If not available (third-party factory), example generation skips that config with a debug log.

**Formatters:**
- `EnvExampleFormatter` — existing `.env.example` logic extracted from `EnvExampleProvider`
- `YamlExampleFormatter` — generates `.env.example.yaml`, reconstructs nesting from dot-path keys

**EnvExampleProvider** is refactored: formatting logic moves to formatters, the provider becomes a thin wrapper that delegates. In dotenv mode it still runs in `OnModuleInit` as a secondary pass (with runtime values for richer fallbacks). The pre-DI generation in `forRoot()` produces a baseline example; the `OnModuleInit` pass can enhance it with resolved values if the app starts successfully.

**Array handling in YAML example:**
```typescript
@Env('database.replicas', {
  type: Array,
  example: [{ host: 'localhost', port: 5432 }],
})
```
Outputs:
```yaml
database:
  replicas:
    - host: "localhost"
      port: 5432
```
If no `example` provided → `replicas: []`

## Types

### IConfigModuleOptions

```typescript
interface IConfigModuleOptions {
  /** Config format. Determines how @Env() keys are interpreted. Default: 'dotenv'. */
  format?: ConfigFormat;

  /** Default YAML file path. Used by configs that don't specify their own path. */
  path?: string;

  /** Config factories to load. Accepts bare ConfigFactory or { path, config } objects. */
  load?: Array<ConfigFactory | IConfigLoadItem>;
}
```

### IConfigLoadItem

```typescript
interface IConfigLoadItem {
  /** Path to the YAML file for this specific config. */
  path: string;

  /** The ConfigFactory returned by ConfigBuilder.build(). */
  config: ConfigFactory;
}
```

### IConfigFeatureOptions

```typescript
interface IConfigFeatureOptions {
  /** Path to the YAML file for this feature config. */
  path?: string;

  /** The ConfigFactory returned by ConfigBuilder.build(). */
  config: ConfigFactory;
}
```

## Error Handling

### Fail-fast (process.exit(1))

- `format: 'yaml'`, config has no path, `forRoot` has no default path → `"No YAML path for config 'TOKEN'. Set path in forRoot or provide per-config path."`
- YAML file not found on disk → `"YAML config file not found: config/db.yaml"`
- YAML syntax error → `"Failed to parse YAML file config/db.yaml: <parser error>"`
- Dot-path not found in parsed YAML → same as current env behavior: fallback to default, if no default → accumulate error → exit(1)
- `format: 'dotenv'` + `type: Array` → attempt `JSON.parse()`, if invalid JSON → error
- `ConfigRegistry.getResolver()` called before `setResolver()` → `"ConfigRegistry: resolver not initialized. Ensure ConfigModule.forRoot() is imported before any config is resolved."`

### Warnings (non-fatal)

- YAML file exists but is empty → warn, all fields fall back to defaults
- `type: Number/Boolean` specified for YAML field → ignored (YAML is already typed), warn in dev
- Example generation failure → warn, app continues startup
- Config class not extractable from factory (third-party) → debug log, skip example for that config

## File Structure

```
libs/config/src/
├── config.module.ts                          # CHANGED — format, path, new forRoot/forFeature signature
├── config.registry.ts                        # NEW — static ConfigRegistry singleton
├── decorators/
│   └── env.decorator.ts                      # MINOR — Array type support in IEnvOptions
├── enums/
│   ├── index.ts
│   ├── kernel.enum.ts
│   └── config-format.enum.ts                 # NEW — ConfigFormat enum
├── helpers/
│   └── config-builder.helper.ts              # CHANGED — lazy build(), delegates to IConfigResolver
├── providers/
│   └── env-example.provider.ts               # CHANGED — delegates to formatter, thin wrapper
├── resolvers/                                # NEW directory
│   ├── config-resolver.interface.ts          # IConfigResolver interface
│   ├── env.resolver.ts                       # EnvResolver (logic extracted from ConfigBuilder)
│   ├── yaml.resolver.ts                      # YamlResolver (parsing + dot-path + cache)
│   └── config-resolver.factory.ts            # Factory by format
├── formatters/                               # NEW directory
│   ├── example-formatter.interface.ts        # IExampleFormatter interface
│   ├── env-example.formatter.ts              # Existing logic extracted from EnvExampleProvider
│   └── yaml-example.formatter.ts             # YAML example generation with nesting
├── tokens/
│   └── index.ts                              # CHANGED — CONFIG_RESOLVER token
├── types/
│   ├── index.ts                              # CHANGED — Array in EnvTypeConstructor, InferTypeFromConstructor
│   ├── app-config.interface.ts
│   ├── config-factory.type.ts
│   ├── config-module-options.interface.ts     # NEW — IConfigModuleOptions
│   ├── config-load-item.interface.ts          # NEW — IConfigLoadItem
│   └── config-feature-options.interface.ts    # NEW — IConfigFeatureOptions
└── index.ts                                  # CHANGED — new exports
```

### Dependencies

- `yaml` — new dependency in `libs/config/package.json`

## Testing

Unit tests (jest):
- `ConfigRegistry` — set/get resolver, set/get file paths, error on uninitialized access
- `YamlResolver` — parsing, dot-path resolution, caching, file override, edge cases (empty file, missing key, invalid YAML)
- `EnvResolver` — type conversion for String/Number/Boolean/Array, missing keys, JSON.parse for arrays
- `ConfigResolverFactory` — returns correct resolver by format
- `ConfigBuilder` — lazy evaluation, both formats with mock resolver, arrays, objects, scalars, defaults, validation
- `YamlExampleFormatter` — YAML example generation, nesting reconstruction, arrays, comments
- `EnvExampleFormatter` — existing logic extracted from EnvExampleProvider

Integration tests:
- Full flow: `ConfigModule.forRoot({ format: 'yaml' })` → parse → resolve → validate → frozen config accessible via `ConfigService.get()`
- `forRoot` + `forFeature` with different paths
- Backward compatibility: `ConfigModule.forRoot({ load: [appConfig] })` works identically to current behavior
- Example generation for both formats — file exists on disk after `forRoot()`
- Fail-fast scenarios — missing file, missing path, invalid YAML, uninitialized registry

## Documentation Standards

All public APIs, interfaces, types, classes, and non-trivial methods must have JSDoc documentation:
- `@description` for classes and interfaces
- `@param` for all parameters
- `@returns` for non-void methods
- `@throws` for methods that throw
- `@example` for public API methods where usage is non-obvious
- Follow existing JSDoc style in the codebase (see `config-builder.helper.ts`, `env-example.provider.ts`)

## Design Decisions

| Decision | Rationale |
|---|---|
| Single format per app | Prevents confusing mixed-source configs; simplifies mental model |
| `@Env()` stays as single decorator | Source-agnostic — key interpretation depends on module-level format |
| ConfigResolver abstraction | Clean SRP, testable, extensible for future formats |
| ConfigRegistry static singleton | Bridges static `ConfigBuilder.build()` factories with runtime `forRoot()` config. Acceptable for config — it is inherently app-global state. |
| Lazy `build()` via registerAs factory | Defers `initializeConfig()` until DI resolution, when resolver is available. Preserves external API. |
| Example generation pre-DI | Example file must exist even if app crashes on validation |
| `yaml` npm package | Actively maintained, YAML 1.2 compliant, good TypeScript support |
| No hot reload | Proper DI hot reload is unsolved industry-wide; read-once is sufficient. Architecture does not preclude adding it later. |
| Arrays from env via JSON.parse | User's choice to use ugly format; we parse it, validation catches bad input |
| `type: Array` for array fields | Explicit declaration, `example` provides template for example generation |
| `filePath` optional on IConfigResolver.get | EnvResolver ignores it; YamlResolver uses it for per-config file override. Single interface, no branching at call site. |
| @nestjs/config BaseConfigModule retained | Consumers still use `ConfigService.get<T>(TOKEN)`. We preprocess `load` array but delegate DI registration to the established library. |
