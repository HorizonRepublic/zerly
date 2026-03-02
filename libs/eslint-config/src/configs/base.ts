import importPlugin from 'eslint-plugin-import-x';
import noSecrets from 'eslint-plugin-no-secrets';
// eslint-disable-next-line @typescript-eslint/ban-ts-comment
// @ts-expect-error
import preferArrowPlugin from 'eslint-plugin-prefer-arrow';
import eslintPluginPrettier from 'eslint-plugin-prettier';
// eslint-disable-next-line @typescript-eslint/ban-ts-comment
// @ts-expect-error
import security from 'eslint-plugin-security';
import sonarjs from 'eslint-plugin-sonarjs';
import unicorn from 'eslint-plugin-unicorn';
import unusedImports from 'eslint-plugin-unused-imports';

import type { Linter } from 'eslint';

/** Common ignores for all Zerly projects. Can be extended by the consumer. */
export const ignoresConfig: Linter.Config = {
  ignores: [
    '**/node_modules/**',
    '**/dist/**',
    '**/.nx/**',
    '**/tmp/**',
    '**/.docusaurus/**',
    '**/webpack.config.js',
  ],
};

/** SonarJS recommended rules scoped to JS/TS files. */
export const sonarjsConfig: Linter.Config = {
  ...sonarjs.configs.recommended,
  files: ['**/*.{js,jsx,ts,tsx}'],
  rules: {
    ...sonarjs.configs.recommended.rules,
    'sonarjs/todo-tag': 'off',
  },
};

/**
 * Base rules for all JS/TS files.
 *
 * Includes: import ordering, code structure, unicorn patterns,
 * security checks, secret detection, and Prettier integration.
 * Does NOT include type-aware rules — those live in `typescriptConfig`.
 */
export const baseConfig: Linter.Config = {
  files: ['**/*.ts', '**/*.tsx', '**/*.js', '**/*.jsx'],
  plugins: {
    'prefer-arrow': preferArrowPlugin,
    prettier: eslintPluginPrettier,
    'unused-imports': unusedImports,
    // eslint-disable-next-line @typescript-eslint/ban-ts-comment
    // @ts-expect-error
    'import-x': importPlugin,
    unicorn,
    security,
    'no-secrets': noSecrets,
  },
  rules: {
    // Naming conventions
    camelcase: ['error', { ignoreDestructuring: false, properties: 'never' }],

    complexity: ['warn', 10],

    // Import ordering and deduplication
    'import-x/newline-after-import': ['error', { count: 1 }],
    'import-x/no-duplicates': 'error',
    'import-x/order': [
      'error',
      {
        groups: ['builtin', 'external', 'internal', 'parent', 'sibling', 'index', 'type'],
        pathGroups: [
          { pattern: '@nestjs/**', group: 'external', position: 'before' },
          { pattern: '@nestia/**', group: 'external', position: 'before' },
          { pattern: '@angular/**', group: 'external', position: 'before' },
          { pattern: '@zerly/**', group: 'internal', position: 'before' },
        ],
        pathGroupsExcludedImportTypes: ['builtin', 'type'],
        'newlines-between': 'always',
        alphabetize: { order: 'asc', caseInsensitive: true },
      },
    ],

    // Function structure rules
    'lines-between-class-members': [
      'error',
      {
        enforce: [
          { blankLine: 'always', next: 'method', prev: 'method' },
          { blankLine: 'always', next: 'method', prev: 'field' },
        ],
      },
      { exceptAfterSingleLine: true },
    ],

    'max-depth': ['warn', 4],
    'max-params': ['warn', 4],

    // General code quality
    'no-alert': 'error',
    'no-console': ['warn', { allow: ['warn', 'error'] }],
    'no-debugger': 'error',
    'no-duplicate-imports': 'off',
    'no-useless-constructor': 'error',
    'no-useless-return': 'error',
    'no-var': 'error',

    // Code formatting & padding
    'padding-line-between-statements': [
      'error',
      { blankLine: 'always', next: '*', prev: ['const', 'let', 'var'] },
      { blankLine: 'any', next: ['const', 'let', 'var'], prev: ['const', 'let', 'var'] },
      { blankLine: 'always', next: '*', prev: 'import' },
      { blankLine: 'any', next: 'import', prev: 'import' },
      { blankLine: 'always', next: 'function', prev: '*' },
      { blankLine: 'always', next: 'class', prev: '*' },
      { blankLine: 'always', next: 'export', prev: '*' },
      { blankLine: 'always', next: '*', prev: 'block-like' },
    ],

    'prefer-arrow-callback': 'error',
    'prefer-arrow/prefer-arrow-functions': [
      'error',
      {
        classPropertiesAllowed: false,
        disallowPrototype: true,
        singleReturnOnly: false,
      },
    ],

    'prefer-const': 'error',
    'prefer-template': 'error',

    // Prettier integration
    'prettier/prettier': 'error',

    // Node.js — prefer explicit node: protocol for builtins
    'unicorn/prefer-node-protocol': 'error',

    // Error handling quality
    'unicorn/error-message': 'error',
    'unicorn/throw-new-error': 'error',
    'unicorn/prefer-optional-catch-binding': 'error',

    // Cleaner code patterns
    'unicorn/no-typeof-undefined': 'error',
    'unicorn/no-useless-promise-resolve-reject': 'error',
    'unicorn/prefer-export-from': ['error', { ignoreUsedVariables: true }],

    // Security — catch common backend vulnerabilities
    'security/detect-unsafe-regex': 'error',
    'security/detect-eval-with-expression': 'error',
    'security/detect-bidi-characters': 'error',
    'security/detect-new-buffer': 'warn',
    'security/detect-buffer-noassert': 'warn',
    'security/detect-possible-timing-attacks': 'warn',
    'security/detect-pseudoRandomBytes': 'warn',
    // Too noisy / not applicable for TypeScript projects
    'security/detect-object-injection': 'off',
    'security/detect-non-literal-fs-filename': 'off',
    'security/detect-non-literal-require': 'off',
    'security/detect-non-literal-regexp': 'off',
    'security/detect-no-csrf-before-method-override': 'off',
    'security/detect-child-process': 'off',

    // Prevent accidentally committed secrets (API keys, tokens, etc.)
    'no-secrets/no-secrets': ['error', { tolerance: 4.5 }],
  },
};

export { default as eslintConfigPrettier } from 'eslint-config-prettier';
