import { join } from 'node:path';

import { ConfigFormat } from '../enums/config-format.enum';
import { YamlResolver } from '../resolvers/yaml.resolver';

const FIXTURES = join(__dirname, 'fixtures');
const VALID_YAML = join(FIXTURES, 'valid.yaml');
const INVALID_YAML = join(FIXTURES, 'invalid.yaml');
const EMPTY_YAML = join(FIXTURES, 'empty.yaml');

describe('YamlResolver', () => {
  it('should have yaml format', () => {
    const resolver = new YamlResolver(VALID_YAML);

    expect(resolver.format).toBe(ConfigFormat.Yaml);
  });

  describe('scalar values', () => {
    it('should resolve string by dot-path', () => {
      const resolver = new YamlResolver(VALID_YAML);

      expect(resolver.get('app.name')).toBe('test-app');
    });

    it('should resolve number by dot-path', () => {
      const resolver = new YamlResolver(VALID_YAML);

      expect(resolver.get('app.port')).toBe(3000);
    });

    it('should resolve boolean by dot-path', () => {
      const resolver = new YamlResolver(VALID_YAML);

      expect(resolver.get('app.debug')).toBe(true);
    });

    it('should return undefined for missing key', () => {
      const resolver = new YamlResolver(VALID_YAML);

      expect(resolver.get('app.nonexistent')).toBeUndefined();
    });

    it('should return undefined for partially matching path', () => {
      const resolver = new YamlResolver(VALID_YAML);

      expect(resolver.get('app.name.extra')).toBeUndefined();
    });
  });

  describe('structured values', () => {
    it('should resolve array of objects', () => {
      const resolver = new YamlResolver(VALID_YAML);

      expect(resolver.get('database.replicas')).toEqual([
        { host: 'replica1.db.com', port: 5432 },
        { host: 'replica2.db.com', port: 5433 },
      ]);
    });

    it('should resolve array of strings', () => {
      const resolver = new YamlResolver(VALID_YAML);

      expect(resolver.get('allowed_origins')).toEqual(['https://app.com', 'https://admin.app.com']);
    });

    it('should resolve nested object', () => {
      const resolver = new YamlResolver(VALID_YAML);

      expect(resolver.get('database')).toEqual({
        host: 'localhost',
        port: 5432,
        replicas: [
          { host: 'replica1.db.com', port: 5432 },
          { host: 'replica2.db.com', port: 5433 },
        ],
      });
    });
  });

  describe('has()', () => {
    it('should return true for existing path', () => {
      const resolver = new YamlResolver(VALID_YAML);

      expect(resolver.has('app.name')).toBe(true);
    });

    it('should return false for missing path', () => {
      const resolver = new YamlResolver(VALID_YAML);

      expect(resolver.has('app.missing')).toBe(false);
    });
  });

  describe('file path override', () => {
    it('should use override path instead of default', () => {
      const resolver = new YamlResolver('/nonexistent/default.yaml');

      expect(resolver.get('app.name', VALID_YAML)).toBe('test-app');
    });
  });

  describe('caching', () => {
    it('should cache parsed YAML per file path', () => {
      const resolver = new YamlResolver(VALID_YAML);
      const result1 = resolver.get('app.name');
      const result2 = resolver.get('app.port');

      expect(result1).toBe('test-app');
      expect(result2).toBe(3000);
    });
  });

  describe('error handling', () => {
    it('should throw on missing file', () => {
      const resolver = new YamlResolver('/nonexistent/file.yaml');

      expect(() => resolver.get('any.key')).toThrow(/not found/i);
    });

    it('should throw on invalid YAML', () => {
      const resolver = new YamlResolver(INVALID_YAML);

      expect(() => resolver.get('any.key')).toThrow(/parse/i);
    });

    it('should return undefined for keys in empty file', () => {
      const resolver = new YamlResolver(EMPTY_YAML);

      expect(resolver.get('any.key')).toBeUndefined();
    });
  });
});
