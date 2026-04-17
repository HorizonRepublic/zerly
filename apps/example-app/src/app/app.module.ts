import { Module } from '@nestjs/common';

import { JetstreamModule } from '@horizon-republic/nestjs-jetstream';

import { GatewayModule } from '@zerly/gateway-sdk';
import { LoggerModule } from '@zerly/logger';

import { AppController } from './app.controller';
import { AppService } from './app.service';
import { GatewayDemoModule } from './gateway-demo/gateway-demo.module';
import { SubModule } from './submodule/sub.module';
import { JetstreamBootstrapProvider } from './transport/jetstream-bootstrap.provider';

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
      defaults: {
        headers: { 'x-frame-options': 'DENY' },
        cookies: {
          httpOnly: true,
          sameSite: 'lax',
          path: '/',
          maxAge: 7200,
          secure: false,
        },
        timeout: 30_000,
      },
    }),

    // app layer
    SubModule,
    GatewayDemoModule,
  ],
  providers: [AppService, JetstreamBootstrapProvider],
})
export class AppModule {}
