import typia from 'typia';

import { APP_CONFIG, ConfigBuilder, Env, Environment, IAppConfig, LogLevel } from '@zerly/config';

class AppConfig implements IAppConfig {
  @Env('application.name', {
    comment: 'kebab-case is recommended',
    example: 'example-app',
  })
  public readonly name: string = 'example-app';

  @Env('application.env', {
    type: Environment,
    comment: 'App environment',
    example: Environment.Production,
  })
  public readonly env!: Environment;

  @Env('application.host', { example: '0.0.0.0' })
  public readonly host!: string;

  @Env('application.port', { type: Number, example: 3000 })
  public readonly port!: number;

  @Env('application.log_level', {
    type: LogLevel,
    default: LogLevel.Info,
  })
  public logLevel: LogLevel = LogLevel.Info;

  @Env('application.generate_env_example', {
    type: Boolean,
    comment: 'Use false in production',
    default: true,
  })
  public generateEnvExample!: boolean;
}

export const appConfig = ConfigBuilder.from(AppConfig, APP_CONFIG)
  .validate((c) => typia.misc.assertPrune<IAppConfig>(c))
  .build();
