# @zerly/eslint-config

Opinionated [ESLint flat config](https://eslint.org/docs/latest/use/configure/configuration-files) preset for NestJS + TypeScript projects.

## What's included

| Plugin                     | Rules                                                               |
|----------------------------|---------------------------------------------------------------------|
| `typescript-eslint`        | Strict type-aware rules, naming conventions, explicit return types  |
| `eslint-plugin-sonarjs`    | Code smell detection, cognitive complexity                          |
| `eslint-plugin-rxjs-x`     | RxJS best practices (no floating observables, no nested subscribes) |
| `eslint-plugin-unicorn`    | Modern JS patterns, `node:` protocol enforcement                    |
| `eslint-plugin-security`   | Common backend vulnerability patterns                               |
| `eslint-plugin-no-secrets` | Accidentally committed credentials detection                        |
| `eslint-plugin-import-x`   | Import ordering and deduplication                                   |
| `eslint-plugin-jsdoc`      | JSDoc validation (validate existing, not require everywhere)        |
| `eslint-plugin-prettier`   | Prettier integration                                                |
| `eslint-config-prettier`   | Disables formatting rules that conflict with Prettier               |

## Installation

```bash
npm install -D @zerly/eslint-config
```

All plugins are bundled as direct dependencies — no extra installs needed.

## Usage

```js
// eslint.config.mjs
import { defineNestjsConfig } from '@zerly/eslint-config';

export default [
  ...defineNestjsConfig({ tsconfigRootDir: import.meta.dirname }),
];
```

### With Nx

Nx-specific rules (`enforce-module-boundaries`, `dependency-checks`) stay in your own config:

```js
// eslint.config.mjs
import nx from '@nx/eslint-plugin';
import jsoncParser from 'jsonc-eslint-parser';
import { defineNestjsConfig } from '@zerly/eslint-config';

export default [
  ...nx.configs['flat/base'],
  ...nx.configs['flat/typescript'],
  ...nx.configs['flat/javascript'],

  {
    files: ['**/*.json'],
    languageOptions: { parser: jsoncParser },
    plugins: { '@nx': nx },
    rules: {
      '@nx/dependency-checks': ['error', { /* your options */ }],
    },
  },

  {
    files: ['**/*.ts', '**/*.tsx', '**/*.js', '**/*.jsx'],
    plugins: { '@nx': nx },
    rules: {
      '@nx/enforce-module-boundaries': ['error', { /* your options */ }],
    },
  },

  ...defineNestjsConfig({ tsconfigRootDir: import.meta.dirname }),
];
```

## Options

```ts
interface ITypescriptConfigOptions {
  /**
   * Absolute path to the directory containing `tsconfig.base.json`.
   * Pass `import.meta.dirname` from your root ESLint config.
   */
  tsconfigRootDir: string;
}
```

## Individual configs

For fine-grained control, each config layer is exported separately:

```js
import {
  baseConfig,       // common JS/TS rules
  typescriptConfig, // type-aware @typescript-eslint rules
  jsdocConfig,      // JSDoc validation
  sonarjsConfig,    // SonarJS recommended
  ignoresConfig,    // default ignores (node_modules, dist, .nx…)
} from '@zerly/eslint-config';
```

## Notable conventions

- **Interface prefix** — interfaces must be prefixed with `I` (`IUserService`, not `UserService`)
- **Explicit accessibility** — all class members require explicit `public`/`private`/`protected`
- **No floating promises** — unhandled promises are errors; use `void` to explicitly ignore
- **No floating observables** — RxJS observables must be subscribed, returned, or marked `void`
- **`node:` protocol** — all Node.js built-in imports must use the explicit `node:` prefix
- **`return await` in try/catch** — required so rejections are caught by the surrounding block
