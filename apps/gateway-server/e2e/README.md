# Gateway end-to-end test harness

This harness runs the full acceptance suite against a live
three-process stack: NATS, the `example-app` NestJS service (which
exposes `@GatewayRoute` handlers), and the `zerly-gateway-server`
binary. Scenarios cover the core HTTP/NATS round trip, the auth
verifier contract, the response-builder surface (cookies /
redirects), and the GatewayRoute contract (CORS, rate limit,
timeout, headers, cookie defaults).

The current setup runs NATS in Docker and both the Node and Go
processes on the host. A fully containerised stack can be layered on
later once a reusable `example-app` image exists.

## Prerequisites

- Docker (or OrbStack) for the NATS container
- `pnpm` and Node 20+ for the `example-app`
- Go 1.25+ for the gateway binary and the e2e test suite

## Startup sequence

The three processes MUST start in this exact order because the
gateway aborts startup if the `handler_registry` KV bucket does not
yet exist, and that bucket is created by `example-app` the moment it
registers its first `@ApiGateway` handler.

1. **NATS (JetStream enabled):**
   ```bash
   pnpm nx run gateway-server:e2e-up
   ```
   Waits for the container's healthcheck to go green.

2. **example-app (creates KV bucket on start):**
   ```bash
   NATS_URL=nats://localhost:4222 pnpm nx serve example-app
   ```
   In a separate terminal. Wait for `listening on` in the log.

3. **gateway-server (binds :8080):**
   ```bash
   pnpm nx build gateway-server
   NATS_URLS=nats://localhost:4222 \
   HTTP_ADDR=:8080 \
   KV_BUCKET=handler_registry \
   LOG_FORMAT=console LOG_LEVEL=info \
     ./dist/apps/gateway-server/gateway
   ```
   In a third terminal. Wait for `http server started`.

4. **Run the e2e suite:**
   ```bash
   pnpm nx run gateway-server:e2e
   ```

## Teardown

```bash
pnpm nx run gateway-server:e2e-down
```

Stops the NATS container and removes its volumes. The Node and Go
processes must be stopped manually from their respective terminals.

## Scenarios covered

### Core HTTP round trip (`e2e_test.go`)

| # | Method | Path               | Expected |
|---|--------|--------------------|----------|
| 1 | GET    | /users/1           | 200 + user JSON |
| 2 | POST   | /users             | 201 + created user + echoed X-Request-Id |
| 3 | DELETE | /users/2           | 204 (void body) |
| 4 | GET    | /nothing/here      | 404 + NOT_FOUND error body |
| 5 | *any*  | *                  | X-Request-Id response header always set |

### Auth contract (`auth_test.go`)

| # | Method | Path               | Token                | Expected |
|---|--------|--------------------|----------------------|----------|
| 1 | GET    | /me                | `demo-alice`         | 200 + claims echo |
| 2 | GET    | /me                | `demo-admin`         | 200 + admin roles |
| 3 | GET    | /me                | _none_               | 401 |
| 4 | GET    | /me                | `nope`               | 401 |
| 5 | GET    | /me                | `demo-banned`        | 403 |
| 6 | GET    | /articles/:id      | _none_               | 200 + `viewer: null` |
| 7 | GET    | /articles/:id      | `demo-alice`         | 200 + populated viewer |
| 8 | GET    | /articles/:id      | `demo-banned`        | 403 (403 never swallowed) |

### Response builder (`response_test.go`)

| # | Method | Path                 | Expected |
|---|--------|----------------------|----------|
| 1 | POST   | /auth/login          | 201 + `sid` cookie w/ flags |
| 2 | POST   | /auth/login (admin)  | admin roles + cookie value |
| 3 | POST   | /auth/logout         | 200 + cookie deletion |
| 4 | GET    | /auth/google/start   | 302 + Location header |
| 5 | GET    | /me (rotate token)   | verifier-rotated cookie surfaces |
| 6 | POST   | /auth/login          | `Secure=false` (local-dev boundary) |

### Route reload (`reload_test.go`)

These tests mutate the `handler_registry` KV bucket directly
via the NATS client and subscribe to synthetic subjects inline,
so no live Nest handler is needed. They exercise the gateway's
watcher → delta → routing-table refresh pipeline end-to-end.

| # | Scenario                                         | Expected |
|---|--------------------------------------------------|----------|
| 1 | New KV entry → gateway picks it up live          | 404 before → 200 after |
| 2 | Delete KV entry → gateway drops the route        | 200 before → 404 after |
| 3 | Modify KV entry (timeout tighten)                | 200 before → 504 after |
| 4 | 3 rapid successive Puts converge to last state   | final Put reflected |

### GatewayRoute contract (`contract_test.go`)

| # | Scenario                                                     | Expected |
|---|--------------------------------------------------------------|----------|
| 1 | OPTIONS preflight with matched origin                        | 204 + full CORS headers |
| 2 | OPTIONS preflight with unknown origin                        | 404 (no allow-origin leak) |
| 3 | OPTIONS preflight without Access-Control-Request-Method      | 404 |
| 4 | GET on CORS-enabled route with matched origin                | 200 + origin echo + Vary |
| 5 | OPTIONS on `credentials: true` route                         | `Allow-Credentials: true` |
| 6 | 2nd request within same second on `rps:1, burst:1` route     | 429 + `Retry-After: 1` |
| 7 | keyBy `header:x-api-key` isolates buckets between tenants    | tenant A's spike does not exhaust tenant B |
| 8 | Per-route timeout (200ms) < handler sleep (500ms)            | 504 returned in < 450ms |
| 9 | Per-route `headers` + forRoot default deep-merge             | both headers land on wire |
| 10 | forRoot header default reaches undecorated routes           | header applied globally |
| 11 | Bare `res.cookie(...)` merges forRoot cookie defaults       | `HttpOnly` / `SameSite` / `Path` / `Max-Age` flow through |
