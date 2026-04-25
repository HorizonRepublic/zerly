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

Numbers below come from an apples-to-apples harness: same hardware, same `wrk` invocation, same warm-up policy across every stack. The harness itself is kept out of the repo — it is a local tool, not something we ship — but the methodology is documented here and the microbenchmarks that guard regressions are version-controlled under [`benchmarks/`](benchmarks/).

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
KV_BUCKET=handler_registry \
HTTP_ADDR=:8080 \
LOG_FORMAT=console LOG_LEVEL=info \
  ./dist/apps/gateway-server/gateway
```

`NATS_URLS` and `KV_BUCKET` are the only required variables. The gateway refuses to start if either is missing or empty, or if the configured bucket does not exist on the NATS cluster. In normal operation the NestJS services using `@zerly/gateway-sdk` (via `@horizon-republic/nestjs-jetstream`) create the bucket automatically the first time they register a handler — so start the services before the gateway on a cold cluster.

## Configuration

All settings are environment variables, grouped by concern. The struct definition in [`internal/config/config.go`](internal/config/config.go) is the source of truth; the tables below mirror its `env:"..."` tags exactly. A regression test (`TestReadmeEnvListMatchesStruct`) fails CI if the two diverge.

`NATS_URLS` and `KV_BUCKET` are the only required variables. Everything else has a default suitable for a production deploy.

### HTTP

| Variable | Type | Default | Description |
|---|---|---|---|
| `HTTP_ADDR` | `host:port` | `:8080` | TCP listen address for the public HTTP server. Empty host binds all interfaces. See `Config.HTTPAddr`. |
| `HTTP_READ_TIMEOUT` | duration | `10s` | Full request (headers + body) read deadline. See `Config.ReadTimeout`. |
| `HTTP_WRITE_TIMEOUT` | duration | `35s` | Full response write deadline. **MUST be strictly greater than `REQUEST_TIMEOUT`** so a 504 emitted on request deadline can still be flushed before the HTTP write deadline fires. See `Config.WriteTimeout`. |
| `HTTP_IDLE_TIMEOUT` | duration | `120s` | Keep-alive idle deadline before the server closes the connection. See `Config.IdleTimeout`. |
| `HTTP_MAX_BODY_BYTES` | int64 | `1048576` | Max request body size in bytes; oversized requests get 413. See `Config.MaxBodyBytes`. |
| `HTTP_MAX_HEADER_BYTES` | int | `16384` | Max header block size in bytes (sum across all headers). See `Config.MaxHeaderBytes`. |

### Security / trusted proxy

| Variable | Type | Default | Description |
|---|---|---|---|
| `TRUSTED_PROXIES` | sentinel \| CIDR list | `private` (when unset) | Trusted upstream proxies for `X-Forwarded-For` resolution. Forms: `""` (trust nothing — always use peer IP), `"private"` (expand to the 7 private/loopback ranges), or a literal comma-separated CIDR list (`"10.0.0.0/8,172.16.0.0/12"`). Invalid CIDR fails `Load()` fail-closed. See `Config.TrustedProxiesRaw` / `Config.TrustedProxies`. |
| `TRUSTED_PROXY_HEADER` | enum | `X-Forwarded-For` | Header the trusted-proxy middleware reads to recover the client IP when the peer is in `TRUSTED_PROXIES`. Allowed values: `X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP` (case-insensitive). Single-value alternatives are used verbatim; `X-Forwarded-For` walks the chain rightmost-untrusted per RFC 7239 §7.1. Multi-hop topologies MUST use `X-Forwarded-For`. Unknown values fail `Load()` fail-closed. See `Config.TrustedProxyHeader`. |

### NATS transport

| Variable | Type | Default | Description |
|---|---|---|---|
| `NATS_URLS` | URL list | — (**REQUIRED**) | Comma-separated NATS server URLs. Single URL, static cluster list, or DNS-resolvable hostname (the client resolves A/SRV records). See `Config.NATSUrls`. |
| `NATS_RANDOMIZE_URLS` | bool | `true` | Shuffle `NATS_URLS` before dialling to spread initial connections across cluster nodes. See `Config.NATSRandomizeUrls`. |
| `NATS_DISCOVER_SERVERS` | bool | `true` | Pick up new cluster nodes via the client-side server-discovery protocol. See `Config.NATSDiscoverServers`. |
| `NATS_USER` | string | — | Username for password auth. Leave empty for creds-file or no auth. See `Config.NATSUser`. |
| `NATS_PASSWORD` | string | — | Password for password auth. See `Config.NATSPassword`. |
| `NATS_CREDS_FILE` | path | — | NKey credentials file (NGS / decentralised JWT auth). See `Config.NATSCredsFile`. |
| `NATS_RECONNECT_WAIT` | duration | `2s` | Delay between reconnect attempts after the connection drops. See `Config.NATSReconnectWait`. |
| `NATS_MAX_RECONNECTS` | int | `-1` | Cap on reconnect attempts. `-1` retries forever — the right default for a gateway that must survive cluster restarts. See `Config.NATSMaxReconnects`. |
| `NATS_RECONNECT_BUFSIZE` | int | `8388608` | In-memory buffer (bytes) for messages published while the connection is temporarily down. See `Config.NATSReconnectBufSize`. |

### Handler registry (KV)

| Variable | Type | Default | Description |
|---|---|---|---|
| `KV_BUCKET` | string | — (**REQUIRED**) | NATS KV bucket the gateway watches for handler registry entries. No default: a typical NATS account is shared across deploy stages and a silent fallback would risk cross-environment data leakage. See `Config.KVBucket`. |

### Request lifecycle

| Variable | Type | Default | Description |
|---|---|---|---|
| `REQUEST_TIMEOUT` | duration | `30s` | Per-request hard deadline applied to the full handler pipeline (RPC round-trip included). See `Config.RequestTimeout`. |
| `SHUTDOWN_TIMEOUT` | duration | `30s` | Wall-clock budget for the graceful-shutdown drain sequence before force-close. See `Config.ShutdownTimeout`. |

### Rate limiting

| Variable | Type | Default | Description |
|---|---|---|---|
| `RATELIMIT_FAIL_POLICY` | enum | `open` | Behavior when the distributed rate-limit store fails (network error, breaker open, CAS budget exhausted). `open` favors availability — a few requests slip past the limit during the outage. `closed` rejects with 503 — for compliance-critical deployments where the RL contract must hold under backend outage. **Normal rate-limit rejections (bucket empty under healthy backend) always return 429 regardless.** See `Config.RateLimitFailPolicy`. |
| `RATELIMIT_KEY_TTL` | duration | `10m` | Stale-key cleanup threshold. **Semantics depend on the store backend:** `memory` treats it as the idle-entry sweep interval (active keys retained indefinitely); `nats-kv` treats it as bucket MaxAge (keys reaped after the duration regardless of activity). Raise for `nats-kv` deployments where state must survive long traffic gaps. See `Config.RateLimitKeyTTL`. |
| `RATELIMIT_TIMEOUT` | duration | `50ms` | Per-request wall-clock budget for the rate-limit gate evaluation. Separate from `REQUEST_TIMEOUT` so a flaky distributed store cannot starve handler latency under retry pressure. Bounds: `> 0` and `≤ 1s` (rejected at `Load()` otherwise). See `Config.RateLimitTimeout`. |

### Logging

| Variable | Type | Default | Description |
|---|---|---|---|
| `LOG_LEVEL` | enum | `info` | Minimum zerolog level: `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic`, `disabled`. See `Config.LogLevel`. |
| `LOG_FORMAT` | enum | `json` | Output encoding: `json` for production, `console` for human-friendly colored output in local dev. See `Config.LogFormat`. |

### Runtime

| Variable | Type | Default | Description |
|---|---|---|---|
| `ENVIRONMENT` | string | `production` | Free-form deployment-tier label. The value `production` triggers redaction of sensitive error details from HTTP responses (`Config.IsProduction()`). See `Config.Environment`. |

## Metrics & counters

The rate-limit subsystem exports counter snapshots via `Counters() map[string]int64` on the `Router`, `MemoryStore`, and `NATSKVStore` types in [`internal/ratelimit/`](internal/ratelimit/). These names are the contract OpenTelemetry export will adopt; treat them as the operator-facing observability surface even before the OTel wiring lands.

The current set is documented as **experimental — names may change** because the unified counter schema across stores is still evolving on the rate-limit side. Once the schema is unified the table below will move to **stable**.

### Router (`ratelimit.Router.Counters()`)

| Counter | Type | Description | Stability |
|---|---|---|---|
| `ratelimit_store_fallback` | counter | Times the router fell back from the declared store to a downgrade target (e.g. `nats-kv → memory` after a sustained outage). | Experimental |

### Memory store (`ratelimit.MemoryStore.Counters()`)

| Counter | Type | Description | Stability |
|---|---|---|---|
| `ratelimit_memory_decisions_allowed` | counter | Allow decisions emitted by the in-process GCRA evaluator. | Experimental |
| `ratelimit_memory_decisions_rejected` | counter | Reject decisions emitted by the in-process GCRA evaluator (bucket empty). | Experimental |

### NATS KV store (`ratelimit.NATSKVStore.Counters()`)

| Counter | Type | Description | Stability |
|---|---|---|---|
| `ratelimit_natskv_decisions_allowed` | counter | Allow decisions confirmed via successful CAS. | Experimental |
| `ratelimit_natskv_decisions_rejected` | counter | Reject decisions confirmed via successful CAS (bucket empty). | Experimental |
| `ratelimit_natskv_cas_retries` | counter | CAS write retries triggered by concurrent updates on the same key. | Experimental |
| `ratelimit_natskv_budget_exhausted` | counter | Decisions abandoned because the per-request CAS retry budget ran out. | Experimental |
| `ratelimit_natskv_circuit_state` | gauge | Current circuit breaker state encoded as int (0 closed, 1 half-open, 2 open). | Experimental |
| `ratelimit_natskv_breaker_transitions` | counter | Breaker state transitions over the process lifetime. | Experimental |
| `ratelimit_natskv_circuit_rejected` | counter | Decisions short-circuited because the breaker was open. | Experimental |
| `ratelimit_natskv_corrupt_tat` | counter | Stored TAT values that failed decode and were treated as a fail-policy event rather than silently reset. | Experimental |

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
