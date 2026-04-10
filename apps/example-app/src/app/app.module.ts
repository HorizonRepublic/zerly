import { Module } from '@nestjs/common';

import { JetstreamModule } from '@horizon-republic/nestjs-jetstream';

import { GatewayModule } from '@zerly/gateway-sdk';
import { LoggerModule } from '@zerly/logger';

import { AppController } from './app.controller';
import { AppService } from './app.service';
import { GatewayDemoModule } from './gateway-demo/gateway-demo.module';
import { SubModule } from './submodule/sub.module';

@Module({
  controllers: [AppController],
  imports: [
    LoggerModule.forRoot(),

    // transport
    JetstreamModule.forRoot({
      name: 'example-app',
      servers: ['nats://localhost:4222'],
    }),

    // gateway SDK
    GatewayModule.forRoot({
      isProduction: process.env['NODE_ENV'] === 'production',
    }),

    // app layer
    SubModule,
    GatewayDemoModule,
  ],
  providers: [AppService],
})
export class AppModule {}
