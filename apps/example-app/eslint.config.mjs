import nestjsTyped from '@darraghor/eslint-plugin-nestjs-typed';

import baseConfig from '../../eslint.config.mjs';

export default [
  ...baseConfig,
  {
    files: ['src/**/*.ts'],
    languageOptions: nestjsTyped.configs.flatRecommended[0].languageOptions,
    plugins: nestjsTyped.configs.flatRecommended[0].plugins,
    rules: {
      ...nestjsTyped.configs.flatRecommended[1].rules,
      ...nestjsTyped.configs.flatNoSwagger[0].rules,
      '@darraghor/nestjs-typed/sort-module-metadata-arrays': 'off',
    },
  },
];
