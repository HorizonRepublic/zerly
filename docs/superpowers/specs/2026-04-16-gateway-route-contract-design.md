# GatewayRoute as Full Endpoint Contract

**Goal:** Transform `@GatewayRoute` from a simple route registrator into the
single source of truth for every endpoint's behavior — CORS, rate limiting,
response headers, cookie defaults, and per-route timeout. SDK declares,
KV stores, Go executes. Zero config on the gateway side.

**Architecture:** Extend the existing `handler_registry` KV entry with new
optional fields. SDK merges `GatewayModule.forRoot({ defaults })` with
per-route `@GatewayRoute` overrides at registration time and writes the
final merged object to KV. Go gateway reads the extended entry and
executes CORS preflight, rate limiting, static response headers, and
per-route timeouts — all before or after the NATS round-trip as
appropriate. Cookie defaults remain SDK-only (Go proxies `Set-Cookie`
headers from the reply envelope as-is).

**Tech Stack:** TypeScript (NestJS SDK), Go (gateway-server), NATS KV
(`handler_registry` bucket).

---

## 1. SDK API Surface

### 1.1 `GatewayModule.forRoot` — endpoint defaults

```ts
GatewayModule.forRoot({
  // existing providers (unchanged)
  replyBuilder?: Type<IGatewayReplyBuilder>,
  statusResolver?: Type<IStatusResolver>,
  errorBodyFactory?: Type<IErrorBodyFactory>,

  // NEW: endpoint defaults merged into every @GatewayRoute at registration
  defaults?: IGatewayDefaults,
})
```

```ts
interface IGatewayDefaults {
  readonly cors?: IGatewayCorsConfig;
  readonly rateLimit?: IGatewayRateLimitConfig;
  readonly headers?: Readonly<Record<string, string>>;
  readonly cookies?: Partial<ICookieOptions>;     // SDK-only, NOT written to KV
  readonly timeout?: number;                       // ms
}
```

Example with env-driven config via `forRootAsync`:

```ts
GatewayModule.forRootAsync({
  imports: [ConfigModule],
  inject: [APP_CONFIG],
  useFactory: (config: IAppConfig) => ({
    defaults: {
      cors: {
        origins: config.corsOrigins,
        credentials: true,
        maxAge: 3600,
      },
      rateLimit: {
        rps: config.globalRps,
        keyBy: ['ip'],
      },
      headers: {
        'x-frame-options': 'DENY',
        'x-content-type-options': 'nosniff',
        'strict-transport-security': `max-age=${config.hstsMaxAge}; includeSubDomains`,
      },
      cookies: {
        path: '/',
        secure: config.isProduction,
        httpOnly: true,
        sameSite: 'lax',
      },
      timeout: config.requestTimeout,
    },
  }),
})
```

### 1.2 `@GatewayRoute` — per-route overrides

```ts
interface IGatewayRouteOptions {
  // existing
  readonly pattern: string;
  readonly method: HttpMethod;
  readonly path: string;
  readonly statusCode?: number;
  readonly auth?: GatewayRouteAuth;

  // NEW — override forRoot defaults for this route
  readonly cors?: IGatewayCorsConfig;
  readonly rateLimit?: IGatewayRateLimitConfig;
  readonly headers?: Readonly<Record<string, string>>;
  readonly timeout?: number;  // ms
}
```

Example:

```ts
@GatewayRoute({
  pattern: 'auth.login',
  method: 'POST',
  path: '/auth/login',
  auth: true,
  rateLimit: { rps: 5, keyBy: ['ip'] },       // stricter than global
  cors: { origins: ['https://login.example.com'] },  // narrower
  timeout: 10_000,                                    // shorter
  headers: { 'cache-control': 'no-store' },           // added to global
})
```

### 1.3 Merge semantics

Four-tier priority (highest to lowest):

1. **Handler** `@GatewayResponse()` — runtime headers/cookies, always wins
2. **`@GatewayRoute({})`** — per-route decorator overrides
3. **`forRoot({ defaults })`** — module-level defaults
4. **SDK hardcoded defaults** — predefined values in SDK code

Merge rules per block:

| Block       | Strategy        | Rationale |
|-------------|-----------------|-----------|
| `cors`      | Shallow replace | CORS policy is a cohesive unit; partial override creates unexpected combos |
| `rateLimit` | Shallow replace | Same — `rps: 5` without `keyBy` inheriting global keyBy is ambiguous |
| `headers`   | Deep merge (per-key) | Key-value set; user adds one header without losing security headers |
| `cookies`   | Shallow replace | SDK-only, cohesive policy |
| `timeout`   | Simple override | Scalar value |

Merge happens at registration time in an injectable `MetadataProvider`, not
in the decorator function (which has no DI access). The decorator writes
raw per-route config to `MessagePattern` extras. The provider merges
defaults before the KV write.

---

## 2. KV Wire Format

### 2.1 Extended `HandlerEntry`

SDK writes the final merged object. Example for `POST /auth/login`:

```json
{
  "http": { "method": "POST", "path": "/auth/login" },
  "auth": { "verifier": "", "optional": false },
  "cors": {
    "origins": ["https://login.example.com"],
    "methods": ["POST"],
    "headers": ["Content-Type", "Authorization"],
    "credentials": true,
    "maxAge": 3600
  },
  "rateLimit": {
    "rps": 5,
    "burst": 10,
    "keyBy": ["ip"]
  },
  "headers": {
    "x-frame-options": "DENY",
    "x-content-type-options": "nosniff",
    "strict-transport-security": "max-age=31536000; includeSubDomains",
    "cache-control": "no-store"
  },
  "timeout": 10000
}
```

Fields without values are omitted from JSON (`omitempty`). Go sees `nil`
pointer and skips the corresponding behavior.

### 2.2 TypeScript interfaces for KV fields

```ts
interface IGatewayCorsConfig {
  readonly origins: readonly string[];
  readonly methods?: readonly string[];      // default: route's own method
  readonly headers?: readonly string[];      // default: ['Content-Type', 'Authorization', 'X-Request-Id']
  readonly credentials?: boolean;
  readonly maxAge?: number;                  // seconds
}

type RateLimitKey = 'ip' | `header:${string}` | `cookie:${string}` | `user:${string}`;

interface IGatewayRateLimitConfig {
  readonly rps: number;
  readonly burst?: number;                   // default: rps * 2
  readonly keyBy?: readonly RateLimitKey[];  // default: ['ip']
}
```

---

## 3. Go Structs

### 3.1 Extended `entry.go`

```go
type HandlerEntry struct {
    HTTP      *HTTPMeta         `json:"http,omitempty"`
    Auth      *RouteAuthMeta    `json:"auth,omitempty"`
    Verifier  *VerifierMeta     `json:"verifier,omitempty"`
    CORS      *CORSMeta         `json:"cors,omitempty"`
    RateLimit *RateLimitMeta    `json:"rateLimit,omitempty"`
    Headers   map[string]string `json:"headers,omitempty"`
    Timeout   *int              `json:"timeout,omitempty"`
}

type CORSMeta struct {
    Origins     []string `json:"origins"`
    Methods     []string `json:"methods,omitempty"`
    Headers     []string `json:"headers,omitempty"`
    Credentials bool     `json:"credentials,omitempty"`
    MaxAge      int      `json:"maxAge,omitempty"`
}

type RateLimitMeta struct {
    RPS   int      `json:"rps"`
    Burst int      `json:"burst,omitempty"`
    KeyBy []string `json:"keyBy,omitempty"`
}
```

### 3.2 Extended `route.go`

```go
type Route struct {
    Subject      string
    Method       string
    PathTemplate string
    Auth         *RouteAuth
    CORS         *CORSMeta
    RateLimit    *RateLimitMeta
    Headers      map[string]string
    Timeout      time.Duration      // 0 = use global REQUEST_TIMEOUT
}
```

`Timeout` converted from `int` (ms) to `time.Duration` once at table
build time, not per-request.

---

## 4. Go Execution Flow

### 4.1 Request lifecycle

```
HTTP request
  │
  ├─ method == OPTIONS?
  │    ├─ Lookup(Access-Control-Request-Method, path) → Route
  │    ├─ route.CORS != nil → writeCORSPreflight(204) → return
  │    └─ route.CORS == nil → 404
  │
  ├─ Lookup(method, path) → Route
  │
  ├─ route.Auth != nil? → NATS verifier call (unchanged, yields claims)
  │
  ├─ route.RateLimit != nil?
  │    ├─ resolve key (walk keyBy chain; user:* reads claims from auth step)
  │    ├─ check token bucket
  │    └─ exceeded → 429 + Retry-After → return
  │
  ├─ route.Timeout > 0 → override context deadline
  │    └─ else → global REQUEST_TIMEOUT
  │
  ├─ NATS proxy call → reply envelope
  │
  ├─ route.Headers → set static headers (BEFORE envelope headers)
  ├─ envelope headers → set on response (OVERRIDE matching keys)
  ├─ route.CORS != nil → add CORS response headers
  │
  └─ write response
```

**Rate limit after auth** — because `user:<field>` in `keyBy` needs
auth claims. Auth verifier responses will be cached (roadmap §7), so
the cost of auth-before-ratelimit is near-zero for repeat requests.
For pure IP-based DDoS protection without auth cost, a lightweight
pre-routing IP limiter can be added later (roadmap H.4) as a
separate Go-env-configured layer.

**OPTIONS preflight not rate-limited** — preflight costs nanoseconds
(no NATS), and blocking it breaks CORS for legitimate clients.

### 4.2 CORS preflight details

- `Access-Control-Allow-Origin`: match `Origin` header against
  `cors.origins`. Match → echo exact origin. No match → omit CORS
  headers (browser blocks). Wildcard `"*"` matches all.
- `Access-Control-Allow-Methods`: from `cors.methods`, or
  `table.Methods(path)` to list all registered methods for the path.
- `Access-Control-Allow-Headers`: from `cors.headers`, or default
  set `["Content-Type", "Authorization", "X-Request-Id"]`.
- `Access-Control-Allow-Credentials`: from `cors.credentials`.
- `Access-Control-Max-Age`: from `cors.maxAge`.
- `Vary: Origin`: always set when origins is not `["*"]`.

### 4.3 CORS on regular responses

Same `Allow-Origin`, `Allow-Credentials`, `Vary` headers added to
every non-OPTIONS response where `route.CORS != nil`. No
`Allow-Methods`/`Allow-Headers`/`Max-Age` — those are preflight-only.

---

## 5. Rate Limiter

### 5.1 Store interface

```go
type RateLimiterStore interface {
    Allow(key string, rps int, burst int) bool
}
```

One method. In-memory implementation now, Redis swap later via
interface substitution.

### 5.2 In-memory implementation

Token bucket per key using `golang.org/x/time/rate`. Keys stored in
`sync.Map` as `"{method}:{path}:{resolved_client_id}"`.

```go
type memoryStore struct {
    limiters sync.Map
}

func (s *memoryStore) Allow(key string, rps int, burst int) bool {
    v, _ := s.limiters.LoadOrStore(key, rate.NewLimiter(rate.Limit(rps), burst))
    return v.(*rate.Limiter).Allow()
}
```

### 5.3 Key resolution

Walk `keyBy` chain in order, return first resolved value:

| Key pattern       | Source                          | Resolves when         |
|-------------------|---------------------------------|-----------------------|
| `ip`              | Trusted-proxy-resolved client IP | Always                |
| `header:<name>`   | Request header                  | Header present + non-empty |
| `cookie:<name>`   | Request cookie                  | Cookie present + non-empty |
| `user:<field>`    | Auth claims after verification  | Auth succeeded + field exists |

Implicit fallback: if nothing in the chain resolves → IP.

### 5.4 Stale entry cleanup

Background goroutine evicts entries untouched for 60 seconds.
Prevents unbounded memory growth from long-tail IPs.

### 5.5 Config change handling

When KV watcher delivers a route with changed `rateLimit` config,
flush all limiter entries for that route (prefix match on key).
New limiters auto-created on next request with updated `rps`/`burst`.

---

## 6. Watcher Diff Logging

### 6.1 Current problem

Go watcher logs on every KV update tick, even when the snapshot
has not changed. Noisy and pollutes logs.

### 6.2 Solution

Compare new snapshot against previous. Log only when something
actually changed. One `INFO` log with full table + diff summary:

```json
{
  "level": "info",
  "message": "routing table updated",
  "diff": {
    "added": ["POST /users/billing", "GET /users/profile"],
    "removed": ["DELETE /users/legacy"],
    "modified": ["PUT /users/:id (cors, timeout)", "POST /auth/login (rateLimit)"]
  },
  "routes": [
    { "method": "GET",  "path": "/users/:id",    "auth": true,  "cors": true, "rateLimit": "10 rps", "timeout": "5s" },
    { "method": "POST", "path": "/users",         "auth": true,  "cors": true, "rateLimit": "100 rps" },
    { "method": "POST", "path": "/auth/login",    "auth": true,  "cors": true, "rateLimit": "5 rps" }
  ],
  "total": 3
}
```

Route summary is compact: method, path, boolean flags for auth/cors,
human-readable rateLimit/timeout. Not a full JSON dump of every field.

Snapshot unchanged → silence. Changed → one log with full picture.

All new fields (CORS, rate limit, headers, timeout) are read from
the Route struct at request time, so config changes via KV watcher
take effect immediately without gateway restart. No warn-on-restart
logging needed — everything is hot-reloadable.

---

## 7. SDK Internals — Merge + Registration

### 7.1 Defaults DI token

```ts
const GATEWAY_DEFAULTS = Symbol('GATEWAY_DEFAULTS');

// in forRoot():
{ provide: GATEWAY_DEFAULTS, useValue: Object.freeze(options.defaults ?? {}) }
```

Global, frozen, injected once. `forRootAsync` resolves via `useFactory`.

### 7.2 MetadataProvider for merge

`@GatewayRoute` decorator remains a pure function (no DI access). It
writes raw per-route config into `MessagePattern` extras:

```ts
MessagePattern(options.pattern, {
  meta: { http, auth, cors, rateLimit, headers, timeout }
})
```

An injectable `GatewayMetadataEnricher` implements the
`MetadataProvider` interface from `@horizon-republic/nestjs-jetstream`.
It merges defaults before KV write:

```ts
@Injectable()
class GatewayMetadataEnricher implements MetadataProvider {
  constructor(
    @Inject(GATEWAY_DEFAULTS)
    private readonly defaults: IGatewayDefaults,
  ) {}

  enrich(pattern: string, meta: Record<string, unknown>): Record<string, unknown> {
    return mergeRouteDefaults(this.defaults, meta);
  }
}
```

### 7.3 Merge function

```ts
function mergeRouteDefaults(
  defaults: IGatewayDefaults,
  route: IGatewayRouteMeta,
): IGatewayRouteMeta {
  return {
    ...route,
    cors: route.cors ?? defaults.cors,
    rateLimit: route.rateLimit ?? defaults.rateLimit,
    headers: { ...defaults.headers, ...route.headers },
    timeout: route.timeout ?? defaults.timeout,
  };
}
```

- `cors`, `rateLimit`: shallow replace (per-route replaces entire block)
- `headers`: deep merge per-key (per-route adds/overrides individual keys)
- `timeout`: simple override
- `cookies`: NOT merged here — SDK-only, handled in interceptor

### 7.4 Cookie defaults (SDK-only)

Cookie defaults from `forRoot({ defaults: { cookies } })` are merged
with per-cookie options in `GatewayResponseInterceptor` / cookie
serializer. Not written to KV. Go proxies `Set-Cookie` headers from
the reply envelope as-is.

---

## 8. Components Changed

| Component | Changes |
|---|---|
| `IGatewayRouteOptions` | + `cors`, `rateLimit`, `headers`, `timeout` fields |
| `IGatewayModuleOptions` | + `defaults: IGatewayDefaults` |
| `GatewayModule.forRoot` | + `GATEWAY_DEFAULTS` DI token, register `GatewayMetadataEnricher` |
| `GatewayModule.forRootAsync` | + async defaults resolution via `useFactory` |
| `GatewayMetadataEnricher` | New — injectable MetadataProvider for merge |
| `@GatewayRoute` decorator | + new optional fields written to `meta` |
| Go `HandlerEntry` (`entry.go`) | + `CORSMeta`, `RateLimitMeta`, `Headers`, `Timeout` |
| Go `Route` (`route.go`) | + corresponding fields |
| Go `CollectRoutes` (`builder.go`) | + copy new fields from entry to route |
| Go proxy handler (`handler.go`) | + rate limit check, CORS preflight, per-route timeout, static headers |
| Go rate limiter | New — `RateLimiterStore` interface + in-memory token bucket |
| Go watcher | Diff-based logging: full table + changes on update, silence when unchanged |
| Cookie defaults | SDK-only merge in interceptor, no KV/Go changes |

---

## 9. Non-Goals

- **Admin HTTP endpoints on the gateway** (`/_gateway/*`). Public
  attack surface. Route inspection will be via CLI later.
- **Redis rate limiter store.** Interface designed for it, but
  in-memory is the only implementation in this iteration.
- **Per-user rate limiting at the gateway layer.** `user:<field>`
  in `keyBy` uses auth claims, but user lifecycle / billing quotas
  belong in a downstream service.
- **Dynamic CORS based on request body or handler logic.** CORS is
  declarative per-route, not computed at runtime.
