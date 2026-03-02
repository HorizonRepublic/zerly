import rxjsX from 'eslint-plugin-rxjs-x';
import unusedImports from 'eslint-plugin-unused-imports';
import tseslint from 'typescript-eslint';

import type { Linter } from 'eslint';

export interface ITypescriptConfigOptions {
  /** Absolute path to the directory containing `tsconfig.base.json`. Pass `import.meta.dirname`. */
  tsconfigRootDir: string;
}

/**
 * Type-aware TypeScript rules.
 *
 * Requires `tsconfigRootDir` so that `@typescript-eslint` rules can access
 * the TypeScript compiler for type information.
 */
export const typescriptConfig = (options: ITypescriptConfigOptions): Linter.Config => ({
  files: ['**/*.ts', '**/*.tsx'],
  languageOptions: {
    parser: tseslint.parser as Linter.Parser,
    parserOptions: {
      project: './tsconfig.base.json',
      tsconfigRootDir: options.tsconfigRootDir,
    },
  },
  plugins: {
    'rxjs-x': rxjsX,
    'unused-imports': unusedImports,
  },
  rules: {
    '@typescript-eslint/array-type': ['error', { default: 'array' }],
    '@typescript-eslint/consistent-type-definitions': ['error', 'interface'],
    '@typescript-eslint/explicit-function-return-type': [
      'error',
      {
        allowConciseArrowFunctionExpressionsStartingWithVoid: false,
        allowDirectConstAssertionInArrowFunctions: true,
        allowExpressions: false,
        allowHigherOrderFunctions: true,
        allowTypedFunctionExpressions: true,
      },
    ],
    '@typescript-eslint/explicit-member-accessibility': [
      'error',
      {
        accessibility: 'explicit',
        overrides: {
          accessors: 'explicit',
          constructors: 'explicit',
          methods: 'explicit',
          parameterProperties: 'off',
          properties: 'explicit',
        },
      },
    ],
    '@typescript-eslint/explicit-module-boundary-types': 'error',
    '@typescript-eslint/member-ordering': [
      'error',
      {
        default: [
          'public-static-field',
          'protected-static-field',
          'private-static-field',
          'public-instance-field',
          'protected-instance-field',
          'private-instance-field',
          'public-abstract-field',
          'protected-abstract-field',
          'public-constructor',
          'protected-constructor',
          'private-constructor',
          'public-static-method',
          'protected-static-method',
          'private-static-method',
          'public-instance-method',
          'protected-instance-method',
          'private-instance-method',
          'public-abstract-method',
          'protected-abstract-method',
        ],
      },
    ],
    '@typescript-eslint/method-signature-style': ['error', 'method'],
    '@typescript-eslint/naming-convention': [
      'error',
      // 1. Constants (UPPER_CASE), standard variables (camelCase), or "Enum-as-Const" (PascalCase)
      {
        selector: 'variable',
        modifiers: ['const'],
        format: ['UPPER_CASE', 'camelCase', 'PascalCase'],
        filter: {
          match: true,
          regex: '^[A-Z][A-Z0-9_]*(_TOKEN|_KEY|_CONFIG)?$|^[a-z][a-zA-Z0-9]*$|^[A-Z][a-zA-Z0-9]*$',
        },
      },
      // 2. Other variables (non-const) — only camelCase
      { selector: 'variable', format: ['camelCase'] },
      // 3. Functions and parameters — camelCase
      { selector: 'function', format: ['camelCase'] },
      { selector: 'parameter', format: ['camelCase'], leadingUnderscore: 'allow' },
      // 4. Classes, interfaces, types, enums — PascalCase
      { selector: 'typeLike', format: ['PascalCase'] },
      { selector: 'enumMember', format: ['PascalCase', 'UPPER_CASE'] },
      { selector: 'interface', format: ['PascalCase'], prefix: ['I'] },
      // 5. Object properties — allow everything (for HTTP headers, enum-objects, etc.)
      { selector: 'objectLiteralProperty', format: null },
      // 6. Class methods and accessors — camelCase
      {
        selector: 'memberLike',
        modifiers: ['private'],
        format: ['camelCase'],
        leadingUnderscore: 'allow',
      },
      { selector: 'memberLike', format: ['camelCase'] },
    ],
    '@typescript-eslint/no-confusing-void-expression': 'error',

    // Warn on usage of APIs marked @deprecated in TSDoc/JSDoc
    '@typescript-eslint/no-deprecated': 'warn',

    '@typescript-eslint/no-explicit-any': 'error',

    // Catch unhandled promises — critical for NestJS async lifecycle methods
    '@typescript-eslint/no-floating-promises': ['error', { ignoreVoid: true, ignoreIIFE: true }],

    // Catch async functions passed where sync is expected (e.g. void callbacks)
    '@typescript-eslint/no-misused-promises': 'error',

    '@typescript-eslint/no-namespace': 'off',
    '@typescript-eslint/no-non-null-assertion': 'error',
    '@typescript-eslint/no-redundant-type-constituents': 'error',

    // Flags `foo as Foo` when foo is already Foo — catches redundant casts
    '@typescript-eslint/no-unnecessary-type-assertion': 'error',

    '@typescript-eslint/no-unnecessary-condition': 'error',
    '@typescript-eslint/no-unused-vars': 'off',

    // TS-aware replacement for the base no-useless-constructor
    '@typescript-eslint/no-useless-constructor': 'error',

    '@typescript-eslint/no-useless-empty-export': 'error',
    '@typescript-eslint/prefer-nullish-coalescing': 'error',
    '@typescript-eslint/prefer-optional-chain': 'error',
    '@typescript-eslint/prefer-readonly': 'error',
    '@typescript-eslint/prefer-string-starts-ends-with': 'error',

    // In try/catch, `return await` is required — otherwise rejections escape the block
    '@typescript-eslint/return-await': ['error', 'in-try-catch'],

    // async function without any await is almost always a mistake
    '@typescript-eslint/require-await': 'error',

    '@typescript-eslint/switch-exhaustiveness-check': 'error',
    '@typescript-eslint/no-empty-function': 'off',

    // catch (err) in RxJS callbacks should be typed as unknown, not implicit any
    '@typescript-eslint/use-unknown-in-catch-callback-variable': 'error',

    // Disable base rules replaced by TS-aware equivalents
    camelcase: 'off',
    'no-duplicate-imports': 'off',
    'no-unused-vars': 'off',
    'no-useless-constructor': 'off',

    'unused-imports/no-unused-imports': 'error',
    'unused-imports/no-unused-vars': [
      'error',
      {
        args: 'after-used',
        argsIgnorePattern: '^_',
        ignoreRestSiblings: true,
        vars: 'all',
        varsIgnorePattern: '^_',
      },
    ],

    // RxJS best practices
    'rxjs-x/no-async-subscribe': 'error',
    'rxjs-x/no-nested-subscribe': 'error',
    'rxjs-x/no-unsafe-takeuntil': 'error',
    'rxjs-x/no-internal': 'error',
    'rxjs-x/no-ignored-error': 'warn',
    'rxjs-x/no-floating-observables': 'warn',
  },
});
