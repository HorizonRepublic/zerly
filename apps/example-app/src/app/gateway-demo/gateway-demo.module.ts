import { Module } from '@nestjs/common';

import { GatewayDemoController } from './gateway-demo.controller';
import { GatewayDemoService } from './gateway-demo.service';

/**
 * Feature module exposing three `@GatewayRoute`-decorated demo endpoints.
 * @remarks
 * This module must be imported under a root module that also imports
 * `GatewayModule.forRoot(...)` so the SDK's response interceptor and
 * exception filter are available in the DI container.
 */
@Module({
  controllers: [GatewayDemoController],
  providers: [GatewayDemoService],
})
export class GatewayDemoModule {}
