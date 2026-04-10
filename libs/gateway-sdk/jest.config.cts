/**
 * Jest configuration for `@zerly/gateway-sdk`.
 *
 * This library runs in ESM mode because its specs need to import real
 * `@nestjs/common@12.0.0-alpha.2`, `@nestjs/core@12.0.0-alpha.2`, and
 * `@nestjs/microservices@12.0.0-alpha.2` — all three ship as pure ESM
 * (`"type": "module"`, ESM-only `exports` map).
 *
 * Sibling libraries still run under the default CommonJS jest preset. This
 * deviation is contained in this single config file and does not affect
 * `@zerly/errors`, `@zerly/kernel`, etc.
 *
 * Spec files MUST `import { jest } from '@jest/globals'` because the `jest`
 * global is only injected in CommonJS mode. `@jest/globals` is an explicit
 * devDependency at the repo root to ensure it is hoisted.
 */
module.exports = {
  displayName: 'gateway-sdk',
  preset: '../../jest.preset.js',
  testEnvironment: 'node',
  extensionsToTreatAsEsm: ['.ts'],
  transform: {
    '^.+\\.[tj]s$': [
      'ts-jest',
      {
        tsconfig: '<rootDir>/tsconfig.spec.json',
        useESM: true,
      },
    ],
  },
  moduleFileExtensions: ['ts', 'js', 'html'],
  moduleNameMapper: {
    '^(\\.{1,2}/.*)\\.js$': '$1',
  },
  coverageDirectory: '../../coverage/libs/gateway-sdk',
  passWithNoTests: true,
};
