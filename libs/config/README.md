# @zerly/config

[![npm version](https://img.shields.io/npm/v/@zerly/config.svg)](https://www.npmjs.com/package/@zerly/config)

Enhanced configuration module for NestJS applications with automatic `.env.example` generation based on decorated
configuration classes.

## Installation

```shell
npm install @zerly/config
```

## Features

- Wrapper around `@nestjs/config` with additional functionality
- Automatic `.env.example` file generation from configuration metadata
- Type-safe environment variable decorators
- Support for nested configuration modules
- Environment-specific example generation
- Smart content-based file regeneration (only updates when configuration changes)
- Support for default values and examples in generated files

## Quick Start

### 1. Define Configuration Class

```typescript
import { Env, ConfigBuilder } from '@zerly/config';

export class DatabaseConfig {
  @Env('DATABASE_HOST', {
    description: 'Database server hostname',
    example: 'localhost',
    default: 'localhost',
  })
  public host!: string;

  @Env('DATABASE_PORT', {
    description: 'Database server port',
    example: 5432,
    type: Number,
    default: 5432,
  })
  public port!: number;

  @Env('DATABASE_NAME', {
    description: 'Database name',
    example: 'myapp',
  })
  public name!: string;

  @Env('DATABASE_URL', {
    description: 'Full database connection string',
    example: 'postgresql://user:password@localhost:5432/myapp',
  })
  public url!: string;
}

export const databaseConfig = ConfigBuilder.from(DatabaseConfig, 'DATABASE_CONFIG')
  .validate((config) => {
    // Add custom validation logic here
    if (!config.url && !config.host) {
      throw new Error('Either DATABASE_URL or DATABASE_HOST must be provided');
    }
    return config;
  })
  .build();
```

### 2. Register in Application Module

```typescript
import { Module } from '@nestjs/common';
import { ZerlyConfigModule } from '@zerly/config';
import { Environment } from '@zerly/core';
import { databaseConfig } from './configs/database.config';
import { appConfig } from './configs/app.config';

@Module({
  imports: [
    ZerlyConfigModule.forRoot({
      load: [databaseConfig, appConfig],
      exampleGenerationEnv: Environment.Local,
    }),
  ],
})
export class AppModule {
}
```

For feature modules:

```typescript
@Module({
  imports: [ZerlyConfigModule.forFeature(featureConfig)],
})
export class FeatureModule {}
```

### 3. Use Configuration in Services

```typescript
import { Injectable, Inject } from '@nestjs/common';
import { DatabaseConfig } from './configs/database.config';

@Injectable()
export class DatabaseService {
  constructor(@Inject('DATABASE_CONFIG') private readonly dbConfig: DatabaseConfig) {
    console.log(`Connecting to ${this.dbConfig.host}:${this.dbConfig.port}`);
  }
}
```

Alternatively, use ConfigService from @nestjs/config:

```typescript
import { Injectable } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';

@Injectable()
export class DatabaseService {
  constructor(private readonly configService: ConfigService) {
    const host = this.configService.get<string>('DATABASE_HOST');
    const port = this.configService.get<number>('DATABASE_PORT');
  }
}
```

## API Reference

### `ZerlyConfigModule.forRoot(options)`

Registers the configuration module globally with the specified options.

#### Options

```typescript
interface IConfigModuleOptions {
  /**
   * Array of configuration factory functions to load.
   */
  load?: ConfigFactory[];

  /**
   * Environment in which .env.example should be generated.
   * Set to false to disable generation.
   * @default Environment.Local
   */
  exampleGenerationEnv?: Environment | false;
}
```

### `ZerlyConfigModule.forFeature(config)`

Registers additional configuration for a specific feature module.

```typescript
@Module({
  imports: [ZerlyConfigModule.forFeature(featureConfig)],
})
export class FeatureModule {}
```

### `@Env(key, options?)` Decorator

Marks a class property as an environment variable with metadata for documentation generation.

#### Parameters

- `key`: Environment variable name
- `options`: Configuration options

```typescript
interface IEnvOptions<TType = typeof String> {
  /**
   * Type constructor for value conversion.
   * @default String
   */
  type?: typeof String | typeof Number | typeof Boolean | EnumType;

  /**
   * Default value if environment variable is not set.
   */
  default?: InferTypeFromConstructor<TType>;

  /**
   * Example value for .env.example generation.
   */
  example?: InferTypeFromConstructor<TType>;

  /**
   * Human-readable description for documentation.
   */
  description?: string;
}
```

### `ConfigBuilder`

Utility class for building configuration factories with validation support.

```typescript
import { ConfigBuilder } from '@zerly/config';

const configFactory = ConfigBuilder.from<T>(ConfigClass, token)
  .validate((config: T) => T)
  .build();
```

Example:

```typescript
export const appConfig = ConfigBuilder.from(AppConfig, 'APP_CONFIG')
  .validate((config) => {
    if (config.port < 1024) {
      throw new Error('Port must be greater than 1024');
    }
    return config;
  })
  .build();
```

## Generated `.env.example`

The module automatically generates a comprehensive `.env.example` file in `examples/env/{app-name}.env`:

```env
###
#
# This is auto generated file based on all config registered. Do not edit it manually.
# If some of configs are not presented here, it means that they are not used @Env() decorator or
# not registered with ConfigBuilder.
#
###

# -- database
DATABASE_HOST="localhost"
DATABASE_PORT="5432"
DATABASE_NAME="myapp"
DATABASE_URL="postgresql://user:password@localhost:5432/myapp"

# -- app
APP_NAME="my-application"
APP_PORT="3000"
NODE_ENV="development"
```

## Advanced Usage

### Enum Types

```typescript
enum LogLevel {
  Debug = 'debug',
  Info = 'info',
  Error = 'error',
}

export class AppConfig {
  @Env('LOG_LEVEL', {
    type: LogLevel,
    example: LogLevel.Info,
    default: LogLevel.Info,
  })
  public logLevel!: LogLevel;
}
```

### Boolean Values

```typescript
export class FeatureConfig {
  @Env('FEATURE_ENABLED', {
    type: Boolean,
    example: true,
    default: false,
  })
  public enabled!: boolean;
}
```

### Numeric Values

```typescript
export class ServerConfig {
  @Env('MAX_CONNECTIONS', {
    type: Number,
    example: 100,
    default: 50,
  })
  public maxConnections!: number;
}
```

## Configuration Generation Control

The `.env.example` file is generated only when:

1. The application runs in the specified environment (`exampleGenerationEnv`)
2. The configuration content has changed (SHA256 hash comparison)

To disable generation entirely:

```typescript
ZerlyConfigModule.forRoot({
  load: [appConfig],
  exampleGenerationEnv: false,
});
```

## Integration with @nestjs/config

This module extends `@nestjs/config` functionality and maintains full compatibility:

- All `@nestjs/config` features are available
- Use `ConfigService` as usual
- Support for `.env` file loading
- Variable expansion with `expandVariables: true` (enabled by default)
- Configuration validation can be added via ConfigBuilder

## Best Practices

1. **Organize by domain**: Create separate configuration classes for different application domains (database, cache,
   external services)

2. **Use ConfigBuilder**: Always wrap configurations with ConfigBuilder for better type safety and validation support

3. **Provide examples**: Always set `example` values for better developer experience

4. **Add descriptions**: Document each environment variable with clear descriptions

5. **Type safety**: Use proper types (Number, Boolean, Enum) instead of strings when possible

6. **Validation**: Add validation logic using ConfigBuilder's validate method

7. **Secrets**: Never commit actual `.env` files, only `.env.example`

## Example Application Structure

```
src/
├── app.module.ts
├── configs/
│   ├── app.config.ts
│   ├── database.config.ts
│   ├── cache.config.ts
│   └── index.ts
└── modules/
    └── ...

examples/
└── env/
    └── my-app.env  (auto-generated)
```

## Contributing

Contributions are welcome. Please ensure all tests pass and follow the existing code style.

## License

MIT © Horizon Republic

```

```
