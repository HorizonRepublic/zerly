import { ConfigFormat } from '../enums/config-format.enum';
import { EnvResolver } from '../resolvers/env.resolver';

describe('EnvResolver', () => {
  let resolver: EnvResolver;

  beforeEach(() => {
    resolver = new EnvResolver();
  });

  afterEach(() => {
    delete process.env['TEST_KEY'];
    delete process.env['TEST_ARRAY'];
  });

  it('should have dotenv format', () => {
    expect(resolver.format).toBe(ConfigFormat.Dotenv);
  });

  it('should return env var value as string', () => {
    process.env['TEST_KEY'] = '3000';
    expect(resolver.get('TEST_KEY')).toBe('3000');
  });

  it('should return undefined for missing key', () => {
    expect(resolver.get('NONEXISTENT_KEY')).toBeUndefined();
  });

  it('should return true for existing key via has()', () => {
    process.env['TEST_KEY'] = 'value';
    expect(resolver.has('TEST_KEY')).toBe(true);
  });

  it('should return false for missing key via has()', () => {
    expect(resolver.has('NONEXISTENT_KEY')).toBe(false);
  });

  it('should ignore filePath parameter', () => {
    process.env['TEST_KEY'] = 'value';
    expect(resolver.get('TEST_KEY', '/some/path.yaml')).toBe('value');
  });
});
