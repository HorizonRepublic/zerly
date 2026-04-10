# zerly-gateway-server

Go HTTP edge server for the Zerly platform. Watches the `handler_registry` NATS KV bucket for routing metadata and proxies HTTP requests to Nest microservice handlers via Core NATS request/reply.

See the [design specification](../../docs/superpowers/specs/2026-04-10-zerly-gateway-design.md) and the companion TypeScript SDK [`@zerly/gateway-sdk`](../../libs/gateway-sdk).

## Status

**Scaffolding placeholder (M11).** The runtime logic lands incrementally in milestones M12 through M22. This README will be expanded in M24 with full configuration reference and deployment notes.

## Requirements

- Go 1.22 or later
- [golangci-lint](https://golangci-lint.run/) for linting (optional for build, required for `nx lint gateway-server`)

## Build

```bash
pnpm nx build gateway-server
```

Produces `dist/apps/gateway-server/gateway` — a static linux/amd64 binary.

## Run

```bash
NATS_URLS=nats://localhost:4222 HTTP_ADDR=:8080 ./dist/apps/gateway-server/gateway
```

Note: in M11 the binary only prints a placeholder message. Real CLI flags land in M12 (config).

## Tests

```bash
pnpm nx test gateway-server                   # unit tests with race detector
pnpm nx run gateway-server:test-integration   # integration tests (requires Docker)
pnpm nx run gateway-server:bench              # benchmarks
pnpm nx lint gateway-server                   # golangci-lint
```
