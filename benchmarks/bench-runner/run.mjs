#!/usr/bin/env node
// Bench orchestration entry point. Invoked by run-all.sh *after*
// the target stack has been started via `docker compose --profile
// <name> up -d --wait`.
//
// Responsibilities:
//   1. Wait for the target port to be accepting TCP + answering
//      200 on the happy-path route (the Go gateway has no container
//      healthcheck, so readiness has to be re-verified from the
//      host).
//   2. Drive autocannon for each scenario (hello, teapot).
//   3. Emit one JSON file per (stack, scenario) under ./results/
//      and one roll-up markdown report covering all stacks that ran
//      in this invocation.
//
// autocannon is fetched on demand via `npx -y autocannon@8` from
// run-all.sh — we never bundle it here because the project is
// already large enough and this tool runs at most once per bench
// session. Programmatic usage keeps scenario handling / per-run
// warmup inside Node rather than shelling out.

import autocannon from 'autocannon';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const resultsDir = join(__dirname, '..', 'results');

mkdirSync(resultsDir, { recursive: true });

const STACKS = {
  'nest-fastify': 'http://127.0.0.1:18091',
  'nest-express': 'http://127.0.0.1:18092',
  zerly: 'http://127.0.0.1:18093',
  'nest-fastify-bun': 'http://127.0.0.1:18094',
  'nest-express-bun': 'http://127.0.0.1:18095',
  'zerly-bun': 'http://127.0.0.1:18093',
};

const SCENARIOS = [
  { name: 'hello-sync', path: '/bench/hello', expectedStatus: 200 },
  { name: 'hello-async', path: '/bench/hello-async', expectedStatus: 200 },
  { name: 'teapot', path: '/bench/teapot', expectedStatus: 418 },
];

// Tuning: 30s per scenario is long enough to average out JIT warmup
// and short enough that a full matrix finishes in under 5 minutes.
// Connections = 50 is a realistic fanout for a single dev machine;
// pipelining 1 so per-request latency numbers stay meaningful.
const DURATION_SECONDS = 30;
const CONNECTIONS = 2000;
const PIPELINING = 1;
const WARMUP_SECONDS = 3;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const waitForReady = async (url) => {
  const deadline = Date.now() + 60_000;
  let lastErr;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url);
      if (res.status < 500) {
        return;
      }
      lastErr = new Error(`status ${res.status}`);
    } catch (err) {
      lastErr = err;
    }

    await sleep(500);
  }

  throw new Error(`readiness timeout for ${url}: ${lastErr?.message ?? 'unknown'}`);
};

const runAutocannon = (opts) =>
  new Promise((resolve, reject) => {
    autocannon(opts, (err, result) => {
      if (err) {
        reject(err);

        return;
      }

      resolve(result);
    });
  });

const runScenario = async ({ stack, baseUrl, scenario }) => {
  const url = `${baseUrl}${scenario.path}`;
  // Tiny warmup pass — gives V8 a few hundred requests to tier up
  // and flushes any first-connection JIT noise out of the real run.
  await runAutocannon({
    url,
    connections: CONNECTIONS,
    pipelining: PIPELINING,
    duration: WARMUP_SECONDS,
    expectBody: undefined,
  });

  const result = await runAutocannon({
    url,
    connections: CONNECTIONS,
    pipelining: PIPELINING,
    duration: DURATION_SECONDS,
  });

  return {
    stack,
    scenario: scenario.name,
    url,
    expectedStatus: scenario.expectedStatus,
    requests: result.requests,
    latency: result.latency,
    throughput: result.throughput,
    errors: result.errors,
    timeouts: result.timeouts,
    non2xx: result.non2xx,
    duration: result.duration,
  };
};

const parseStackFilter = () => {
  const arg = process.argv[2];
  if (!arg) {
    return Object.keys(STACKS);
  }

  if (!(arg in STACKS)) {
    throw new Error(
      `unknown stack "${arg}"; expected one of: ${Object.keys(STACKS).join(', ')}`,
    );
  }

  return [arg];
};

const formatNumber = (n) => (typeof n === 'number' ? n.toFixed(2) : String(n));

const renderMarkdown = (runs, startedAt) => {
  const lines = [];
  lines.push(`# Bench report — ${startedAt}`);
  lines.push('');
  lines.push(
    `Duration: ${DURATION_SECONDS}s per scenario, warmup ${WARMUP_SECONDS}s, ` +
      `${CONNECTIONS} connections, pipelining ${PIPELINING}.`,
  );
  lines.push('');
  lines.push('| Stack | Scenario | Req/s | Latency avg (ms) | Latency p99 (ms) | Throughput (MB/s) | non-2xx | errors |');
  lines.push('|---|---|---:|---:|---:|---:|---:|---:|');

  for (const r of runs) {
    lines.push(
      `| ${r.stack} | ${r.scenario} | ${formatNumber(r.requests.average)} | ${formatNumber(
        r.latency.average,
      )} | ${formatNumber(r.latency.p99)} | ${formatNumber(
        (r.throughput.average ?? 0) / (1024 * 1024),
      )} | ${r.non2xx} | ${r.errors} |`,
    );
  }

  lines.push('');

  return lines.join('\n');
};

const main = async () => {
  const stacks = parseStackFilter();
  const startedAt = new Date().toISOString().replace(/[:.]/g, '-');
  const runs = [];

  for (const stack of stacks) {
    const baseUrl = STACKS[stack];
    // eslint-disable-next-line no-console
    console.log(`[bench] waiting for ${stack} at ${baseUrl} ...`);
    await waitForReady(`${baseUrl}/bench/hello`);

    for (const scenario of SCENARIOS) {
      // eslint-disable-next-line no-console
      console.log(`[bench] running ${stack}/${scenario.name}`);
      const result = await runScenario({ stack, baseUrl, scenario });
      runs.push(result);

      const jsonPath = join(resultsDir, `${startedAt}_${stack}_${scenario.name}.json`);
      writeFileSync(jsonPath, JSON.stringify(result, null, 2));
    }
  }

  const mdPath = join(resultsDir, `${startedAt}_report.md`);
  writeFileSync(mdPath, renderMarkdown(runs, startedAt));
  // eslint-disable-next-line no-console
  console.log(`[bench] wrote ${mdPath}`);
};

main().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('[bench] fatal:', err);
  process.exit(1);
});
