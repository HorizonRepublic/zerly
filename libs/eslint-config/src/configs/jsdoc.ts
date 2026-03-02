import jsdoc from 'eslint-plugin-jsdoc';

import type { Linter } from 'eslint';

/**
 * JSDoc validation for TypeScript files.
 *
 * Only validates existing JSDoc — does not require it everywhere.
 * TypeScript handles types, so no `@param`/`@returns` type annotations needed.
 */
export const jsdocConfig: Linter.Config = {
  ...jsdoc.configs['flat/recommended-typescript'],
  files: ['**/*.ts', '**/*.tsx'],
  rules: {
    ...jsdoc.configs['flat/recommended-typescript'].rules,

    // --- Formatting & correctness (keep active) ---
    'jsdoc/check-access': 'warn',
    'jsdoc/check-alignment': 'warn',
    'jsdoc/check-param-names': 'warn',
    'jsdoc/check-tag-names': ['warn', { definedTags: ['final'] }],
    'jsdoc/empty-tags': 'warn',
    'jsdoc/multiline-blocks': 'warn',
    'jsdoc/no-multi-asterisks': 'warn',
    'jsdoc/tag-lines': 'warn',

    // TypeScript handles types — never duplicate them in JSDoc
    'jsdoc/no-types': 'error',
    'jsdoc/no-defaults': 'warn',

    // --- Do not require JSDoc on every declaration ---
    // JSDoc is encouraged, not mandatory
    'jsdoc/require-jsdoc': 'off',
    'jsdoc/require-param': 'off',
    'jsdoc/require-param-description': 'off',
    'jsdoc/require-returns': 'off',
    'jsdoc/require-returns-description': 'off',
    'jsdoc/require-property': 'off',
    'jsdoc/require-property-description': 'off',
    'jsdoc/require-yields': 'off',
    'jsdoc/require-yields-check': 'off',
    'jsdoc/require-yields-type': 'off',
    'jsdoc/require-throws-type': 'off',
    'jsdoc/require-next-type': 'off',
    'jsdoc/check-types': 'off',
    'jsdoc/check-values': 'off',
    'jsdoc/valid-types': 'off',

    // TypeScript type-system rules handled by @typescript-eslint
    'jsdoc/ts-no-empty-object-type': 'off',
    'jsdoc/reject-any-type': 'off',
    'jsdoc/reject-function-type': 'off',
  },
};
