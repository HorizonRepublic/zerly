import { DynamicModule, Module } from '@nestjs/common';

@Module({
  controllers: [],
  exports: [],
  providers: [],
})
export class SwaggerModule {
  public static forRoot(): DynamicModule {
    return {
      module: SwaggerModule,
    };
  }
}
