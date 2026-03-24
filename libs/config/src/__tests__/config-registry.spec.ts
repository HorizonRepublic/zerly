import { ConfigRegistry } from '../config.registry';
import { EnvResolver } from '../resolvers/env.resolver';

describe('ConfigRegistry', () => {
  afterEach(() => {
    ConfigRegistry.reset();
  });

  it('should store and retrieve resolver', () => {
    const resolver = new EnvResolver();
    ConfigRegistry.setResolver(resolver);
    expect(ConfigRegistry.getResolver()).toBe(resolver);
  });

  it('should throw when getting resolver before setting', () => {
    expect(() => ConfigRegistry.getResolver()).toThrow(/not initialized/i);
  });

  it('should store and retrieve file paths by token', () => {
    const token = Symbol('test');
    ConfigRegistry.setFilePath(token, '/path/to/config.yaml');
    expect(ConfigRegistry.getFilePath(token)).toBe('/path/to/config.yaml');
  });

  it('should return undefined for unregistered token file path', () => {
    expect(ConfigRegistry.getFilePath(Symbol('unknown'))).toBeUndefined();
  });

  it('should clear all state on reset', () => {
    const resolver = new EnvResolver();
    const token = Symbol('test');
    ConfigRegistry.setResolver(resolver);
    ConfigRegistry.setFilePath(token, '/path');
    ConfigRegistry.reset();
    expect(() => ConfigRegistry.getResolver()).toThrow();
    expect(ConfigRegistry.getFilePath(token)).toBeUndefined();
  });
});
