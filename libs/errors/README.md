# @zerly/errors

Domain error handling library for the **Zerly** ecosystem.

Provides a structured exception hierarchy, a consistent HTTP response shape, and a global `AllExceptionsFilter` that handles HTTP and RPC contexts uniformly.

## Key Features

- **Domain codes** — `Uppercase<string>` codes (`USER_NOT_FOUND`, `ORDER_ALREADY_PAID`) instead of raw HTTP statuses
- **No `message` in response** — `code` + `details` is self-sufficient; frontends map codes to localized strings
- **Prod / dev split** — `internal` field with diagnostic data appears only outside production
- **RPC transparency** — `DomainException` serializes to a typed payload that survives NATS transport and is re-hydrated at the API gateway
- **Opt-in observability** — `IErrorReporter` integration (OTel, Sentry) registered via `forRootAsync`, zero default overhead
- **typia adapter** — `TypeGuardError` is automatically converted to `VALIDATION_FAILED`

---

## Installation

```bash
pnpm add @zerly/errors
```

Peer dependencies: `@nestjs/common`, `@nestjs/config`, `@nestjs/core`, `rxjs`.
Optional peer: `@nestjs/microservices` (required for RPC payload wrapping).

---

## HTTP Response Shape

```typescript
interface IErrorResponse {
  code: Uppercase<string>;
  details?: Record<string, unknown>; // always included when set by the developer
  internal?: Record<string, unknown>; // non-production only
  timestamp: string;
  requestId: string | null;          // null until CLS is implemented
}
```

**Production:**
```json
{ "code": "ORDER_ALREADY_PAID", "details": { "orderId": "abc" }, "timestamp": "...", "requestId": null }
```

**Development (extra `internal` field):**
```json
{ "code": "EXCEL_VALIDATION_FAILED", "details": { "rows": [5, 12] }, "internal": { "rawCsv": "..." }, "timestamp": "...", "requestId": null }
```

**Unknown error in production:**
```json
{ "code": "INTERNAL_SERVER_ERROR", "timestamp": "...", "requestId": null }
```

**Unknown error in development:**
```json
{ "code": "INTERNAL_SERVER_ERROR", "internal": { "message": "relation 'users' does not exist" }, "timestamp": "...", "requestId": null }
```

---

## Quick Start

### Register in the application

`@zerly/errors` is **not** registered automatically by `@zerly/kernel`. Import it explicitly in your root module — this lets you choose between `forRoot()` and `forRootAsync()` depending on whether you need an error reporter.

> **Note:** Registering `AllExceptionsFilter` replaces the default NestJS error format for **all** exceptions, including built-in `HttpException` subclasses (`NotFoundException`, `BadRequestException`, etc.). Their `message` field is dropped; only `code` + optional `details` are returned.

```typescript
import { ErrorsModule } from '@zerly/errors';

@Module({
  imports: [ErrorsModule.forRoot()],
})
export class AppModule {}
```

---

## Exception Hierarchy

### `DomainException` (abstract base)

```typescript
import { HttpStatus } from '@nestjs/common';
import { DomainException } from '@zerly/errors';

export class UserNotFoundException extends DomainException {
  public override readonly code = 'USER_NOT_FOUND' as const;
  public override readonly httpStatus = HttpStatus.NOT_FOUND;

  public constructor(userId: string) {
    super({ details: { userId } });
  }
}
```

```typescript
throw new UserNotFoundException('user-123');
// → 404  { "code": "USER_NOT_FOUND", "details": { "userId": "user-123" } }
```

### `ApiException` (concrete, for quick throws)

No subclassing required when the exception is local or one-off:

```typescript
import { HttpStatus } from '@nestjs/common';
import { ApiException } from '@zerly/errors';

// Basic
throw new ApiException('ORDER_ALREADY_PAID', {
  httpStatus: HttpStatus.CONFLICT,
  details: { orderId },
});

// With dev-only diagnostic context
throw new ApiException('EXCEL_VALIDATION_FAILED', {
  httpStatus: HttpStatus.UNPROCESSABLE_ENTITY,
  details: { rows: [5, 12, 34] },
  internalDetails: { rawCsv: chunk }, // excluded in production
});
```

### Constructor options

```typescript
interface IDomainExceptionOptions {
  httpStatus?: HttpStatus;                   // default: 400
  details?: Record<string, unknown>;         // always in response
  internalDetails?: Record<string, unknown>; // non-production only → internal
}
```

---

## Standard HTTP → Code Mapping

The filter automatically maps plain `HttpException` instances to reserved codes:

| Status | Code                    |
|--------|-------------------------|
| 400    | `BAD_REQUEST`           |
| 401    | `UNAUTHORIZED`          |
| 403    | `FORBIDDEN`             |
| 404    | `NOT_FOUND`             |
| 409    | `CONFLICT`              |
| 422    | `UNPROCESSABLE_ENTITY`  |
| 429    | `TOO_MANY_REQUESTS`     |
| 500    | `INTERNAL_SERVER_ERROR` |
| 503    | `SERVICE_UNAVAILABLE`   |
| 504    | `GATEWAY_TIMEOUT`       |

Domain codes must not collide with these reserved names. Use compound names (`USER_NOT_FOUND`, `ORDER_ALREADY_PAID`).

---

## RPC Support

`DomainException` can be serialized to a typed payload for NATS transport and re-hydrated at the API gateway.

**Microservice (throws):**
```typescript
throw new UserNotFoundException(userId);
// AllExceptionsFilter wraps it in RpcException({ isDomainException: true, code, httpStatus, details })
```

**API Gateway (re-hydrates):**
```typescript
// AllExceptionsFilter on the gateway calls ApiException.fromRpcPayload(rpcError.getError())
// and maps it back to the correct HTTP response automatically
```

Manual re-hydration (if needed outside the filter):
```typescript
import { ApiException } from '@zerly/errors';

const exception = ApiException.fromRpcPayload(rpcError.getError());
if (exception) {
  // exception.code, exception.httpStatus, exception.details are all restored
}
```

---

## Observability (OTel, Sentry)

The reporter is fully opt-in. Without `forRootAsync`, no reporter is used and there is zero overhead.

### Register via `forRootAsync`

```typescript
import { ErrorsModule, IErrorReporter, IErrorContext } from '@zerly/errors';
import { ConfigService } from '@nestjs/config';

@Injectable()
export class SentryErrorReporter implements IErrorReporter {
  public report(error: unknown, context: IErrorContext): void {
    Sentry.captureException(error, { extra: context });
  }
}

// In your module:
ErrorsModule.forRootAsync({
  imports: [ConfigModule],
  useFactory: (config: ConfigService) => new SentryErrorReporter(config),
  inject: [ConfigService],
})
```

`report()` is called only for 5xx errors (HTTP and RPC). 4xx errors are silent.

### `IErrorContext`

```typescript
interface IErrorContext {
  type: 'http' | 'rpc';
  requestId?: string;
  method?: string; // HTTP only
  url?: string;    // HTTP only
}
```

---

## Validation Adapters

### typia (built-in)

`TypeGuardError` thrown by `typia.assertEquals<T>()` is automatically caught and converted:

```json
{
  "code": "VALIDATION_FAILED",
  "details": { "path": "$.email", "expected": "string", "value": 42 }
}
```

No configuration required.

### class-validator (optional)

For projects that use `class-validator` instead of typia, import the adapter explicitly:

```typescript
import { adaptClassValidatorErrors } from '@zerly/errors/adapters/class-validator.adapter';

// In a custom validation pipe:
throw adaptClassValidatorErrors(validationErrors);
```

Requires `class-validator` to be installed separately. Not a dependency of this library.

---

## Filter Behaviour Reference

| Exception source                 | `code`                     | `details`                   | `internal` (prod) | `internal` (dev)            |
|----------------------------------|----------------------------|-----------------------------|-------------------|-----------------------------|
| `DomainException`                | `exception.code`           | Always, if set              | —                 | `exception.internalDetails` |
| `HttpException`                  | `HTTP_ERROR_CODES[status]` | —                           | —                 | —                           |
| `RpcException` (domain payload)  | `payload.code`             | `payload.details`           | —                 | —                           |
| `RpcException` (unknown payload) | `INTERNAL_SERVER_ERROR`    | —                           | —                 | —                           |
| `TypeGuardError`                 | `VALIDATION_FAILED`        | `{ path, expected, value }` | —                 | —                           |
| Unknown error                    | `INTERNAL_SERVER_ERROR`    | —                           | —                 | `{ message }`               |

**Logging:** 5xx → `logger.error` with full `err` object. 4xx → silent.

---

## License

MIT © Horizon Republic
