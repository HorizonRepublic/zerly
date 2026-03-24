# @zerly/config

[![npm version](https://img.shields.io/npm/v/@zerly/config.svg)](https://www.npmjs.com/package/@zerly/config)

Type-safe configuration module for NestJS with support for **dotenv** and **YAML** sources, automatic example file generation, and validation.

## Installation

```shell
pnpm add @zerly/config
```

## Features

- Dual format support: `process.env` (dotenv) and YAML files
- `@Env()` decorator for declarative config field mapping
- `ConfigBuilder` with fluent API, lazy evaluation, and custom validation
- Automatic `.env.example` / `env.example.yaml` generation (pre-DI, survives crashes)
- Frozen config objects (immutable after creation)
- Smart app root resolution for monorepo setups (Nx, pnpm workspaces)
- Full `@nestjs/config` compatibility (`ConfigService.get()` works as usual)

## Quick Start

### 1. Define a config class

```typescript
import { Env, ConfigBuilder } from '@zerly/config';

const DB_CONFIG = Symbol('db-config');

class DbConfig {
  @Env('database.host', { default: 'localhost' })
  host!: string;

  @Env('database.port', { type: Number, default: 5432 })
  port!: number;

  @Env('database.name', { description: 'Database name' })
  name!: string;
}

export const dbConfig = ConfigBuilder
  .from(DbConfig, DB_CONFIG)
  .validate(myValidator) // typia, zod, or any (config) => config function
  .build();
```

### 2. Register in your module

```typescript
import { Module } from '@nestjs/common';
import { ConfigModule } from '@zerly/config';
import { dbConfig } from './configs/db.config';

@Module({
  imports: [
    ConfigModule.forRoot({
      format: 'yaml',           // or omit for dotenv (default)
      path: 'config/app.yaml',  // default YAML file path (optional)
      load: [dbConfig],
    }),
  ],
})
export class AppModule {}
```

### 3. Use in services

```typescript
import { Injectable } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';

@Injectable()
export class DbService {
  constructor(private readonly config: ConfigService) {
    const db = this.config.getOrThrow<DbConfig>(DB_CONFIG);
    console.log(`Connecting to ${db.host}:${db.port}`);
  }
}
```

## Formats

### Dotenv (default)

Reads from `process.env`. Keys are flat env var names:

```typescript
@Env('DATABASE_PORT', { type: Number, default: 5432 })
port!: number;
```

### YAML

Reads from YAML files. Keys are dot-separated paths:

```typescript
@Env('database.port', { type: Number, default: 5432 })
port!: number;
```

```yaml
# env.yaml
database:
  port: 5432
  host: localhost
```

The format is set **once** at module level — no mixing within one app.

## API

### `ConfigModule.forRoot(options?)`

Registers configuration globally.

```typescript
interface IConfigModuleOptions {
  /** Config format: 'dotenv' (default) or 'yaml'. */
  format?: ConfigFormat;

  /** Default YAML file path. Defaults to 'env.yaml' resolved from app root. */
  path?: string;

  /** Config factories to load. Accepts ConfigFactory or { path, config } objects. */
  load?: Array<ConfigFactory | IConfigLoadItem>;

  /** Output directory for example file generation (useful in monorepos). */
  outputDir?: string;
}
```

### `ConfigModule.forFeature(configOrOptions)`

Registers feature-specific configuration.

```typescript
// Simple
ConfigModule.forFeature(notificationsConfig)

// With custom YAML path
ConfigModule.forFeature({
  path: 'config/notifications.yaml',
  config: notificationsConfig,
})
```

### `@Env(key, options?)`

Property decorator mapping a config field to a key.

| Option        | Type                                           | Description                                                  |
|---------------|------------------------------------------------|--------------------------------------------------------------|
| `type`        | `String \| Number \| Boolean \| Array \| Enum` | Type conversion (dotenv only; YAML values are already typed) |
| `default`     | `T`                                            | Fallback value if key is missing                             |
| `example`     | `T`                                            | Example value for generated files                            |
| `description` | `string`                                       | Inline comment in generated example                          |
| `comment`     | `string`                                       | Additional comment                                           |

### `ConfigBuilder.from(Class, token).validate(fn).build()`

Fluent builder producing a `ConfigFactory` for `@nestjs/config`. Resolution is **lazy** — values are read when NestJS DI invokes the factory, not at import time.

## Per-config YAML paths

Each config can read from its own YAML file:

```typescript
ConfigModule.forRoot({
  format: 'yaml',
  path: 'config/app.yaml',              // default for configs without own path
  load: [
    appConfig,                            // uses default path
    { path: 'config/db.yaml', config: dbConfig },  // own file
  ],
})
```

## Example file generation

On startup, the module generates `.env.example` (dotenv) or `env.example.yaml` (YAML) **synchronously before DI resolution** — so the file exists even if the app crashes on validation.

- Content is written only when the SHA-256 hash changes
- Errors during generation are logged as warnings, never block startup
- In dotenv mode, a secondary async pass runs post-DI with resolved runtime values

## Array support

```typescript
// YAML — native arrays
@Env('database.replicas', {
  type: Array,
  example: [{ host: 'replica1', port: 5432 }],
})
replicas!: IReplica[];

// Dotenv — JSON string in env var
// DATABASE_REPLICAS='[{"host":"replica1","port":5432}]'
@Env('DATABASE_REPLICAS', { type: Array })
replicas!: IReplica[];
```

## Monorepo support

In Nx / pnpm workspaces, relative YAML paths and example output are resolved against the **app root** (detected via `process.argv[1]` heuristic), not `process.cwd()`. Override with `outputDir` if needed:

```typescript
ConfigModule.forRoot({
  outputDir: 'apps/my-app',
  load: [appConfig],
})
```

## License

MIT
