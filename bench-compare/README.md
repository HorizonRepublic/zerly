# bench-compare

A tiny, self-contained harness for producing apples-to-apples HTTP throughput
numbers for the Zerly Gateway against a handful of popular Node.js HTTP stacks
on the **same** machine, under the **same** `wrk` configuration, in a
deterministic sequential run.

## Why this exists

Public "X is N times faster than Y" benchmarks are almost always captured on
unknown hardware, with unknown kernel tunings, unknown Node versions, unknown
concurrency, and unknown warm-up policies. Extrapolating from them to reason
about Zerly's gateway performance is misleading. This harness removes those
variables by running every stack on the caller's own machine, in the same
process model, with a fixed `wrk` invocation and a fixed warm-up policy,
emitting a single markdown report you can paste into docs or slides.

## What it measures

Five scenarios, each bound to `127.0.0.1` on a unique port, all serving the
**same path** `GET /demo/users/1` so the `wrk` command line is identical
across runs:

| # | Stack            | Port  | What it does                                                                       |
|---|------------------|-------|------------------------------------------------------------------------------------|
| 1 | `fastify-hello`  | 18081 | Minimal Fastify hello-world, returns a static JSON body.                           |
| 2 | `express-hello`  | 18082 | Same endpoint via Express (ETag and `x-powered-by` disabled for fairness).         |
| 3 | `upstream-node`  | 18090 | Fastify upstream used by the proxy scenarios. Serves `/users/1`.                   |
| 4 | `fastify-proxy`  | 18083 | Fastify front, proxies to `upstream-node` via `undici.request`, streams the reply. |
| 5 | `express-proxy`  | 18084 | Express front, proxies via Node stdlib `http.request` with a keep-alive agent.    |

**`wrk` configuration:**

- Warm-up: `-t2 -c50 -d5s` against the same endpoint, output discarded. The
  purpose is to let the V8 JIT stabilize and the keep-alive pools warm up
  before the measured window starts.
- Measured: `-t4 -c100 -d15s --latency http://127.0.0.1:<port>/demo/users/1`.

Every scenario runs in a **fresh** Node child process: started, warmed,
measured, torn down. JIT caches and connection pools are never carried between
scenarios, so the numbers reflect steady-state behavior of *that* stack in
isolation. Runs are sequential on purpose — parallel execution would contend
for CPU and produce noise-level differences.

The generated report records:

- Host kernel, Node version, `wrk` version, git HEAD short SHA, and run
  start/end timestamps.
- `Requests/sec`, p50, p75, p90, p99, max latency, and non-2xx count for each
  scenario, parsed straight out of `wrk`'s output.
- A pinned placeholder row for the Zerly gateway that the operator pastes in
  by hand after running the gateway against a live NATS container (see
  "What this harness does NOT measure" below).

## Prerequisites

- Node.js 20 or newer (checked via `engines.node` in each stack's
  `package.json`).
- npm — each stack has its own `node_modules` installed outside the repo's
  pnpm workspace, so there is zero cross-contamination with the top-level
  Nx / pnpm setup.
- `wrk` 4.x on `PATH`.
- `nc` (BSD or GNU netcat) — used to probe port readiness when starting stacks.
- `awk` — used to parse `wrk` output.
- Enough free cores that a single-threaded client and a single-threaded server
  do not contend. On a 4-core machine this is fine.

## How to run it

From the repository root:

```bash
# 1. one-time: install dependencies into every stack (creates per-stack
# node_modules; takes about 30 seconds end-to-end). Safe to re-run.
./bench-compare/run-all.sh --install

# 2. full run: execute every scenario sequentially and write a timestamped
# report under bench-compare/results/REPORT-YYYY-MM-DD-HHMMSS.md.
./bench-compare/run-all.sh

# debugging: run a single scenario (useful when a stack is flaky).
./bench-compare/run-all.sh --only fastify-hello
```

Teardown is automatic. The driver registers an `EXIT`/`INT`/`TERM` trap that
kills every background process it spawned, so `Ctrl+C` at any point leaves
the machine in a clean state.

## What this harness does NOT measure

The Zerly Gateway itself is **not** a scenario in this harness. It depends on
a live NATS JetStream container plus the `example-app` Node process plus the
Go gateway binary, each with its own startup orchestration that does not fit
into a single-file shell driver without introducing Docker as a hard
dependency.

To capture Zerly numbers:

1. Follow the setup in `apps/gateway-server/README.md` to bring up NATS and
   the gateway plus `example-app`.
2. Run `wrk -t4 -c100 -d15s --latency http://127.0.0.1:<gateway-port>/...`
   against the gateway with the same shape of request, using the same warm-up
   policy as this harness.
3. Paste the `Requests/sec`, latency percentiles, and non-2xx count into the
   pre-formatted `zerly-gateway` rows in the generated report. The rows are
   pinned at the bottom of the Results table precisely so the numbers live
   in the same document as the baseline stacks.

## Known caveats

- All stacks bind `127.0.0.1` only. No TLS, no HTTP/2, no compression, no
  auth, no CORS. Every layer that is orthogonal to "HTTP framework
  per-request overhead" is intentionally disabled, so these numbers are an
  **upper bound** on what each stack can do on this machine — real
  production numbers will always be lower.
- `wrk`'s `Requests/sec` includes non-2xx responses. Always check the
  `Non-2xx` column before quoting a number from the table.
- Lockfiles are gitignored on purpose. Each stack re-resolves its dependency
  tree on every `--install` run. This keeps scaffold diffs clean at the cost
  of slightly reduced reproducibility across long time spans. If this
  harness lives longer than a few months, commit the lockfiles.
- Runs are sensitive to other CPU-heavy processes on the host. Close your
  browser, your editor's language servers, and any other noisy process
  before running a measured pass.
