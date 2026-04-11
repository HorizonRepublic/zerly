#!/usr/bin/env bash
#
# Zerly Gateway benchmark comparison harness.
#
# Runs a sequential wrk load test across several HTTP stacks on
# localhost and writes a deterministic markdown report to
# bench-compare/results/. See ./README.md for the motivation and for
# what the numbers do and do not mean.
#
# Safe to Ctrl+C — the EXIT trap kills every process the script
# started, so no orphaned node servers are left behind.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STACKS_DIR="$SCRIPT_DIR/stacks"
RESULTS_DIR="$SCRIPT_DIR/results"

# wrk configuration. Keep in sync with the README.
WARMUP_THREADS=2
WARMUP_CONNS=50
WARMUP_DURATION=5
MEASURED_THREADS=4
MEASURED_CONNS=100
MEASURED_DURATION=15

# Scenario metadata, pipe-separated tuples of:
#   name|port|dir|needs_upstream
# needs_upstream=1 means the upstream-node stack must be running on
# UPSTREAM_PORT before this scenario's front starts.
SCENARIOS=(
  "fastify-hello|18081|fastify-hello|0"
  "express-hello|18082|express-hello|0"
  "fastify-proxy|18083|fastify-proxy|1"
  "express-proxy|18084|express-proxy|1"
)

UPSTREAM_PORT=18090

# PID registry used by the EXIT trap. Every background process the
# script spawns is appended here and mass-killed on teardown.
declare -a SPAWNED_PIDS=()

cleanup() {
  local pid
  for pid in "${SPAWNED_PIDS[@]:-}"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done
  sleep 0.2
  for pid in "${SPAWNED_PIDS[@]:-}"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -KILL "$pid" 2>/dev/null || true
    fi
  done
}
trap cleanup EXIT INT TERM

require_binary() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    echo "ERROR: '$name' is required but not on PATH" >&2
    exit 1
  fi
}

wait_for_port() {
  local port="$1"
  local deadline=$(( SECONDS + 10 ))
  while (( SECONDS < deadline )); do
    if nc -z 127.0.0.1 "$port" 2>/dev/null; then
      return 0
    fi
    sleep 0.1
  done
  echo "ERROR: port $port did not come up within 10s" >&2
  return 1
}

start_stack() {
  local dir="$1"
  local port="$2"
  local stack_path="$STACKS_DIR/$dir"
  if [[ ! -d "$stack_path/node_modules" ]]; then
    echo "ERROR: $dir has no node_modules — run $0 --install first" >&2
    exit 1
  fi
  ( cd "$stack_path" && PORT="$port" node server.js >"/tmp/zerly-bench-$dir.log" 2>&1 ) &
  local pid=$!
  SPAWNED_PIDS+=("$pid")
  if ! wait_for_port "$port"; then
    echo "  log tail for $dir:" >&2
    tail -n 20 "/tmp/zerly-bench-$dir.log" >&2 || true
    return 1
  fi
}

stop_stack() {
  local port="$1"
  local pid
  for pid in "${SPAWNED_PIDS[@]:-}"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done
  # Rebuild SPAWNED_PIDS with dead PIDs filtered out so subsequent
  # teardowns do not re-signal zombies.
  local alive=()
  for pid in "${SPAWNED_PIDS[@]:-}"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      alive+=("$pid")
    fi
  done
  SPAWNED_PIDS=( "${alive[@]:-}" )
  # Give the OS a moment to release the port before the next stack
  # tries to bind it.
  local deadline=$(( SECONDS + 3 ))
  while (( SECONDS < deadline )); do
    if ! nc -z 127.0.0.1 "$port" 2>/dev/null; then
      return 0
    fi
    sleep 0.1
  done
}

run_wrk_measured() {
  local port="$1"
  wrk -t"$MEASURED_THREADS" -c"$MEASURED_CONNS" -d"${MEASURED_DURATION}s" --latency \
    "http://127.0.0.1:$port/demo/users/1"
}

run_wrk_warmup() {
  local port="$1"
  wrk -t"$WARMUP_THREADS" -c"$WARMUP_CONNS" -d"${WARMUP_DURATION}s" \
    "http://127.0.0.1:$port/demo/users/1" >/dev/null
}

# Extracts a single pipe-separated line from a wrk output blob:
#   rps|p50|p75|p90|p99|max|non2xx
# Latency values keep their unit suffix (e.g. "1.23ms", "412us") so
# the report table shows wrk's own formatting verbatim.
parse_wrk() {
  local raw="$1"
  local rps p50 p75 p90 p99 max non2xx
  rps=$(printf '%s\n' "$raw" | awk '/Requests\/sec:/ {print $2}')
  p50=$(printf '%s\n' "$raw" | awk '$1=="50%"{print $2}')
  p75=$(printf '%s\n' "$raw" | awk '$1=="75%"{print $2}')
  p90=$(printf '%s\n' "$raw" | awk '$1=="90%"{print $2}')
  p99=$(printf '%s\n' "$raw" | awk '$1=="99%"{print $2}')
  max=$(printf '%s\n' "$raw" | awk '/^ *Latency / {print $4; exit}')
  non2xx=$(printf '%s\n' "$raw" | awk '/Non-2xx or 3xx responses:/ {print $4}')
  [[ -z "$non2xx" ]] && non2xx=0
  printf '%s|%s|%s|%s|%s|%s|%s' "$rps" "$p50" "$p75" "$p90" "$p99" "$max" "$non2xx"
}

bench_scenario() {
  local name="$1" port="$2" dir="$3" needs_upstream="$4"

  echo ""
  echo "=== $name ==="

  if [[ "$needs_upstream" == "1" ]]; then
    echo "  starting upstream-node on $UPSTREAM_PORT ..."
    start_stack "upstream-node" "$UPSTREAM_PORT"
  fi

  echo "  starting $name on $port ..."
  start_stack "$dir" "$port"

  echo "  warm-up (${WARMUP_DURATION}s) ..."
  run_wrk_warmup "$port"

  echo "  measured run (${MEASURED_DURATION}s) ..."
  local raw
  raw=$(run_wrk_measured "$port")
  printf '%s\n' "$raw"

  RESULTS[$name]=$(parse_wrk "$raw")

  stop_stack "$port" || true
  if [[ "$needs_upstream" == "1" ]]; then
    stop_stack "$UPSTREAM_PORT" || true
  fi
}

install_all() {
  local dir
  for dir in fastify-hello express-hello upstream-node fastify-proxy express-proxy; do
    echo "  npm install in $dir ..."
    ( cd "$STACKS_DIR/$dir" && npm install --silent --no-audit --no-fund --no-progress )
  done
  echo "  done."
}

write_report() {
  local path="$1"
  local hw_info node_v wrk_v git_sha started_at finished_at
  hw_info=$(uname -srm)
  node_v=$(node --version 2>/dev/null || echo "unknown")
  wrk_v=$(wrk --version 2>&1 | head -1 || echo "unknown")
  git_sha=$(git -C "$SCRIPT_DIR/.." rev-parse --short HEAD 2>/dev/null || echo "unknown")
  started_at="$REPORT_STARTED_AT"
  finished_at=$(date +"%Y-%m-%d %H:%M:%S %z")

  {
    echo "# Zerly Gateway — HTTP stack benchmark comparison"
    echo ""
    echo "| Field | Value |"
    echo "|---|---|"
    echo "| Host kernel | $hw_info |"
    echo "| Node.js | $node_v |"
    echo "| wrk | $wrk_v |"
    echo "| Git HEAD | $git_sha |"
    echo "| Run started | $started_at |"
    echo "| Run finished | $finished_at |"
    echo "| Warm-up | ${WARMUP_THREADS}t / ${WARMUP_CONNS}c / ${WARMUP_DURATION}s (discarded) |"
    echo "| Measured | ${MEASURED_THREADS}t / ${MEASURED_CONNS}c / ${MEASURED_DURATION}s |"
    echo "| Path | \`/demo/users/1\` |"
    echo ""
    echo "## Results"
    echo ""
    echo "| Stack | Req/sec | p50 | p75 | p90 | p99 | Max | Non-2xx |"
    echo "|---|---:|---:|---:|---:|---:|---:|---:|"
    local name
    for name in fastify-hello express-hello fastify-proxy express-proxy; do
      if [[ -n "${RESULTS[$name]:-}" ]]; then
        local fields
        IFS='|' read -r -a fields <<< "${RESULTS[$name]}"
        printf '| %s | %s | %s | %s | %s | %s | %s | %s |\n' \
          "$name" "${fields[0]}" "${fields[1]}" "${fields[2]}" "${fields[3]}" \
          "${fields[4]}" "${fields[5]}" "${fields[6]}"
      else
        printf '| %s | _skipped_ | | | | | | |\n' "$name"
      fi
    done
    echo "| **zerly-gateway (NATS round-trip)** | _paste from wrk run against live stack_ | | | | | | |"
    echo "| **zerly-gateway (404 no-upstream path)** | _paste from wrk run against live stack_ | | | | | | |"
    echo ""
    echo "## Scenario definitions"
    echo ""
    echo "- **fastify-hello**: \`GET /demo/users/1\` returns a static JSON object. Single process, no upstream."
    echo "- **express-hello**: Same endpoint via Express (ETag and x-powered-by disabled for fairness)."
    echo "- **fastify-proxy**: \`GET /demo/users/1\` forwarded via \`undici.request\` to a separate upstream-node process."
    echo "- **express-proxy**: Same but via Node stdlib \`http.request\` with a keep-alive agent."
    echo "- **zerly-gateway (NATS)**: Our gateway fronting \`example-app\` via Core NATS request/reply. Captured manually per the README because this stack requires a live NATS container plus the example-app Node process."
    echo "- **zerly-gateway (404)**: Our gateway's routing-miss path (no NATS hop). Also captured manually."
    echo ""
    echo "## Caveats"
    echo ""
    echo "- All stacks bind \`127.0.0.1\` only. No TLS, no HTTP/2, no compression, no auth, no CORS — every layer is disabled to measure framework overhead on a best-case path."
    echo "- Warm-up is 5 seconds at half the measured concurrency, discarded. Measured window is 15 seconds at 4 threads × 100 connections."
    echo "- wrk's \`Requests/sec\` is throughput including non-2xx responses — inspect the Non-2xx column before trusting the number."
    echo "- Node processes are fresh per scenario: every scenario starts a brand new child process, runs, and is torn down before the next scenario begins. This keeps JIT cache state and connection pool state isolated."
    echo "- The script is sequential on purpose. Parallel runs would contend for CPU and produce noise-level differences."
  } > "$path"
}

main() {
  require_binary node
  require_binary npm
  require_binary wrk
  require_binary nc
  require_binary awk

  # Associative array used to stash parsed wrk output per scenario.
  # Declared inside main (after the functions above are defined) so
  # the script remains a single sourceable file even though bash
  # requires the declaration to exist before any RESULTS[...] write.
  declare -gA RESULTS

  REPORT_STARTED_AT=$(date +"%Y-%m-%d %H:%M:%S %z")

  local only=""
  local install_only=0
  while (( $# > 0 )); do
    case "$1" in
      --install) install_only=1; shift ;;
      --only) only="$2"; shift 2 ;;
      -h|--help)
        cat <<EOF
Usage: $0 [--install] [--only NAME]

  --install     Run \`npm install\` in each stack directory and exit.
  --only NAME   Run a single scenario (for debugging).
  (no flags)    Run every scenario sequentially and write a report.
EOF
        exit 0
        ;;
      *)
        echo "unknown flag: $1" >&2
        exit 1
        ;;
    esac
  done

  if (( install_only == 1 )); then
    install_all
    exit 0
  fi

  mkdir -p "$RESULTS_DIR"

  local entry name port dir needs_upstream
  for entry in "${SCENARIOS[@]}"; do
    IFS='|' read -r name port dir needs_upstream <<< "$entry"
    if [[ -n "$only" && "$only" != "$name" ]]; then
      continue
    fi
    bench_scenario "$name" "$port" "$dir" "$needs_upstream"
  done

  local report_path="$RESULTS_DIR/REPORT-$(date +%Y-%m-%d-%H%M%S).md"
  write_report "$report_path"
  echo ""
  echo "Report: $report_path"
}

main "$@"
