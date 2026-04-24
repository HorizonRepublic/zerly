# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Critical Rules

- **NEVER add `Co-Authored-By` or any co-authorship attribution to commits.** Applies to ALL commits without exception.
- **NEVER add "Generated with Claude Code" or any AI attribution** to PR descriptions, commit messages, or documentation.
- **Communicate with the user in Ukrainian.** Code, comments, docs, commit messages, and PR titles/bodies stay in **English**.
- **Full orthographic correctness.** Preserve diacritics, accents, and special characters in Ukrainian and other non-English text. Never substitute `nao` for `não`, `fur` for `für`, or similar ASCII fallbacks.
- **NEVER reference internal plan/spec/brainstorm documents from code, tests, or godoc/TSDoc.** Phrases like "see spec §5.3", "per Task 3.2", "from the plan", "per the roadmap" are forbidden in any artifact that ships (source files, comments, commit bodies that will be read by external contributors). Plans and specs are **internal scaffolding**; the reader of `@zerly/*` and the gateway source has no access to them and the references become noise when the plan is archived. If you are tempted to cite a plan section, instead **inline the concrete rationale, constraint, or invariant** into the comment itself (e.g., `// Rejections do not advance TAT` instead of `// See spec §3.1`). Commit message subjects are fine (`feat(x): add Y`), but commit bodies MUST NOT cite internal docs either.

## Design Philosophy

- **This is a production-ready library suite.** Every feature must be implemented with production scenarios in mind. No half-measures, no "future improvements" hidden behind TODOs, no shortcuts that leak complexity onto the user.
- When the user asks for a feature — deliver the full solution, not a simplified version with placeholders. If a production-grade approach is harder, that's the one to build.
- Never suggest deferring critical functionality. If message loss, data corruption, downtime, or security exposure is on the table — solve it now, not later.
- **Defaults must work for production, not only for demos.** A developer copying an example from the README must end up with code that is safe to deploy without rewriting.

## Project Overview

**Zerly** (`nestkit-x`) is an Nx monorepo publishing a suite of performant NestJS modules under the `@zerly/*` npm scope. It uses **pnpm**, **NestJS 12 (alpha)**, **Fastify** as the HTTP adapter, and **typia** for compile-time + runtime type validation (instead of class-validator).

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
| `libs/errors` | `@zerly/errors` | HTTP-only exception filter, HttpException-native, optional error reporter port |
| `libs/gateway-sdk` | `@zerly/gateway-sdk` | NestJS SDK for zerly-gateway-server (decorators, filter, interceptor) |
| `libs/swagger` | `@zerly/swagger` | Swagger/OpenAPI placeholder (WIP) |
| `libs/cli` | `@zerly/cli` | CLI tooling via `nest-commander` |

`apps/example-app` is the reference application demonstrating how to use these libraries together. `apps/gateway-server` is the Go HTTP edge server (Hertz + sonic + zerolog).

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

## Coding Conventions

### General

- **No `as any`.** Use proper typing, generics, or `unknown` with narrowing. For test doubles prefer `jest.fn<Signature>()` with an explicit generic or a minimal typed mock; never reach for `any` to silence the compiler.
- **Prefer arrow functions** (`const fn = () => {}`) over `function` declarations for non-class utilities. Enforced by ESLint `prefer-arrow/prefer-arrow-functions`.
- **No magic strings for enumerated values.** Use TypeScript `enum`, const object (`as const`) with `keyof typeof`, or a literal union type. Magic strings are only acceptable for opaque external identifiers (NATS subjects, database column names) where the value IS the contract.
- **Lookup maps over nested ternaries.** `const map = { a: 1, b: 2 } as const; return map[key] ?? fallback;` beats `key === 'a' ? 1 : key === 'b' ? 2 : fallback`. Enforced by `sonarjs/no-nested-conditional`.
- **Exhaustive switches with `never` check** for tagged unions:
  ```typescript
  switch (kind) {
    case Kind.A: return handleA();
    case Kind.B: return handleB();
    default: {
      const _exhaustive: never = kind;
      throw new Error(`Unhandled kind: ${String(_exhaustive)}`);
    }
  }
  ```
- **`padding-line-between-statements` ESLint rule** — blank line before `return`, `expect`, and assignments that follow a multi-line block. Respect this when writing or editing code; lint failures here are pure noise and must not be the reason a CI run fails.
- **`@typescript-eslint/no-redundant-type-constituents`** — avoid unions like `'http' | string` (the literal is absorbed by `string`). Use either the literal or the generic, not both.
- **`@typescript-eslint/naming-convention`** — interfaces prefixed with `I` (`IUserService`, not `UserService`), camelCase for properties, PascalCase for types. NATS snake_case fields need a scoped `/* eslint-disable @typescript-eslint/naming-convention */` with a comment explaining why.
- **`sonarjs/cognitive-complexity` max 15** — if a function exceeds this, split it before adding more behavior. Refactoring for complexity is always cheaper than reviewing a bloated function.

### Comments and documentation

- **Write thorough TSDoc on public APIs.** This monorepo is feeding a Docusaurus site later; every exported symbol is a documentation candidate. Include `@remarks`, `@example`, and `@param` blocks where they add context.
- Use `@template` for generics. No blank line before tag blocks (`@remarks` / `@param` / `@example`) — ESLint `jsdoc/tag-lines` rejects them.
- **Default to writing no inline comments.** Add one only when the WHY is non-obvious: a hidden constraint, a subtle invariant, a workaround for a specific bug, behavior that would surprise a reader. If removing the comment wouldn't confuse a future reader, don't write it.
- **Never explain WHAT the code does** — names do that. Never reference the current task, fix, or callers ("used by X", "added for Y flow", "handles issue #123") — those belong in the PR description and rot as the codebase evolves.

### Documentation-first development

The codebase will generate a full Docusaurus documentation site. TSDoc is the primary input — write it for the **end user of the library**, not for the internal developer. Every piece of documentation should help a consumer answer "how do I use this?" and "what happens when...?".

**What to document in TSDoc (mandatory for every exported symbol):**
- **Interfaces/types**: what the type represents, which component consumes it, what happens when fields are omitted (defaults), merge semantics if applicable.
- **Decorator options**: every field with its default value, interaction with `forRoot` defaults (override vs merge), and the side effects on the wire format (what gets written to KV, what Go gateway does with it).
- **DI tokens**: what value sits behind the token, who provides it, who consumes it.
- **Module methods** (`forRoot`, `forRootAsync`): full usage example with env-driven config, what providers are registered, what happens with zero-config.
- **Behavioral contracts**: things like "auth verifier and route handler share one timeout deadline", "per-route timeout overrides global but auth sub-request uses the same budget", "CORS preflight is handled by Go without NATS round-trip". These are non-obvious to end users and MUST be in `@remarks`.
- **Wire format notes**: when a TypeScript type mirrors a Go struct (e.g., `IGatewayHttpMeta` ↔ `HTTPMeta`, `IGatewayCorsConfig` �� `CORSMeta`), document the cross-boundary contract in `@remarks` with a pointer to the Go file. Any rename on either side requires a synchronized update.

**Go godoc**: same standard — every exported type, function, and method gets a comment. For structs that mirror TypeScript interfaces, reference the SDK type name so grep finds both sides.

**What NOT to put in TSDoc:**
- Implementation details that don't affect the consumer (pool internals, RxJS pipe shape).
- Temporary state ("added in PR #42", "workaround for NestJS bug").
- Anything derivable from the type signature alone.

## Testing Conventions

- **Test runner:** Jest 30 with `@jest/globals` explicit imports — no globals pattern.
  ```typescript
  import { describe, it, expect, jest, beforeEach, afterEach } from '@jest/globals';
  ```
- **Mocking: `createMock<T>()` from `@golevelup/ts-jest`** for typed deep mocks of interfaces and services. Avoid ad-hoc `{} as T` casts and plain `jest.fn()` grab-bags for multi-method collaborators — `createMock<IUserService>()` gives you a fully-typed double where every method is a `jest.Mock`, and unused methods are auto-stubbed without compiler noise.
  ```typescript
  import { createMock } from '@golevelup/ts-jest';

  const users = createMock<IUserService>({
    findById: jest.fn().mockResolvedValue(fakeUser),
  });
  ```
  Gotcha: `'ack' in proxy` returns false unless `ack` is explicitly provided, and a handful of other "magic" presence checks behave the same. When asserting presence, pass the field through in the `createMock` override.
- **Test data: `@faker-js/faker`.** Prefer faker over inline literals for anything that is not part of the assertion. Stable fields that are part of the assertion (e.g. "expect status 404") stay as literals; volatile identity fields (user ids, emails, tokens, request ids) go through faker.
  ```typescript
  import { faker } from '@faker-js/faker';

  const user = {
    id: faker.string.uuid(),
    email: faker.internet.email(),
    createdAt: faker.date.past().getTime(),
  };
  ```
  Seed faker for deterministic runs when snapshot-testing or debugging flakiness: `faker.seed(42)` in a `beforeAll`.
- **System under test variable (TypeScript):** always `sut`. Reviewers learn to scan for that identifier in every test file. This convention applies to the TypeScript / NestJS / Jest suites where the sut pattern is well-established. Go tests are exempt — idiomatic Go uses a short, domain-specific name (`handler`, `store`, `watcher`, `r`, `s`) and the rest of the file already mirrors that convention. Do not sweep `*_test.go` files for a cosmetic rename.
- **Given-When-Then structure.** Comment blocks or blank-line-separated sections mark each phase. Keep setup tight, action atomic, assertion focused.
- **Test ordering within a `describe`:** happy path → edge cases → error cases. Readers should see the success flow first so they understand what the code is supposed to do before they learn what it rejects.
- **Cleanup discipline.** `afterEach(() => jest.resetAllMocks())` in every suite that uses mocks. Never rely on ambient state leaking between tests.
- **Assertions on async code.** Use `await expect(promise).rejects.toThrow(...)` — never implicit auto-await.
- **Integration tests as empirical verification.** When uncertain about library or NATS behavior, write a (possibly throwaway) integration test rather than guessing from docs. For `@zerly/microservice` and `gateway-sdk`, prefer integration tests over unit tests for transport-level code.
- **Coverage is not a goal, correctness is.** 100% branch coverage on getters is worse than 60% on a hot path with thoughtful edge cases.
- **`jest.config.cts` should have `passWithNoTests: true`** for libraries that genuinely have no tests yet — otherwise `nx run-many -t test` fails with no actionable signal.

## Commit Convention

Uses [Conventional Commits](https://www.conventionalcommits.org/) enforced by commitlint + husky:
- `feat(scope): ...` → minor version bump
- `fix(scope): ...` → patch bump
- `chore|refactor|perf|docs|test|...` → patch bump

Breaking changes use `!` after the scope (e.g. `refactor(errors)!: drop DomainException`) and an explanatory body.

## Pull Requests

PRs target `dev` branch. `dev → main` is the release gate.

**Title** must follow Conventional Commits with a scope — enforced by `lint-pr.yml`:
```
type(scope): short description
```

**Description** follows `.github/pull_request_template.md`:
- **What** — what changed and why
- **Changes** — bullet list of key changes
- **Type** — check the appropriate checkbox
- **Notes** — optional: breaking changes, follow-ups

Do not add "Generated with Claude Code" or any AI attribution to PR descriptions.
