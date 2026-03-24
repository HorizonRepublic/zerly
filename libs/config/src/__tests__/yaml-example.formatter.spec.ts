import { IConfigSection } from '../formatters/example-formatter.interface';
import { YamlExampleFormatter } from '../formatters/yaml-example.formatter';

describe('YamlExampleFormatter', () => {
  const formatter = new YamlExampleFormatter();

  it('should have .env.example.yaml as file name', () => {
    expect(formatter.fileName).toBe('.env.example.yaml');
  });

  it('should reconstruct nesting from dot-paths', () => {
    const sections: IConfigSection[] = [
      {
        title: 'app',
        fields: [
          { key: 'app.name', propertyKey: 'name', options: { default: 'my-app' } },
          { key: 'app.port', propertyKey: 'port', options: { default: 3000, type: Number } },
        ],
        instance: { name: 'my-app', port: 3000 },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('app:');
    expect(result).toContain('  name: "my-app"');
    expect(result).toContain('  port: 3000');
  });

  it('should handle array examples', () => {
    const sections: IConfigSection[] = [
      {
        title: 'db',
        fields: [
          {
            key: 'database.replicas',
            propertyKey: 'replicas',
            options: {
              type: Array,
              example: [{ host: 'localhost', port: 5432 }],
            },
          },
        ],
        instance: { replicas: [] },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('database:');
    expect(result).toContain('replicas:');
    expect(result).toContain('host:');
  });

  it('should output empty array when no example provided for Array type', () => {
    const sections: IConfigSection[] = [
      {
        title: 'db',
        fields: [
          {
            key: 'database.replicas',
            propertyKey: 'replicas',
            options: { type: Array },
          },
        ],
        instance: { replicas: [] },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('replicas: []');
  });

  it('should include comments for descriptions', () => {
    const sections: IConfigSection[] = [
      {
        title: 'app',
        fields: [
          {
            key: 'app.name',
            propertyKey: 'name',
            options: { default: 'app', description: 'App name' },
          },
        ],
        instance: { name: 'app' },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('# App name');
  });

  it('should include auto-generated header', () => {
    const result = formatter.format([]);

    expect(result).toContain('auto-generated');
  });
});
