import eslintConfigPrettier from 'eslint-config-prettier';
import tseslint from 'typescript-eslint';

import { baseConfig, ignoresConfig, sonarjsConfig } from './base.js';
import { jsdocConfig } from './jsdoc.js';
import { typescriptConfig, type ITypescriptConfigOptions } from './typescript.js';

import type { Linter } from 'eslint';

/** Override to disable naming rules in jest config files. */
const jestOverride: Linter.Config = {
  files: ['jest.config.*'],
  rules: {
    '@typescript-eslint/naming-convention': 'off',
    camelcase: 'off',
  },
};

/**
 * Full NestJS preset: TypeScript + RxJS + sonarjs + JSDoc + prettier.
 *
 * Pass `tsconfigRootDir: import.meta.dirname` from the consumer's root ESLint config.
 * @example
 * ```js
 * // eslint.config.mjs
 * import { defineNestjsConfig } from '@zerly/eslint-config';
 *
 * export default [
 *   ...nxConfigs,
 *   ...defineNestjsConfig({ tsconfigRootDir: import.meta.dirname }),
 * ];
 * ```
 */
export const defineNestjsConfig = (options: ITypescriptConfigOptions): Linter.Config[] => [
  ...tseslint.configs.recommended,
  sonarjsConfig,
  ignoresConfig,
  baseConfig,
  typescriptConfig(options),
  jsdocConfig,
  jestOverride,
  // `typescript-eslint/recommended` has no `files` restriction and enables this rule globally.
  // jsonc-eslint-parser wraps JSON root in an ExpressionStatement, so we suppress it for JSON.
  { files: ['**/*.json'], rules: { '@typescript-eslint/no-unused-expressions': 'off' } },
  // Must be last — disables ESLint rules that conflict with Prettier formatting
  eslintConfigPrettier,
];
