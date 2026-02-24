import { DynamicModule, Module } from '@nestjs/common';
import { ConfigFactory, ConfigModule as BaseConfigModule } from '@nestjs/config';
import { ConfigModuleOptions } from '@nestjs/config/dist/interfaces/config-module-options.interface';

import { EnvExampleProvider } from './providers/env-example.provider';

@Module({})
export class ConfigModule {
  public static forRoot(load: ConfigModuleOptions['load'] = []): DynamicModule {
    return {
      module: ConfigModule,
      global: true,
      imports: [
        BaseConfigModule.forRoot({
          cache: true,
          isGlobal: true,
          expandVariables: true,
          load,
        }),
      ],
      providers: [EnvExampleProvider],
      exports: [BaseConfigModule],
    };
  }

  public static forFeature(config: ConfigFactory): DynamicModule {
    return {
      module: ConfigModule,
      imports: [BaseConfigModule.forFeature(config)],
      exports: [BaseConfigModule],
    };
  }
}
