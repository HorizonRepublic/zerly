# zerly-gateway-server

A performance-first HTTP edge server for the Zerly platform. Watches the `handler_registry` NATS KV bucket for routing metadata published by NestJS services and proxies incoming HTTP requests to the matching handler via Core NATS request/reply.

Companion TypeScript SDK: [`@zerly/gateway-sdk`](../../libs/gateway-sdk). NestJS services declare `@ApiGateway` routes via that SDK; the gateway picks up the metadata without any configuration change.

## Why this exists

The gateway lets NestJS microservices expose HTTP endpoints **without** each service standing up its own public listener. A single hardened Go binary fronts every Nest handler in the cluster, so you get:

- **One place** to terminate TLS, enforce rate limits, inject observability headers, and apply cluster-wide policies
- **Zero HTTP boilerplate inside Nest services** — handlers stay pure RPC code and publish route metadata declaratively
- **Hot reload of routes** — new handlers appear in the routing table within milliseconds of the KV update, with no gateway restart
- **A performance budget** that aims for single-digit microsecond overhead per request on the gateway hop

## Architecture at a glance

```
   HTTP client
       │
       │  Hertz (catch-all /*path)
       ▼
   gateway-server
       │    ┌────────────────┐
       │    │ routing.Table  │ ← rebuilt atomically on every KV change
       │    └────────────────┘
       │  Core NATS request/reply
       ▼
   NestJS handler (via @zerly/gateway-sdk)
```

1. Hertz accepts the HTTP request on a catch-all route.
2. The framework-agnostic adapter translates it into a `proxy.ServeInput`.
3. The proxy handler looks up the matching route in the atomic `routing.Table`.
4. It encodes a `GatewayRequest` envelope (via `sonic`), sends it over Core NATS with a deadline, and waits for the reply.
5. The reply is decoded, status/headers/body are copied back onto the Hertz response, and the gateway-generated `X-Request-Id` is stamped.

## Stack

- [`cloudwego/hertz`](https://github.com/cloudwego/hertz) — HTTP server
- [`nats.go`](https://github.com/nats-io/nats.go) — NATS client (Core RPC + JetStream KV watch)
- [`bytedance/sonic`](https://github.com/bytedance/sonic) — JSON marshalling on the hot path
- [`rs/zerolog`](https://github.com/rs/zerolog) — structured logging
- [`oklog/ulid`](https://github.com/oklog/ulid) — monotonic request IDs
- [`caarlos0/env`](https://github.com/caarlos0/env) — environment config parsing

## Requirements

- Go 1.25+
- A reachable NATS server with JetStream enabled (the gateway watches a JetStream KV bucket)
- [`golangci-lint`](https://golangci-lint.run/) for `nx lint gateway-server` (optional for `nx build`)

## Build

```bash
pnpm nx build gateway-server
```

Produces `dist/apps/gateway-server/gateway` — a statically linked binary suitable for the included scratch-based `Dockerfile`.

## Run

```bash
NATS_URLS=nats://localhost:4222 \
HTTP_ADDR=:8080 \
KV_BUCKET=handler_registry \
LOG_FORMAT=console LOG_LEVEL=info \
  ./dist/apps/gateway-server/gateway
```

The gateway refuses to start if the KV bucket does not exist. In normal operation the NestJS services using `@zerly/gateway-sdk` (via `@horizon-republic/nestjs-jetstream`) create the bucket automatically the first time they register a handler — so start the services before the gateway on a cold cluster.

## Configuration

All settings are environment variables, grouped by concern. Only `NATS_URLS` is required; everything else has a sensible default.

### HTTP

| Variable | Default | Notes |
|---|---|---|
| `HTTP_ADDR` | `:8080` | `host:port` for the public listener |
| `HTTP_READ_TIMEOUT` | `10s` | Full request (headers + body) read deadline |
| `HTTP_WRITE_TIMEOUT` | `30s` | Full response write deadline |
| `HTTP_IDLE_TIMEOUT` | `120s` | Keep-alive idle deadline |
| `HTTP_MAX_BODY_BYTES` | `1048576` | Max request body size (413 above) |
| `HTTP_MAX_HEADER_BYTES` | `16384` | Max header block size |
| `HTTP_ENABLE_H2` | `true` | Toggle h2c (cleartext HTTP/2) — reserved; not yet wired |

### NATS

| Variable | Default | Notes |
|---|---|---|
| `NATS_URLS` | — | **Required.** Comma-separated URLs (`nats://a:4222,nats://b:4222`) |
| `NATS_RANDOMIZE_URLS` | `true` | Shuffle URLs before dialling |
| `NATS_DISCOVER_SERVERS` | `true` | Track cluster topology changes |
| `NATS_USER` / `NATS_PASSWORD` | — | Plain-password auth |
| `NATS_CREDS_FILE` | — | NKey credentials file path (NGS / JWT) |
| `NATS_CONNECTION_POOL` | `1` | Parallel `*nats.Conn` instances; raise only after benchmarking contention |
| `NATS_RECONNECT_WAIT` | `2s` | Delay between reconnect attempts |
| `NATS_MAX_RECONNECTS` | `-1` | Retry forever by default |
| `NATS_RECONNECT_BUFSIZE` | `8388608` | In-memory buffer for outgoing messages while disconnected |

### Registry (KV)

| Variable | Default | Notes |
|---|---|---|
| `KV_BUCKET` | `handler_registry` | JetStream KV bucket to watch |
| `KV_WATCH_TIMEOUT` | `5s` | Initial hydration timeout; startup aborts if exceeded |

### Request lifecycle

| Variable | Default | Notes |
|---|---|---|
| `REQUEST_TIMEOUT` | `30s` | Per-request deadline passed to the upstream handler via the envelope |
| `SHUTDOWN_TIMEOUT` | `30s` | Global timeout for the graceful drain sequence |

### Logging

| Variable | Default | Notes |
|---|---|---|
| `LOG_LEVEL` | `info` | `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic`, `disabled` |
| `LOG_FORMAT` | `json` | `json` for production, `console` for human-friendly local output |
| `LOG_REQUESTS` | `true` | Access-log-style per-request entries |
| `LOG_REQUEST_BODY` | `false` | Include request body in logs — expensive, leaks PII |
| `LOG_RESPONSE_BODY` | `false` | Include response body in logs — expensive, leaks PII |
| `LOG_SLOW_REQUEST_MS` | `1000` | Additionally log any request whose latency exceeds this at `warn` |
| `LOG_SAMPLING_RATE` | `1` | 1-in-N sampling when `LOG_REQUESTS` is on |

### Other

| Variable | Default | Notes |
|---|---|---|
| `ENVIRONMENT` | `production` | Free-form label; `production` redacts sensitive error detail from responses |
| `METRICS_ENABLED` | `true` | Prometheus endpoint toggle |
| `METRICS_ADDR` | `:9090` | Metrics listener (separate from the public HTTP port) |
| `METRICS_PATH` | `/metrics` | Metrics HTTP path |
| `HEALTH_ENABLED` | `true` | Liveness / readiness endpoints toggle |
| `HEALTH_LIVE_PATH` | `/_gateway/live` | Liveness probe path |
| `HEALTH_READY_PATH` | `/_gateway/ready` | Readiness probe path |

## Tests

```bash
pnpm nx test gateway-server                    # unit tests with -race
pnpm nx run gateway-server:test-integration    # integration tests (requires Docker)
pnpm nx run gateway-server:bench               # benchmarks
pnpm nx lint gateway-server                    # golangci-lint
```

Benchmark baselines are committed under [`benchmarks/baseline.txt`](benchmarks/baseline.txt); re-run the bench target on the same hardware class to spot regressions.

## End-to-end harness

A build-tagged e2e suite lives in [`e2e/`](e2e/) and exercises the full HTTP → NATS → Nest → HTTP flow against a live stack. See [`e2e/README.md`](e2e/README.md) for the three-terminal startup protocol.

## Shutdown semantics

On `SIGTERM` / `SIGINT` the gateway runs an ordered drain bounded by `SHUTDOWN_TIMEOUT`:

1. **HTTP** — Hertz stops accepting new connections and waits for in-flight requests to finish.
2. **Registry watcher** — the KV watch loop is cancelled so no late update mutates the routing table mid-drain.
3. **NATS** — the connection is drained, letting any in-flight upstream replies land before the socket is torn down.

Step failures are logged but never abort the sequence; the process always attempts every drain so a stuck dependency cannot leak the others.
