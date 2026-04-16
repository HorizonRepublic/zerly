// @ts-nocheck — bench stack has its own tsconfig + npm install happens inside docker
import { Module } from '@nestjs/common';

import { JetstreamModule } from '@horizon-republic/nestjs-jetstream';

import { GatewayModule } from '@zerly/gateway-sdk';

import { BenchController } from './bench.controller';

/**
 * Root bench module. Two dynamic imports:
 *
 *  - `JetstreamModule.forRoot` configures the NATS JetStream
 *    transport strategy used by `@GatewayRoute` under the hood.
 *    `servers` points at the in-compose NATS container.
 *
 *  - `GatewayModule.forRoot()` installs the SDK's reply builder,
 *    status resolver and error body factory — without it the
 *    response interceptor / exception filter attached by
 *    `@GatewayRoute` have no DI tokens to resolve and the app
 *    refuses to start.
 *
 * The bench-app deliberately avoids every other example-app piece
 * (logger, config, kernel, bootstrap providers) so the numbers
 * reflect gateway-sdk cost, not framework scaffolding.
 */
@Module({
  imports: [
    JetstreamModule.forRoot({
      name: 'bench-zerly',
      servers: ['nats://nats:4222'],
    }),
    GatewayModule.forRoot(),
  ],
  controllers: [BenchController],
})
export class AppModule {}
