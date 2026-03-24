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

### Approach: ConfigResolver Abstraction

A `ConfigResolver` interface with two implementations (`EnvResolver`, `YamlResolver`). `ConfigBuilder.initializeConfig()` delegates to the resolver instead of accessing `process.env` directly.

### Components

```
ConfigResolver (interface)
├── EnvResolver        — reads process.env[key], converts types (existing logic)
├── YamlResolver       — parses YAML file, resolves dot-path, caches per file
└── ConfigResolverFactory — creates the appropriate resolver based on format
```

### IConfigResolver Interface

```typescript
interface IConfigResolver {
  get(key: string): unknown;
  has(key: string): boolean;
  readonly format: ConfigFormat;
}
```

- `EnvResolver.get('APP_PORT')` → `process.env['APP_PORT']` (string) → ConfigBuilder converts via `type`
- `YamlResolver.get('database.replicas')` → returns typed value from parsed YAML (number, string, array, object). Type conversion is skipped — YAML has native types.

### Resolution Flow

```
ConfigModule.forRoot({ format, path, load })
  │
  ├─ 1. Example generation (pre-DI, sync)
  │    └─ Scan @Env() metadata from config classes
  │    └─ Write .env.example or .env.example.yaml
  │
  ├─ 2. ConfigResolverFactory.create(format)
  │    └─ EnvResolver | YamlResolver (registered as global provider)
  │
  └─ 3. For each config in load:
       ConfigBuilder.initializeConfig(resolver, filePath?)
         ├─ Read @Env() metadata
         ├─ For each field: resolver.get(key)
         │   ├─ EnvResolver: string → type conversion
         │   └─ YamlResolver: returns as-is
         ├─ Fallback: default → class value → accumulate error
         ├─ Validate (if .validate() was called)
         └─ Object.freeze() → registerAs(token)
```

### YamlResolver Details

- Accepts default file path at creation
- `get(key, overridePath?)` — uses override path if provided, otherwise default
- Caches parsed result per file path (read-once)
- Dot-path resolution: `'database.replicas'` → `parsed['database']['replicas']`
- Uses `yaml` npm package for parsing

### ConfigBuilder Changes

- `initializeConfig()` accepts `IConfigResolver` instead of direct `process.env` access
- `convertValue()` is called only for `EnvResolver` (string → primitive)
- For `YamlResolver` — value is returned as-is (YAML already has types)

### Example Generation

Runs in `ConfigModule.forRoot()` **before DI resolution**. This ensures the example file exists even if the app crashes during config validation.

Generation does not require DI:
1. Instantiate config class with `new ConfigClass()`
2. Read `@Env()` metadata via `Reflect.getMetadata()`
3. Format and write file

**Formatters:**
- `EnvExampleFormatter` — existing `.env.example` logic extracted from `EnvExampleProvider`
- `YamlExampleFormatter` — generates `.env.example.yaml`, reconstructs nesting from dot-path keys

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

## Error Handling

### Fail-fast (process.exit(1))

- `format: 'yaml'`, config has no path, `forRoot` has no default path → clear error message
- YAML file not found on disk → `"YAML config file not found: config/db.yaml"`
- YAML syntax error → `"Failed to parse YAML file config/db.yaml: <parser error>"`
- Dot-path not found in parsed YAML → same as current env behavior: fallback to default, if no default → accumulate error → exit(1)
- `format: 'dotenv'` + array value in env → attempt `JSON.parse()`, if invalid JSON → error

### Warnings (non-fatal)

- YAML file exists but is empty → warn, all fields fall back to defaults
- `type: Number/Boolean` specified for YAML field → ignored (YAML is already typed), warn in dev
- Example generation failure → warn, app continues startup

## File Structure

```
libs/config/src/
├── config.module.ts                          # CHANGED — format, path, new forRoot/forFeature signature
├── decorators/
│   └── env.decorator.ts                      # MINOR — Array type support in IEnvOptions
├── enums/
│   ├── index.ts
│   ├── kernel.enum.ts
│   └── config-format.enum.ts                 # NEW — ConfigFormat enum
├── helpers/
│   └── config-builder.helper.ts              # CHANGED — delegates to IConfigResolver
├── providers/
│   └── env-example.provider.ts               # CHANGED — delegates to formatter
├── resolvers/                                # NEW directory
│   ├── config-resolver.interface.ts          # IConfigResolver interface
│   ├── env.resolver.ts                       # EnvResolver (logic from ConfigBuilder)
│   ├── yaml.resolver.ts                      # YamlResolver (parsing + dot-path)
│   └── config-resolver.factory.ts            # Factory by format
├── formatters/                               # NEW directory
│   ├── example-formatter.interface.ts        # IExampleFormatter interface
│   ├── env-example.formatter.ts              # Existing logic from EnvExampleProvider
│   └── yaml-example.formatter.ts             # YAML example generation
├── tokens/
│   └── index.ts                              # CHANGED — CONFIG_RESOLVER token
├── types/
│   ├── index.ts                              # CHANGED — new types
│   ├── app-config.interface.ts
│   ├── config-factory.type.ts
│   ├── config-module-options.interface.ts     # NEW
│   └── config-feature-options.interface.ts    # NEW
└── index.ts                                  # CHANGED — new exports
```

### Dependencies

- `yaml` — new dependency in `libs/config/package.json`

## Testing

Unit tests (jest):
- `YamlResolver` — parsing, dot-path resolution, caching, edge cases
- `EnvResolver` — type conversion (refactored from ConfigBuilder)
- `ConfigResolverFactory` — returns correct resolver by format
- `ConfigBuilder` — both formats with mock resolver, arrays, objects, scalars, defaults
- `YamlExampleFormatter` — YAML example generation, nesting, arrays
- `EnvExampleFormatter` — existing logic extracted from EnvExampleProvider

Integration tests:
- Full flow: `ConfigModule.forRoot({ format: 'yaml' })` → parse → resolve → validate → frozen config
- `forRoot` + `forFeature` with different paths
- Backward compatibility with `format: 'dotenv'`
- Example generation for both formats
- Fail-fast scenarios

## Design Decisions

| Decision | Rationale |
|---|---|
| Single format per app | Prevents confusing mixed-source configs; simplifies mental model |
| `@Env()` stays as single decorator | Source-agnostic — key interpretation depends on module-level format |
| ConfigResolver abstraction | Clean SRP, testable, extensible for future formats |
| Example generation pre-DI | Example file must exist even if app crashes on validation |
| `yaml` npm package | Actively maintained, YAML 1.2 compliant, good TypeScript support |
| No hot reload | Proper DI hot reload is unsolved industry-wide; read-once is sufficient. Architecture does not preclude adding it later. |
| Arrays from env via JSON.parse | User's choice to use ugly format; we parse it, validation catches bad input |
| `type: Array` for array fields | Explicit declaration, `example` provides template for example generation |
