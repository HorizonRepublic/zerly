import { Module } from '@nestjs/common';

import { AuthVerifierController } from './auth-verifier.controller';
import { GatewayContractDemoController } from './gateway-contract-demo.controller';
import { GatewayDemoController } from './gateway-demo.controller';
import { GatewayDemoService } from './gateway-demo.service';

/**
 * Feature module exposing the `@GatewayRoute`-decorated demo endpoints
 * plus a toy `@GatewayAuthVerifier` for the gateway auth contract demo.
 * @remarks
 * This module must be imported under a root module that also imports
 * `GatewayModule.forRoot(...)` so the SDK's response interceptor and
 * exception filter are available in the DI container. The auth verifier
 * is registered as the default so protected routes in
 * `GatewayDemoController` can opt in with a bare `auth: true`.
 */
@Module({
  controllers: [AuthVerifierController, GatewayDemoController, GatewayContractDemoController],
  providers: [GatewayDemoService],
})
export class GatewayDemoModule {}
