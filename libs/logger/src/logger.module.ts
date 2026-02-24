import { DynamicModule, Module } from '@nestjs/common';
import { ConfigModule } from '@nestjs/config';

import { LoggerModule as PinoModule } from 'nestjs-pino';
import { Params } from 'nestjs-pino/params';

import { LoggerConfigFactory } from './logger-config-factory.service';
import { LoggerProvider } from './logger.provider';

@Module({})
export class LoggerModule {
  public static forRoot(): DynamicModule {
    return {
      exports: [],
      imports: [
        ConfigModule,
        PinoModule.forRootAsync({
          imports: [ConfigModule],
          inject: [LoggerConfigFactory],
          providers: [LoggerConfigFactory],
          useFactory: (configFactory: LoggerConfigFactory): Params => configFactory.get(),
        }),
      ],
      module: LoggerModule,
      providers: [LoggerProvider],
    };
  }
}
