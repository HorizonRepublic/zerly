# @zerly/kernel

The core bootstrap layer of the **Zerly** ecosystem.

`@zerly/kernel` provides an opinionated and secure bootstrap mechanism for NestJS applications. It replaces the typical `main.ts` boilerplate and enforces a consistent initialization flow for both HTTP servers and CLI or standalone processes.

## Key Features

- **Unified Entry Point:** Initialize an application with a single call.
- **Dual Mode:** Run either as an **HTTP server** (default) or in **Standalone or CLI** mode using a flag.
- **Lifecycle Management:** Lifecycle hooks (`onCreated`, `onListening`) for controlled initialization of infrastructure concerns such as Swagger or microservices.
- **Fastify by Default:** Uses `FastifyAdapter` with predefined limits and security-related options.

## Installation

```bash
pnpm add @zerly/kernel
```

## Usage

### 1. Bootstrap your application

Replace the contents of your `main.ts` with the following:

```typescript
import { Kernel } from '@zerly/kernel';
import { AppModule } from './app/app.module'; // <-- your root module

Kernel.init(AppModule);
```

Kernel is responsible for creating the application instance, configuring the adapter, and handling startup.

### 2. Run in HTTP Mode (Default)

Start the application normally. The kernel creates the NestJS application, initializes the Fastify adapter, and begins listening on the configured address.

```bash
node dist/apps/my-app/main.js
```

Output:

```shell
Application is listening on http://0.0.0.0:3000
```

### 3. Run in CLI or Standalone Mode
You can also run the application in only **CLI Mode**:
```typescript
import { Kernel } from '@zerly/kernel';
import { AppModule } from './app/app.module'; // <-- your root module

Kernel.standalone(AppModule);
```

Or pass the `--cli` flag to usual app to start the application in **Standalone Mode**.  
In this mode the HTTP server is not started.

This is intended for:

- **System Scheduled Tasks:** Jobs triggered by OS cron, Kubernetes CronJobs, or cloud schedulers.
- **CLI Utilities:** Database migrations, seeding scripts, or administrative commands.
- **One-off Scripts:** CI/CD pipeline tasks.

```bash
node dist/apps/my-app/main.js --cli my-command
```

> **Note:** While Standalone mode is typically used for short-lived processes, it can also be used for long-running background workers by keeping the process alive.

## Advanced Features

### Lifecycle Hooks (AppStateService)

`@zerly/kernel` exposes an `AppStateService` that allows you to hook into specific stages of the application lifecycle. This is the recommended way to register integrations that require an `INestApplication` instance, such as Swagger, Helmet, or microservices.

Inject `APP_STATE_SERVICE` into your providers:

```typescript
import { Inject, Injectable } from '@nestjs/common';
import { APP_STATE_SERVICE, IAppStateService } from '@zerly/core';

@Injectable()
export class AppSetupProvider {
  constructor(
    @Inject(APP_STATE_SERVICE)
    private readonly appState: IAppStateService,
  ) {
    // Executed after the app is created, but before it starts listening
    this.appState.onCreated((app) => {
      const config = new DocumentBuilder().setTitle('My API').build();
      const document = SwaggerModule.createDocument(app, config);
      SwaggerModule.setup('api', app, document);

      app.enableCors();
    });

    // Executed after the app has started listening
    this.appState.onListening(() => {
      console.log('Microservices and WebSockets are ready');
    });
  }
}
```

### Accessing the App Reference

If access to the `INestApplication` instance is required outside of the bootstrap flow, `AppRefService` can be used.

> ⚠️ **Warning:** Prefer lifecycle hooks and dependency injection where possible.

```typescript
import { Inject } from '@nestjs/common';
import { APP_REF_SERVICE, IAppRefService } from '@zerly/core';

export class MyService {
  constructor(@Inject(APP_REF_SERVICE) private readonly appRef: IAppRefService) {
  }

  someMethod() {
    const app = this.appRef.get(); // Returns INestApplication
  }
}
```

### Environment Configuration

The Kernel integrates with `@zerly/config` and can generate a `.env.example` file based on configuration classes when the application starts.

## Frequently Asked Questions

### Can I replace the underlying HTTP adapter?

**No.** The kernel standardizes on **FastifyAdapter**. This ensures consistent behavior, performance characteristics, and security defaults across the ecosystem. Adapter selection may be revisited in future major versions.

### Why can't I register microservices or middlewares directly in `main.ts`?

The bootstrap process is structured around lifecycle hooks rather than imperative setup in `main.ts`.

This allows you to:

- Keep the entry point minimal and consistent across services.
- Move infrastructure configuration (Swagger, Helmet, WebSockets) into dedicated providers.
- Control initialization order through the `AppStateService`.

Use `onCreated` to register global middlewares and documentation, and `onListening` for post-startup tasks.

### How do I control the execution order of multiple lifecycle hooks?

`AppStateService` supports priorities. When registering a callback via `.onCreated()` or `.onListening()`, you can pass a second argument representing the priority (default is `0`).

Lower numbers run earlier.  
Higher numbers run later.

### Where do I configure the listening port and host?

Configuration is provided through environment variables using `@zerly/config`.

- `APP_PORT` controls the port (default: `3000`)
- `APP_HOST` controls the bind address (default: `0.0.0.0`)

If no `.env` file exists, the kernel generates a `.env.example` file with defaults.

### How do I register global Pipes, Guards, or Interceptors?

#### Declarative (recommended)

Use the standard NestJS `APP_PIPE`, `APP_GUARD`, and `APP_INTERCEPTOR` providers in your modules.

#### Imperative

Use the `onCreated` hook and call `app.useGlobalPipes()` or related methods on the application instance.

## Roadmap

- [ ] WebSockets support

## License

This project is part of the **Zerly** ecosystem.  
MIT licensed.
