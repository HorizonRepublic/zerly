import { resolve } from 'node:path';

import nx from '@nx/eslint-plugin';
import jsoncParser from 'jsonc-eslint-parser';
import { tsImport } from 'tsx/esm/api';

// Use own source directly to avoid circular: root → lib source → lib/eslint.config → root
const { defineNestjsConfig } = await tsImport('./src/index.ts', import.meta.url);

// tsconfigRootDir must point to the monorepo root, not the lib directory
const repoRoot = resolve(import.meta.dirname, '../..');

export default [
  ...nx.configs['flat/base'],
  ...nx.configs['flat/typescript'],
  ...nx.configs['flat/javascript'],

  {
    files: ['**/*.json'],
    languageOptions: { parser: jsoncParser },
    plugins: { '@nx': nx },
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

  {
    files: ['**/*.ts', '**/*.tsx', '**/*.js', '**/*.jsx'],
    plugins: { '@nx': nx },
    rules: {
      '@nx/enforce-module-boundaries': [
        'error',
        {
          allow: ['^.*/eslint(\\.base)?\\.config\\.[cm]?[jt]s$', '@zerly/core/**'],
          depConstraints: [{ onlyDependOnLibsWithTags: ['*'], sourceTag: '*' }],
          enforceBuildableLibDependency: true,
        },
      ],
    },
  },

  ...defineNestjsConfig({ tsconfigRootDir: repoRoot }),
];