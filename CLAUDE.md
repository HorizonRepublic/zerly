# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Zerly** (`nestkit-x`) is an Nx monorepo publishing a suite of performant NestJS modules under the `@zerly/*` npm scope. It uses **pnpm**, **NestJS 11**, **Fastify** as the HTTP adapter, and **typia** for compile-time + runtime type validation (instead of class-validator).

## Commands

```bash
# Install dependencies
pnpm install

# Build all libs
pnpm nx run-many -t build

# Build only affected projects
pnpm nx affected -t build

# Build a single library (e.g., kernel)
pnpm nx build kernel

# Lint all (with auto-fix)
pnpm lint

# Lint only affected
pnpm nx affected -t lint

# Test all
pnpm nx run-many -t test

# Test only affected
pnpm nx affected -t test

# Test a single library
pnpm nx test kernel

# Run the example app (uses bun + webpack watch)
pnpm nx serve example-app

# Run example app with Node.js instead of Bun
pnpm nx run example-app:serve:node
```

## Monorepo Structure

Each library under `libs/` has its own `package.json` (published as `@zerly/<name>`), `project.json` (Nx targets), and `tsconfig*.json`. Libraries are built with `@nx/js:tsc` into `dist/libs/<name>`.

| Library | Package | Purpose |
|---|---|---|
| `libs/core` | `@zerly/core` | Shared base types (`IBaseResource` with UUID + ms timestamps) |
| `libs/config` | `@zerly/config` | `@Env()` decorator + `ConfigBuilder` for typed env config |
| `libs/kernel` | `@zerly/kernel` | Application bootstrap entry point |
| `libs/logger` | `@zerly/logger` | Pino-based structured logging via `nestjs-pino` |
| `libs/db` | `@zerly/db` | MikroORM + PostgreSQL; `BaseEntity` with soft-delete |
| `libs/microservice` | `@zerly/microservice` | NATS JetStream transport |
| `libs/swagger` | `@zerly/swagger` | Swagger/OpenAPI placeholder (WIP) |
| `libs/cli` | `@zerly/cli` | CLI tooling via `nest-commander` |

`apps/example-app` is the reference application demonstrating how to use these libraries together.

## Architecture

### Bootstrap Flow

`Kernel.init(AppModule, { mode: AppMode.Server })` is the single entry point for all apps. Internally it:
1. Creates a `FastifyAdapter` with opinionated defaults (trace ID header, qs querystring parser, proto poisoning protection)
2. Wraps the user's module in `KernelModule.forServe(appModule)` which globally provides `ConfigModule`, `AllExceptionsFilter`, `AppRefService`, and `AppStateService`
3. Manages lifecycle state transitions via RxJS: `NotReady → Created → Listening`

For CLI mode, `Kernel.init(AppModule, { mode: AppMode.Cli })` delegates to `nest-commander` via `CommandFactory.run`.

### Config Pattern

Define a config class with `@Env()` property decorators, then build it with `ConfigBuilder`:

```typescript
export const appConfig = ConfigBuilder
  .from(AppConfig, APP_CONFIG)
  .validate(typia.assertEquals<IAppConfig>)
  .build();
```

`@Env('PORT', { default: 3000, type: Number })` reads from `process.env` at startup. Missing required vars cause `process.exit(1)`. All config objects are frozen.

### DI Token Pattern

Services injected by interface use symbol tokens (e.g. `APP_REF_SERVICE`, `APP_STATE_SERVICE`). Tokens live in `src/tokens/index.ts`, interfaces in `src/types/`. Providers use `satisfies Provider<IInterface>` for type safety.

### Type Validation

**typia** (not class-validator/zod) is the validation library. It requires the `typia/lib/transform` TypeScript compiler plugin configured in `tsconfig.base.json`. Use `typia.assertEquals<T>` for strict validation that throws on unknown properties.

### Database Entities

All entities extend `BaseEntity` from `@zerly/db`, which provides:
- `id`: UUID v7 (auto-generated)
- `createdAt`, `updatedAt`, `deletedAt`: Unix milliseconds (number), soft-delete pattern
- Static helpers: `Entity.tableName()`, `Entity.columns()` for type-safe query building

## TypeScript Configuration

The project uses strict TypeScript settings including `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, and `noPropertyAccessFromIndexSignature`. These are enforced project-wide via `tsconfig.base.json`. Path aliases (`@zerly/*`) map to library source entries.

## Commit Convention

Uses [Conventional Commits](https://www.conventionalcommits.org/) enforced by commitlint + husky:
- `feat(scope): ...` → minor version bump
- `fix(scope): ...` → patch bump
- `chore|refactor|perf|docs|...` → patch bump

PRs should target the `dev` branch, not `main`.