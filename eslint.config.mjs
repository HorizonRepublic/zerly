import nx from '@nx/eslint-plugin';
import eslintConfigPrettier from 'eslint-config-prettier';
import preferArrowPlugin from 'eslint-plugin-prefer-arrow';
import eslintPluginPrettier from 'eslint-plugin-prettier';
import sonarjs from 'eslint-plugin-sonarjs';
import unusedImports from 'eslint-plugin-unused-imports';
import jsoncParser from 'jsonc-eslint-parser';
import tseslint from 'typescript-eslint';
import importPlugin from 'eslint-plugin-import-x';


export default [
  // Base configurations from Nx and TypeScript
  eslintConfigPrettier,
  ...nx.configs['flat/base'],
  ...nx.configs['flat/typescript'],
  ...nx.configs['flat/javascript'],
  ...tseslint.configs.recommended,

  {
    ...sonarjs.configs.recommended,
    files: ['**/*.{js,jsx,ts,tsx}'],
    rules: {
      ...sonarjs.configs.recommended.rules,
      // Disable TODO tag rule - TODOs are meant to be done later
      'sonarjs/todo-tag': 'off',
    },
  },

  // Ignored paths
  {
    ignores: [
      '**/node_modules/**',
      '**/dist/**',
      '**/.nx/**',
      '**/tmp/**',
      '**/.docusaurus/**',
      '**/webpack.config.js',
    ],
  },

  // JSON files - dependency checks
  {
    files: ['**/*.json'],
    languageOptions: {
      parser: jsoncParser,
    },
    plugins: {
      '@nx': nx,
    },
    rules: {
      '@nx/dependency-checks': [
        'error',
        {
          ignoredDependencies: ['typia', '@zerly/*'],
          buildTargets: ['build'],
          checkMissingDependencies: true,
          checkObsoleteDependencies: true,
          checkVersionMismatches: true,
          ignoredFiles: ['{projectRoot}/eslint.config.{js,cjs,mjs,ts,cts,mts}'],
          useLocalPathsForWorkspaceDependencies: true,
          peerDepsVersionStrategy: 'workspace', // or 'workspace' to use workspace:* for peer dependencies
        },
      ],
    },
  },

  // Rules for all JS/TS/JSX/TSX files
  {
    files: ['**/*.ts', '**/*.tsx', '**/*.js', '**/*.jsx'],
    plugins: {
      '@nx': nx,
      'prefer-arrow': preferArrowPlugin,
      prettier: eslintPluginPrettier,
      'unused-imports': unusedImports,
      'import-x': importPlugin,
    },
    rules: {
      // Nx-specific rules
      '@nx/enforce-module-boundaries': [
        'error',
        {
          allow: ['^.*/eslint(\\.base)?\\.config\\.[cm]?[jt]s$', '@zerly/core/**'],
          depConstraints: [
            {
              onlyDependOnLibsWithTags: ['*'],
              sourceTag: '*',
            },
          ],
          enforceBuildableLibDependency: true,
        },
      ],

      'array-bracket-spacing': ['error', 'never'],

      'arrow-spacing': 'error',
      // Naming conventions
      camelcase: ['error', { ignoreDestructuring: false, properties: 'never' }],

      'comma-dangle': ['error', 'always-multiline'],
      complexity: ['warn', 10],
      // Function structure rules
      'function-call-argument-newline': ['error', 'consistent'],
      'lines-between-class-members': [
        'error',
        {
          enforce: [
            { blankLine: 'always', next: 'method', prev: 'method' },
            { blankLine: 'always', next: 'method', prev: 'field' },
          ],
        },
        {
          exceptAfterSingleLine: true,
        },
      ],
      'max-depth': ['warn', 4],

      'max-params': ['warn', 4],
      'no-alert': 'error',
      // General code quality
      'no-console': ['warn', { allow: ['warn', 'error'] }],
      'no-debugger': 'error',
      'import-x/order': [
        'error',
        {
          groups: [
            'builtin', // Node.js built-ins (fs, path, etc.)
            'external', // npm packages (@nestjs/*, typia, rxjs, etc.)
            'internal', // Workspace aliases (@nestkit-x/*, @zerly/*)
            'parent', // Parent imports (../)
            'sibling', // Sibling imports (./)
            'index', // Index imports (./)
            'type', // Type-only imports
          ],
          pathGroups: [
            {
              pattern: '@nestjs/**',
              group: 'external',
              position: 'before',
            },
            {
              pattern: '@nestia/**',
              group: 'external',
              position: 'before',
            },
            {
              pattern: '@nestkit-x/**',
              group: 'internal',
              position: 'before',
            },
            {
              pattern: '@zerly/**',
              group: 'internal',
              position: 'before',
            },
          ],
          pathGroupsExcludedImportTypes: ['builtin', 'type'],
          'newlines-between': 'always',
          alphabetize: {
            order: 'asc',
            caseInsensitive: true,
          },
        },
      ],
      'import-x/newline-after-import': ['error', { count: 1 }],
      'import-x/no-duplicates': 'error',

      // Remove the base no-duplicate-imports since import-x/no-duplicates replaces it
      'no-duplicate-imports': 'off',

      'no-multiple-empty-lines': ['error', { max: 1, maxBOF: 0, maxEOF: 1 }],
      'no-useless-constructor': 'error',
      'no-useless-return': 'error',
      'no-var': 'error',
      'object-curly-spacing': ['error', 'always'],
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

      // Prefer arrow functions
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
      'prettier/prettier': ['error'],
      'template-curly-spacing': ['error', 'never'],
    },
  },

  // Rules specifically for TypeScript files
  {
    files: ['**/*.ts', '**/*.tsx'],
    languageOptions: {
      parser: tseslint.parser,
      parserOptions: {
        project: './tsconfig.base.json',
        tsconfigRootDir: import.meta.dirname,
      },
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
          // Allow prefixes/suffixes for special tokens
          filter: {
            match: true,
            regex:
              '^[A-Z][A-Z0-9_]*(_TOKEN|_KEY|_CONFIG)?$|^[a-z][a-zA-Z0-9]*$|^[A-Z][a-zA-Z0-9]*$',
          },
        },
        // 2. Other variables (non-const) - only camelCase
        {
          selector: 'variable',
          format: ['camelCase'],
        },
        // 3. Functions and parameters - camelCase
        {
          selector: 'function',
          format: ['camelCase'],
        },
        {
          selector: 'parameter',
          format: ['camelCase'],
          leadingUnderscore: 'allow', // Allow _unusedParam
        },
        // 4. Classes, interfaces, types, enums - PascalCase
        {
          selector: 'typeLike',
          format: ['PascalCase'],
        },
        {
          selector: 'enumMember',
          format: ['PascalCase', 'UPPER_CASE'],
        },
        {
          selector: 'interface',
          format: ['PascalCase'],
          prefix: ['I'], // Require "I" prefix
        },
        // 5. Object properties - allow everything (for HTTP headers, enum-objects, etc.)
        {
          selector: 'objectLiteralProperty',
          format: null,
        },
        // 6. Class methods and accessors - camelCase
        {
          selector: 'memberLike',
          modifiers: ['private'],
          format: ['camelCase'],
          leadingUnderscore: 'allow', // Private members allow leading underscore
        },
        {
          selector: 'memberLike',
          format: ['camelCase'],
        },
      ],
      '@typescript-eslint/no-confusing-void-expression': 'error',
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/no-namespace': 'off',
      '@typescript-eslint/no-non-null-assertion': 'error',
      '@typescript-eslint/no-redundant-type-constituents': 'error',
      '@typescript-eslint/no-unnecessary-condition': 'error',
      '@typescript-eslint/no-unused-vars': 'off',
      '@typescript-eslint/no-useless-empty-export': 'error',
      '@typescript-eslint/prefer-nullish-coalescing': 'error',
      '@typescript-eslint/prefer-optional-chain': 'error',
      '@typescript-eslint/prefer-readonly': 'error',
      '@typescript-eslint/prefer-string-starts-ends-with': 'error',
      '@typescript-eslint/switch-exhaustiveness-check': 'error',
      '@typescript-eslint/no-empty-function': 'off',

      camelcase: 'off',

      // Disable base rules that conflict with TS equivalents
      'comma-dangle': 'off',

      'no-duplicate-imports': 'off',
      'no-unused-vars': 'off',
      'no-useless-constructor': 'off',
      semi: 'off',
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
    },
  },

  // disable camel case for jest
  {
    files: ['jest.config.*'],
    rules: {
      '@typescript-eslint/naming-convention': 'off',
      camelcase: 'off',
    },
  },
];
