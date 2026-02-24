import typia from 'typia';

import { APP_CONFIG, ConfigBuilder, Env, Environment, IAppConfig, LogLevel } from '@zerly/config';

class AppConfig implements IAppConfig {
  @Env('APP_NAME', {
    comment: 'kebab-case is recommended',
  })
  public readonly name: string = 'example-app';

  @Env('APP_ENV', {
    type: Environment,
    comment: 'App environment',
    default: Environment.Production,
  })
  public readonly env!: Environment;

  @Env('APP_HOST', {
    default: '0.0.0.0',
  })
  public readonly host!: string;

  @Env('APP_PORT', {
    type: Number,
    default: 3000,
  })
  public readonly port!: number;

  @Env('APP_LOG_LEVEL', {
    type: LogLevel,
    default: LogLevel.Info,
  })
  public logLever: LogLevel = LogLevel.Info;

  @Env('APP_GENERATE_ENV_EXAMPLE', {
    type: Boolean,
    comment: 'Use false in production',
    default: true,
  })
  public generateEnvExample!: boolean;
}

export const appConfig = ConfigBuilder.from(AppConfig, APP_CONFIG)
  .validate((c) => typia.misc.assertPrune<IAppConfig>(c))
  .build();
