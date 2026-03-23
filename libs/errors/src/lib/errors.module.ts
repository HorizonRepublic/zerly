import { DynamicModule, Module } from '@nestjs/common';
import { APP_FILTER } from '@nestjs/core';

import { AllExceptionsFilter } from '../filters/all-exceptions.filter';
import { IErrorsModuleAsyncOptions } from '../interfaces/errors-module-async-options.interface';
import { ERROR_REPORTER } from '../tokens';

@Module({})
export class ErrorsModule {
  public static forRoot(): DynamicModule {
    return {
      global: true,
      module: ErrorsModule,
      providers: [
        {
          provide: APP_FILTER,
          useClass: AllExceptionsFilter,
        },
      ],
    };
  }

  public static forRootAsync(options: IErrorsModuleAsyncOptions): DynamicModule {
    return {
      global: true,
      module: ErrorsModule,
      imports: options.imports ?? [],
      providers: [
        {
          provide: APP_FILTER,
          useClass: AllExceptionsFilter,
        },
        {
          provide: ERROR_REPORTER,
          useFactory: options.useFactory,
          inject: options.inject ?? [],
        },
      ],
    };
  }
}
