import { DynamicModule, Module, Provider, Type } from '@nestjs/common';
import { APP_FILTER, DiscoveryService, MetadataScanner } from '@nestjs/core';

import { ConfigModule } from '@zerly/config';

import { appConfig } from './config/app.config';
import { AllExceptionsFilter } from './filters/all-exceptions.filter';
import { KernelProvider } from './providers/kernel.provider';
import { RoutesInspectorProvider } from './providers/routes-inspector.provider';
import { AppRefService } from './services/app-ref.service';
import { AppStateService } from './services/app-state.service';
import { APP_REF_SERVICE, APP_STATE_SERVICE } from './tokens';
import { IAppRefService, IAppStateService } from './types';

@Module({})
export class KernelModule {
  /**
   * Used for serving HTTP applications
   *
   * @param appModule
   */
  public static forServe(appModule: Type<unknown>): DynamicModule {
    return {
      global: true,
      imports: [ConfigModule.forRoot([appConfig]), appModule],
      exports: [APP_REF_SERVICE, APP_STATE_SERVICE],
      module: KernelModule,
      providers: [
        {
          provide: APP_STATE_SERVICE,
          useClass: AppStateService,
        } satisfies Provider<IAppStateService>,

        {
          provide: APP_REF_SERVICE,
          useClass: AppRefService,
        } satisfies Provider<IAppRefService>,
        {
          provide: APP_FILTER,
          useClass: AllExceptionsFilter,
        },

        DiscoveryService,
        MetadataScanner,

        RoutesInspectorProvider,
        KernelProvider,
      ],
    };
  }

  /**
   * Used for standalone applications
   *
   * @param appModule
   */
  public static forStandalone(appModule: Type<unknown>): DynamicModule {
    return {
      global: true,
      imports: [appModule],
      exports: [
        {
          provide: APP_REF_SERVICE,
          useClass: AppRefService,
        } satisfies Provider<IAppRefService>,

        {
          provide: APP_STATE_SERVICE,
          useClass: AppStateService,
        } satisfies Provider<IAppStateService>,
      ],
      module: KernelModule,
      providers: [
        {
          provide: APP_STATE_SERVICE,
          useClass: AppStateService,
        } satisfies Provider<IAppStateService>,

        {
          provide: APP_REF_SERVICE,
          useClass: AppRefService,
        } satisfies Provider<IAppRefService>,
      ],
    };
  }
}
