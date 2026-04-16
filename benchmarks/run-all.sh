#!/usr/bin/env bash
# Benchmark orchestrator. Runs one or more per-stack docker-compose
# profiles in isolation, benches each from the host via the
# bench-runner node script, and tears everything down cleanly.
#
# Usage:
#   ./run-all.sh                  # all stacks
#   ./run-all.sh nest-fastify     # single stack
#   STACKS="nest-fastify zerly" ./run-all.sh
#
# The zerly stack needs `dist/libs/gateway-sdk` pre-built on the
# host — we do that with `pnpm nx build gateway-sdk` before the
# docker image build and COPY the output into the bench-app's
# `vendor/gateway-sdk` directory so the `file:` workspace link
# resolves offline inside the container.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$ROOT_DIR/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

ALL_STACKS=(nest-fastify nest-express zerly nest-fastify-bun nest-express-bun zerly-bun)

if [[ $# -gt 0 ]]; then
  STACKS=("$@")
elif [[ -n "${STACKS:-}" ]]; then
  # shellcheck disable=SC2206
  STACKS=(${STACKS})
else
  STACKS=("${ALL_STACKS[@]}")
fi

echo "[bench] stacks: ${STACKS[*]}"

# Install autocannon into bench-runner/node_modules if missing.
# We keep this out of the root package.json on purpose — autocannon
# is only ever needed when someone explicitly runs the benchmarks.
if [[ ! -d "$ROOT_DIR/bench-runner/node_modules/autocannon" ]]; then
  echo "[bench] installing bench-runner deps (autocannon)"
  (cd "$ROOT_DIR/bench-runner" && npm install --no-audit --no-fund --no-progress)
fi

needs_zerly=0
for stack in "${STACKS[@]}"; do
  if [[ "$stack" == "zerly" || "$stack" == "zerly-bun" ]]; then
    needs_zerly=1
  fi
done

# Zerly bench-app depends on a pre-built @zerly/gateway-sdk dist so
# the container build has a self-contained `file:` workspace link.
# We build it once per invocation even if the user is rerunning, to
# make sure in-development SDK changes actually land in the bench.
if [[ "$needs_zerly" == "1" ]]; then
  echo "[bench] building @zerly/gateway-sdk"
  (cd "$REPO_ROOT" && pnpm nx build gateway-sdk)

  SDK_DIST="$REPO_ROOT/dist/libs/gateway-sdk"
  VENDOR_DIR="$ROOT_DIR/stacks/zerly/nest-bench-app/vendor/gateway-sdk"
  rm -rf "$VENDOR_DIR"
  mkdir -p "$(dirname "$VENDOR_DIR")"
  cp -R "$SDK_DIST" "$VENDOR_DIR"
  echo "[bench] vendored gateway-sdk at $VENDOR_DIR"
fi

# Best-effort global teardown: nuke every known profile on exit.
# `docker compose down` is idempotent — hitting a profile that was
# never started, or one that was already torn down cleanly inside
# the loop, is a no-op. This keeps the trap logic simple and makes
# sure an interrupted run never leaves bench containers around.
teardown() {
  for profile in "${ALL_STACKS[@]}"; do
    docker compose -f "$COMPOSE_FILE" --profile "$profile" down --volumes --remove-orphans >/dev/null 2>&1 || true
  done
}
trap teardown EXIT

for stack in "${STACKS[@]}"; do
  echo "[bench] bringing up profile: $stack"
  docker compose -f "$COMPOSE_FILE" --profile "$stack" up -d --build --wait

  echo "[bench] running bench-runner for $stack"
  (cd "$ROOT_DIR/bench-runner" && node run.mjs "$stack")

  echo "[bench] stopping profile: $stack"
  docker compose -f "$COMPOSE_FILE" --profile "$stack" down --volumes --remove-orphans
done

echo "[bench] done — reports written to $ROOT_DIR/results/"
