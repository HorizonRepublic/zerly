import { DynamicModule, Module } from '@nestjs/common';

import { MikroOrmModule } from '@mikro-orm/nestjs';

@Module({})
export class DatabaseModule {
  public forRoot(): DynamicModule {
    return {
      module: DatabaseModule,
      global: false,
      imports: [MikroOrmModule.forRoot()],
    };
  }
}
