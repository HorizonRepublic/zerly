# @zerly/gateway-sdk

NestJS integration for the [`zerly-gateway-server`](../../apps/gateway-server). Adds the `@ApiGateway` decorator on top of `@MessagePattern`, publishing HTTP routing metadata to the `handler_registry` NATS KV bucket so the Go gateway can route real HTTP traffic to your Nest handlers via Core NATS RPC — without any HTTP boilerplate in the service itself.

## Why this exists

Nest microservices usually terminate their own HTTP listener, which spreads TLS, rate limiting, auth middleware, and observability across every service. With this SDK:

- Handlers are declared with one decorator — `@ApiGateway({ method, path, statusCode? })` — on top of the existing Nest `@Controller` / method shape
- The SDK registers route metadata in a single JetStream KV bucket; the Go gateway watches that bucket and rebuilds its routing table on every change
- You write normal Nest handlers (DI, interceptors, pipes, exception filters) that return plain values; the SDK normalises them into the wire envelope the gateway expects
- `HttpException` family throws from `@nestjs/common` (`NotFoundException`, `BadRequestException`, etc.) are recognised natively and mapped to the correct HTTP status without any custom error class

## Quick start

```ts
// app.module.ts
import { Module } from '@nestjs/common';
import { GatewayModule } from '@zerly/gateway-sdk';

@Module({
  imports: [
    GatewayModule.forRoot({
      isProduction: process.env['NODE_ENV'] === 'production',
    }),
    // ... your other imports
  ],
})
export class AppModule {}
```

```ts
// users.controller.ts
import { Controller } from '@nestjs/common';
import {
  ApiGateway,
  GatewayBody,
  GatewayParam,
  GatewayRequestId,
} from '@zerly/gateway-sdk';

@Controller()
export class UsersController {
  @ApiGateway({
    pattern: 'users.get',
    method: 'GET',
    path: '/users/:id',
  })
  public getUser(@GatewayParam('id') id: string) {
    return this.usersService.findById(id);
  }

  @ApiGateway({
    pattern: 'users.create',
    method: 'POST',
    path: '/users',
    statusCode: 201,
  })
  public createUser(
    @GatewayBody() dto: CreateUserDto,
    @GatewayRequestId() requestId: string,
  ) {
    return { ...this.usersService.create(dto), requestId };
  }

  @ApiGateway({
    pattern: 'users.delete',
    method: 'DELETE',
    path: '/users/:id',
  })
  public deleteUser(@GatewayParam('id') id: string): void {
    this.usersService.delete(id);
  }
}
```

That's the whole surface. The handler is a normal Nest method: no explicit response object, no HTTP framework coupling, just typed inputs and a return value.

## How it works

1. `@ApiGateway(options)` composes `@MessagePattern(pattern, { meta: { http: options } })`, locally attaches `GatewayResponseInterceptor` and `GatewayExceptionFilter`, and lets the `@horizon-republic/nestjs-jetstream` transport persist the HTTP metadata to the `handler_registry` JetStream KV bucket.
2. The interceptor wraps the handler's return value into the `IGatewayReply` envelope (`{ status, headers, body }`), using:
   - an explicit `statusCode` from the decorator when provided
   - `204 No Content` for `null` / `undefined` returns
   - `200 OK` otherwise
3. The exception filter catches throws and serialises them through `IErrorBodyFactory`. The default implementation recognises:
   - `@zerly/errors` `DomainException` via the `isDomainException` marker (duck-typed, no hard dep)
   - Any `HttpException` subclass via duck-typed `getStatus()` / `getResponse()` detection (also no hard dep)
   - Everything else → generic `500` with the original message hidden in production

## Parameter decorators

| Decorator | What it injects |
|---|---|
| `@GatewayBody()` | The parsed request body (`IGatewayRequest.body`) |
| `@GatewayParam(name)` | A single path parameter (`/users/:id`) |
| `@GatewayParams()` | The full path parameter map |
| `@GatewayQuery(name?)` | A single query value, or the whole query map |
| `@GatewayHeader(name)` | A single header value (case-insensitive) |
| `@GatewayRequestId()` | The gateway-generated request ID (monotonic ULID) |

All of them read from the envelope payload via a lightweight accessor — there is no reflection metadata to configure, no pipe setup, no DI plumbing.

## Customising normalization

The three normalization seams — reply builder, status resolver, error body factory — are bound to DI tokens and can be replaced without touching the interceptor or filter:

```ts
GatewayModule.forRoot({
  isProduction: true,
  replyBuilder: MyCustomReplyBuilder,       // implements IGatewayReplyBuilder
  statusResolver: MyCustomStatusResolver,   // implements IStatusResolver
  errorBodyFactory: MyCustomErrorFactory,   // implements IErrorBodyFactory
});
```

Each override accepts either a Nest provider class or an explicit instance. Defaults are used for any slot you leave unset.

## Requirements

- NestJS 11+ (`@nestjs/common`, `@nestjs/microservices`)
- `@horizon-republic/nestjs-jetstream` ≥ 2.9.0 (for the handler-metadata KV integration)
- A NATS server with JetStream enabled, reachable via the transport you configure

## Tests

```bash
pnpm nx test gateway-sdk
pnpm nx lint gateway-sdk
pnpm nx build gateway-sdk
```

The unit suite covers every normalization path plus a full integration test that runs the interceptor + filter + decorator against the real NestJS runtime via `Test.createTestingModule` under Jest ESM.
