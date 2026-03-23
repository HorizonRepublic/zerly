import nx from '@nx/eslint-plugin';
import jsoncParser from 'jsonc-eslint-parser';
import { tsImport } from 'tsx/esm/api';

// Import TypeScript source directly — no build step needed
const { defineNestjsConfig } = await tsImport('./libs/eslint-config/src/index.ts', import.meta.url);

export default [
  // Nx base configurations
  ...nx.configs['flat/base'],
  ...nx.configs['flat/typescript'],
  ...nx.configs['flat/javascript'],

  // JSON files — Nx dependency checks
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
          peerDepsVersionStrategy: 'workspace',
        },
      ],
    },
  },

  // Nx module boundaries
  {
    files: ['**/*.ts', '**/*.tsx', '**/*.js', '**/*.jsx'],
    plugins: {
      '@nx': nx,
    },
    rules: {
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
    },
  },

  // Shared NestJS preset from @zerly/eslint-config
  ...defineNestjsConfig({ tsconfigRootDir: import.meta.dirname }),
];