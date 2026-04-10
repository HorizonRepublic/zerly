# @zerly/gateway-sdk

> **Status:** scaffolding in progress. Public API below is the **planned** shape and is not yet exported by the package. A fully functional release lands in a subsequent version; until then, treat this README as a preview of the upcoming API.

NestJS integration for [`zerly-gateway-server`](../../apps/gateway-server). Adds the `@ApiGateway` decorator on top of `@MessagePattern`, publishing HTTP routing metadata to the `handler_registry` NATS KV bucket so the Go gateway can route HTTP traffic to your Nest handlers via Core NATS RPC.

See the [design specification](../../docs/superpowers/specs/2026-04-10-zerly-gateway-design.md) for architecture details.

## Planned API (preview)

```ts
import { ApiGateway, GatewayBody, GatewayModule } from '@zerly/gateway-sdk';

@Module({
  imports: [GatewayModule.forRoot({ isProduction: process.env.NODE_ENV === 'production' })],
})
export class AppModule {}

@Controller()
export class UsersController {
  @ApiGateway({ pattern: 'users.create', method: 'POST', path: '/users', statusCode: 201 })
  createUser(@GatewayBody() dto: CreateUserDto) {
    return this.usersService.create(dto);
  }
}
```
