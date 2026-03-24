import { EnvExampleFormatter } from '../formatters/env-example.formatter';
import { IConfigSection } from '../formatters/example-formatter.interface';

describe('EnvExampleFormatter', () => {
  const formatter = new EnvExampleFormatter();

  it('should have .env.example as file name', () => {
    expect(formatter.fileName).toBe('.env.example');
  });

  it('should format a simple section', () => {
    const sections: IConfigSection[] = [
      {
        title: 'app',
        fields: [
          { key: 'APP_PORT', propertyKey: 'port', options: { default: 3000, type: Number } },
          { key: 'APP_NAME', propertyKey: 'name', options: { description: 'Application name' } },
        ],
        instance: { port: 3000, name: 'my-app' },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('# -- app');
    expect(result).toContain('APP_PORT="3000"');
    expect(result).toContain('APP_NAME="my-app"');
    expect(result).toContain('Application name');
  });

  it('should mark required fields', () => {
    const sections: IConfigSection[] = [
      {
        title: 'db',
        fields: [
          { key: 'DB_HOST', propertyKey: 'host', options: {} },
        ],
        instance: { host: undefined },
      },
    ];

    const result = formatter.format(sections);

    expect(result).toContain('REQUIRED');
  });

  it('should include auto-generated header', () => {
    const result = formatter.format([]);
    expect(result).toContain('auto-generated');
  });
});
