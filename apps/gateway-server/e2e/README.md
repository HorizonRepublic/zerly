# Gateway end-to-end test harness

This harness runs five acceptance scenarios against a live
three-process stack: NATS, the `example-app` NestJS service (which
exposes `@ApiGateway` handlers), and the `zerly-gateway-server`
binary.

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

| # | Method | Path               | Expected |
|---|--------|--------------------|----------|
| 1 | GET    | /demo/users/1      | 200 + user JSON |
| 2 | POST   | /demo/users        | 201 + created user + echoed X-Request-Id |
| 3 | DELETE | /demo/users/2      | 204 (void body) |
| 4 | GET    | /nothing/here      | 404 + NOT_FOUND error body |
| 5 | *any*  | *                  | X-Request-Id response header always set |
