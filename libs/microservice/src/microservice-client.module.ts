import { DynamicModule, Module } from '@nestjs/common';

import { JetstreamClientModule } from '@horizon-republic/nestjs-jetstream';

import { IMicroserviceModuleOptions } from './types/microservice-module.options';

@Module({})
export class MicroserviceClientModule {
  public static forRoot(options: IMicroserviceModuleOptions): DynamicModule {
    return {
      module: MicroserviceClientModule,
      imports: [JetstreamClientModule.forFeature(options)],
    };
  }
}
