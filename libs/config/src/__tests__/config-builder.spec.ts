import 'reflect-metadata';

// Mock @nestjs/config to avoid ESM-only @nestjs/common transitive import.
// Our mock `registerAs` mirrors the real one: attaches metadata and returns the factory.
jest.mock('@nestjs/config', () => ({
  registerAs: (_token: string | symbol, factory: () => unknown) => factory,
}));

import { ConfigRegistry } from '../config.registry';
import { Env } from '../decorators/env.decorator';
import { ConfigFormat } from '../enums/config-format.enum';
import { ConfigBuilder } from '../helpers/config-builder.helper';
import { IConfigResolver } from '../resolvers/config-resolver.interface';

const TEST_TOKEN = Symbol('test-config');

class TestConfig {
  @Env('app.host', { default: 'localhost' })
  host!: string;

  @Env('app.port', { type: Number, default: 3000 })
  port!: number;

  @Env('app.debug', { type: Boolean, default: false })
  debug!: boolean;
}

class ArrayConfig {
  @Env('app.origins', { type: Array, default: [] })
  origins!: unknown[];
}

function createMockResolver(
  data: Record<string, unknown>,
  format: ConfigFormat = ConfigFormat.Yaml,
): IConfigResolver {
  return {
    format,
    get: (key: string) => data[key],
    has: (key: string) => key in data,
  };
}

describe('ConfigBuilder', () => {
  afterEach(() => {
    ConfigRegistry.reset();
  });

  describe('lazy evaluation', () => {
    it('should not call initializeConfig at build() time', () => {
      // build() should return a factory without resolving values
      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      expect(typeof factory).toBe('function');
      // No error thrown — resolver not needed yet
    });

    it('should resolve values when factory is invoked', () => {
      ConfigRegistry.setResolver(
        createMockResolver({ 'app.host': 'example.com', 'app.port': 8080, 'app.debug': true }),
      );

      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      const result = factory() as TestConfig;

      expect(result.host).toBe('example.com');
      expect(result.port).toBe(8080);
      expect(result.debug).toBe(true);
    });

    it('should use defaults when resolver returns undefined', () => {
      ConfigRegistry.setResolver(createMockResolver({}));

      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      const result = factory() as TestConfig;

      expect(result.host).toBe('localhost');
      expect(result.port).toBe(3000);
      expect(result.debug).toBe(false);
    });
  });

  describe('dotenv format', () => {
    it('should convert string to number', () => {
      ConfigRegistry.setResolver(
        createMockResolver({ 'app.host': 'h', 'app.port': '8080', 'app.debug': 'false' }, ConfigFormat.Dotenv),
      );

      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      const result = factory() as TestConfig;

      expect(result.port).toBe(8080);
      expect(typeof result.port).toBe('number');
    });
  });

  describe('yaml format — arrays', () => {
    it('should accept array values as-is from yaml resolver', () => {
      ConfigRegistry.setResolver(
        createMockResolver({ 'app.origins': ['https://a.com', 'https://b.com'] }),
      );

      const factory = ConfigBuilder.from(ArrayConfig, Symbol('arr')).build();
      const result = factory() as ArrayConfig;

      expect(result.origins).toEqual(['https://a.com', 'https://b.com']);
    });
  });

  describe('validation', () => {
    it('should run validator on resolved config', () => {
      ConfigRegistry.setResolver(createMockResolver({}));

      const validator = jest.fn((c: TestConfig) => c);
      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).validate(validator).build();
      factory();

      expect(validator).toHaveBeenCalledTimes(1);
    });
  });

  describe('frozen output', () => {
    it('should return a frozen object', () => {
      ConfigRegistry.setResolver(createMockResolver({}));

      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      const result = factory();

      expect(Object.isFrozen(result)).toBe(true);
    });
  });

  describe('file path from registry', () => {
    it('should pass file path from registry to resolver', () => {
      const mockResolver = createMockResolver({});
      const getSpy = jest.spyOn(mockResolver, 'get');

      ConfigRegistry.setResolver(mockResolver);
      ConfigRegistry.setFilePath(TEST_TOKEN, '/custom/path.yaml');

      const factory = ConfigBuilder.from(TestConfig, TEST_TOKEN).build();
      factory();

      // Each @Env field should receive the file path
      expect(getSpy).toHaveBeenCalledWith('app.host', '/custom/path.yaml');
    });
  });
});
