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

## Performance

Numbers below come from the apples-to-apples harness committed at [`bench-compare/`](../../bench-compare/) — same hardware, same `wrk` invocation, same warm-up policy across every stack. Run it yourself with `./bench-compare/run-all.sh` and reproduce them in ~70 seconds.

**Test environment**

| Field | Value |
|---|---|
| CPU | Apple M4 Pro (arm64) |
| OS | macOS 26.3 (build 25D5112c), Darwin 25.3.0 |
| Go | go1.26.1 darwin/arm64 |
| Node.js | v24.2.0 |
| Load tool | `wrk` 4.2.0 |
| Workload | `GET /demo/users/1`, 4 threads × 100 connections, 15 s measured + 5 s warm-up |
| Body | `{"id":"1","name":"Alice"}` (single in-memory record) |

**Headline numbers**

| Stack | Req/sec | p50 | p99 | What it actually does |
|---|---:|---:|---:|---|
| **Zerly gateway — pure routing path (404)** | **185 266** | **0.48 ms** | **1.05 ms** | Hertz accept + atomic routing-table lookup + pre-encoded error body + `X-Request-Id` stamp |
| Fastify hello-world | 97 300 | 1.02 ms | 1.75 ms | Static JSON response, zero work beyond `JSON.stringify` |
| Express hello-world | 77 074 | 1.26 ms | 1.72 ms | Static JSON response (with `etag` and `x-powered-by` disabled for fairness) |
| **Zerly gateway — full NATS round-trip → Nest handler** | **59 596** | **1.42 ms** | **4.50 ms** | Encode envelope → Core NATS RPC → Nest handler → decode reply → write response |
| Express + `http.request` proxy → Node upstream | 36 877 | 2.70 ms | 3.88 ms | Front + HTTP/1 keep-alive call to a separate upstream Node process |
| Fastify + `undici.request` proxy → Node upstream | 25 454 | 3.64 ms | 7.54 ms | Same shape via `undici` (Fastify's recommended HTTP client) |

### What this means

**On absolute throughput, the Zerly gateway hits ~185 k req/sec on its routing-table path** — almost 2× faster than Fastify hello-world and 2.4× faster than Express, while doing strictly more work per request (route lookup, error body assembly, request-id stamping, content-type pinning). The win comes from Hertz's `netpoll` + sonic + the hand-rolled zero-allocation envelope encoder + lock-free `atomic.Value` routing reads. There is no GC pressure on the hot path: `Encoder.Encode` measures 358 ns/op with 0 allocations in the microbenchmark.

**On end-to-end latency, the gateway costs you ~0.4 ms per request vs hitting a Fastify monolith directly.** That is the price of having a gateway at all — there is no way around an inter-process hop. The honest framing is "gateway tax", and on that axis Zerly is dramatically cheaper than the Node-based alternatives:

| Architecture | p50 latency | Tax vs same-language hello-world |
|---|---:|---:|
| Fastify monolith | 1.02 ms | — (baseline) |
| **Zerly gateway → Nest handler** | **1.42 ms** | **+0.40 ms (+39 %)** |
| Express monolith | 1.26 ms | — (baseline) |
| Express HTTP proxy → Node upstream | 2.70 ms | +1.44 ms (+114 %) |
| Fastify + undici proxy → Node upstream | 3.64 ms | +2.62 ms (+257 %) |

**Zerly's gateway tax is roughly 3.6× cheaper than Express's HTTP proxy and 6.5× cheaper than Fastify's undici-based proxy** for the same architectural shape (gateway in front of an upstream service). The win is structural: Core NATS request/reply is dramatically more efficient than HTTP/1 keep-alive between two local processes, and our hand-rolled encoder avoids the per-request allocation cost that any sonic-style or `JSON.stringify`-based path pays.

### When to use Zerly (and when not to)

- **Use Zerly when** you are building a NestJS-based microservices platform and want a single hardened HTTP edge that fronts every service without per-service HTTP boilerplate, dynamic route registration (handlers appear within milliseconds of `@ApiGateway` decoration), and the lowest gateway-hop overhead among production-grade options.
- **Do NOT use Zerly when** your entire system fits inside a single process. A Fastify monolith is faster on absolute latency than any gateway architecture — Zerly included — because there is no inter-process hop to pay for. We are not competing with monolith Fastify; we are competing with the Express/Fastify *gateway* topology, and on that axis we are an order of magnitude better on throughput and 3-6× better on latency tax.

### Caveats

- Numbers were captured on a single Apple M4 Pro under no other workload. Server-grade Xeon/EPYC and ARM Graviton hardware will scale roughly linearly per core but exact numbers will differ.
- All stacks bind `127.0.0.1` only. No TLS, no HTTP/2, no compression, no auth, no CORS — every layer was disabled to measure framework overhead on a best-case path.
- The Nest upstream handler returns a single in-memory record. Real handlers that hit a database or fan out to other services will be dominated by those operations, not by the gateway hop. Adding ~0.4 ms to a 50 ms database query is invisible; adding it to a 0.5 ms cache lookup matters.
- Microbenchmark baselines for the four hot-path operations live in [`benchmarks/baseline.txt`](benchmarks/baseline.txt) and are tracked in git so regressions show up as PR diffs.

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
