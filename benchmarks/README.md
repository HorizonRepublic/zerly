# benchmarks

Isolated HTTP load benchmarks for the Zerly gateway stack against
canonical NestJS baselines. Every target runs in its own set of
Docker containers so the dev environment (NATS, Postgres, example
app) is never touched.

## What is measured

Six stacks — three architectures × two JavaScript runtimes. Node
and Bun variants share the entire source tree; only the
Dockerfile `target:` stage differs, so any perf delta is purely
V8 vs JavaScriptCore + the runtime's built-in HTTP/IO primitives.

| Stack | HTTP entry point | Adapter | Runtime | Port |
|---|---|---|---|---|
| `nest-fastify` | Nest → FastifyAdapter | Fastify 5 | Node 22 | 18091 |
| `nest-express` | Nest → ExpressAdapter | Express 4 | Node 22 | 18092 |
| `zerly` | Go gateway-server → NATS → Nest (FastifyAdapter) via `@zerly/gateway-sdk` | Hertz + sonic | Node 22 | 18093 |
| `nest-fastify-bun` | Nest → FastifyAdapter | Fastify 5 | Bun 1 | 18094 |
| `nest-express-bun` | Nest → ExpressAdapter | Express 4 | Bun 1 | 18095 |
| `zerly-bun` | Go gateway-server → NATS → Nest (FastifyAdapter) via `@zerly/gateway-sdk` | Hertz + sonic | Bun 1 | 18093* |

`*` `zerly-bun` reuses the same Go gateway-server container as
`zerly`. Since `run-all.sh` runs profiles sequentially, only one
of the two zerly variants is ever up at once, so port 18093 is
shared without conflict.

Every stack exposes:

- `GET /bench/hello` — trivial JSON body, 200. Measures the
  happy-path hot loop.
- `GET /bench/teapot` — throws `ImATeapotException`. Measures the
  exception-formatting path (filter chain → envelope encoder →
  adapter serialization). The throw is mandatory: returning
  `res.status(418).json(...)` would bench a happy path that
  happens to emit 418 and miss the exception path entirely.

Each stack is wired in the canonical Nest quick-start style —
everything lives in `main.ts`, no kernel, no lifecycle providers,
no globals beyond what the gateway SDK itself installs. The DI
graph is deliberately the smallest thing that can serve the two
routes so the numbers reflect framework + adapter cost, not
scaffolding cost.

## Prerequisites

- Docker (with Compose v2)
- Node 20+ on the host (for the bench-runner)
- pnpm + the full repo toolchain (for building `@zerly/gateway-sdk`
  before the zerly stack image is built)

The bench-runner auto-installs `autocannon` into
`bench-runner/node_modules` on first run. It is intentionally not
listed in the root `package.json` — benchmarking is not part of
normal development.

## Running

```bash
# All three stacks, one at a time
./run-all.sh

# Single stack
./run-all.sh nest-fastify

# A subset, in order
./run-all.sh nest-fastify zerly
```

Results are written to `./results/`:

- `YYYY-MM-DDTHH-MM-SS-sssZ_<stack>_<scenario>.json` — raw
  autocannon output per (stack, scenario).
- `YYYY-MM-DDTHH-MM-SS-sssZ_report.md` — a roll-up table for all
  stacks that ran in the same invocation.

## Tuning knobs

Bench parameters are fixed in `bench-runner/run.mjs` at the top of
the file:

```js
const DURATION_SECONDS = 30;
const CONNECTIONS = 50;
const PIPELINING = 1;
const WARMUP_SECONDS = 3;
```

A 30-second run at 50 connections is the sweet spot for a single
dev machine: long enough to average out JIT warmup, short enough
that the full matrix finishes in under five minutes.

## How the zerly stack is wired

1. `run-all.sh` calls `pnpm nx build gateway-sdk` and copies the
   resulting `dist/libs/gateway-sdk` tree into
   `stacks/zerly/nest-bench-app/vendor/gateway-sdk`.
2. The bench-app's `package.json` carries
   `"@zerly/gateway-sdk": "file:./vendor/gateway-sdk"` so `npm
   install` inside the image resolves the SDK offline — no
   registry access, no workspace aliasing inside Docker.
3. `docker compose --profile zerly up` starts three containers:
   NATS (with JetStream), the Nest bench-app (microservice
   attached via `app.connectMicroservice`), and the Go
   gateway-server built from `apps/gateway-server/Dockerfile`.
4. The bench-runner on the host hits the gateway-server on
   `:18093`, which translates HTTP into NATS requests that land
   on the `@GatewayRoute`-decorated handlers in the bench-app.

Ports are fixed high (18091–18093) to avoid clashing with the dev
environment's typical 3000/4000/8080 bindings.
