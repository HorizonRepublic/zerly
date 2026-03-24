import { ConfigFormat } from '../enums/config-format.enum';
import { ConfigResolverFactory } from '../resolvers/config-resolver.factory';
import { EnvResolver } from '../resolvers/env.resolver';
import { YamlResolver } from '../resolvers/yaml.resolver';

describe('ConfigResolverFactory', () => {
  it('should create EnvResolver for dotenv format', () => {
    const resolver = ConfigResolverFactory.create(ConfigFormat.Dotenv);

    expect(resolver).toBeInstanceOf(EnvResolver);
  });

  it('should create YamlResolver for yaml format', () => {
    const resolver = ConfigResolverFactory.create(ConfigFormat.Yaml, '/some/path.yaml');

    expect(resolver).toBeInstanceOf(YamlResolver);
  });

  it('should create EnvResolver when no format specified', () => {
    const resolver = ConfigResolverFactory.create();

    expect(resolver).toBeInstanceOf(EnvResolver);
  });
});
