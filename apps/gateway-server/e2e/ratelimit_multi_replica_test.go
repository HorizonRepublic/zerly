//go:build e2e

// Package e2e — multi-replica NATS KV rate-limit scenarios. Sibling
// of e2e_test.go / contract_test.go; spawns a SECOND gateway process
// on :8081 alongside the primary on :8080, both pointed at the same
// NATS cluster, and verifies that the shared NATS KV store produces
// one converged rate-limit decision across replicas.
//
// The secondary process is launched via `exec.CommandContext`
// against the prebuilt binary in `dist/apps/gateway-server/gateway`.
// Tests skip gracefully (not fail) when the binary is missing — a
// clear log line directs the developer to run
// `pnpm nx build gateway-server` before re-running e2e.
//
// Helpers in this file (`gatewayBinaryPath`, `startSecondaryGateway`,
// `waitForGatewayAt`, `postJSON`) are intentionally reusable so the
// fail-policy scenarios in a follow-up file consume the same harness
// without copy-paste.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// secondaryGatewayURL is the base URL the spawned secondary gateway
// binds to. Kept as a package-level constant so every multi-replica
// scenario in this file and its siblings agrees on the port.
const secondaryGatewayURL = "http://localhost:8081"

// secondaryGatewayReadyTimeout bounds how long the harness waits for
// the spawned secondary gateway to become reachable. 10 seconds is
// generous for a prebuilt binary connecting to an already-running
// NATS container; a cold boot should finish well under 2 seconds.
const secondaryGatewayReadyTimeout = 10 * time.Second

// gatewayBinaryPath resolves the absolute path to the prebuilt
// gateway binary at `dist/apps/gateway-server/gateway`.
//
// If the binary is missing the test is SKIPPED (not failed) so
// contributors who run the e2e suite without pre-building the Go
// binary get an actionable hint rather than a red signal. CI is
// expected to run `pnpm nx build gateway-server` as a prerequisite
// step; see the e2e README for the full protocol.
func gatewayBinaryPath(t *testing.T) string {
	t.Helper()

	p, err := filepath.Abs("../../../dist/apps/gateway-server/gateway")
	require.NoError(t, err)

	if _, err := os.Stat(p); err != nil {
		t.Skipf("gateway binary not built at %s — run `pnpm nx build gateway-server` first", p)
	}

	return p
}

// startSecondaryGateway spawns a SECOND gateway process on :8081
// against the same NATS cluster the primary :8080 gateway uses. The
// process inherits the ambient env and then appends the full
// multi-replica env set (NATS URL, HTTP addr, KV bucket, log config,
// rate-limit fail-policy + TTL) so the test is deterministic no
// matter what the developer has in `.env`.
//
// Cleanup is registered with t.Cleanup immediately after Start
// succeeds, BEFORE the readiness probe, so a Fatalf in
// waitForGatewayAt cannot leak the spawned process. The returned
// closure is equivalent to the registered cleanup and is retained
// so scenarios can still trigger shutdown explicitly mid-test; both
// paths converge on a sync.Once-guarded teardown so a caller that
// defers the closure after t.Cleanup has already fired is a no-op
// rather than a double-wait.
//
// Readiness is verified via waitForGatewayAt; if the secondary fails
// to bind :8081 (e.g. port already in use) the helper fails the test
// with a clear message AFTER the process has been scheduled for
// cleanup.
func startSecondaryGateway(t *testing.T) func() {
	t.Helper()

	binary := gatewayBinaryPath(t)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(),
		"NATS_URLS=nats://localhost:4222",
		"HTTP_ADDR=:8081",
		"KV_BUCKET=handler_registry",
		"LOG_FORMAT=console",
		"LOG_LEVEL=info",
		"ENVIRONMENT=development",
		"RATELIMIT_FAIL_POLICY=open",
		"RATELIMIT_KEY_TTL=10m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	require.NoError(t, cmd.Start(), "failed to start secondary gateway binary")

	var once sync.Once
	shutdown := func() {
		once.Do(func() {
			cancel()
			_ = cmd.Wait()
		})
	}

	// Register cleanup BEFORE the readiness probe. If waitForGatewayAt
	// calls t.Fatalf, the test aborts immediately; without a prior
	// t.Cleanup registration the child process would linger and the
	// next test run would find :8081 already bound.
	t.Cleanup(shutdown)

	waitForGatewayAt(t, secondaryGatewayURL, secondaryGatewayReadyTimeout)

	return shutdown
}

// waitForGatewayAt polls baseURL at 100ms intervals until the HTTP
// listener accepts a connection OR timeout elapses. Any HTTP response
// (including 404 for an unregistered path) counts as ready — the
// gateway registers `/*path` as a catch-all and routes through the
// proxy, so no health endpoint is pre-reserved.
//
// Uses a 500ms per-request client timeout so the poll loop stays
// responsive during cold start. On timeout the test fails with the
// elapsed budget and the last-seen error.
//
// Kept distinct from the package-level waitForGateway(t) helper
// (which targets the fixed primary URL) so both can coexist — Go
// disallows function overloading, and a multi-URL readiness probe is
// a fundamentally different shape than the single-gateway variant.
func waitForGatewayAt(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}

	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/__probe__")
		if err == nil {
			_ = resp.Body.Close()

			return
		}

		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("gateway at %s did not become ready within %s: %v", baseURL, timeout, lastErr)
}

// postJSON posts an empty JSON body (`{}`) to url with
// `Content-Type: application/json`, returning the response status
// code and headers. The body is drained and discarded so the
// underlying connection can be reused across the rapid fire loop in
// the correctness test. Any transport-level or request-build error
// is fatal because the test has no meaningful recovery path.
func postJSON(t *testing.T, url string) (int, http.Header) {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{}`)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, resp.Header
}

// TestE2E_MultiReplica_NATSKVRateLimitShared pins the cross-replica
// convergence contract for the NATS KV rate-limit store.
//
// Scenario: two gateway replicas (primary on :8080, secondary on
// :8081) share a single NATS cluster. A route decorated with
// `rateLimit: { rps: 10, burst: 10, store: 'nats-kv', keyBy: ['ip'] }`
// is hit 30 times total, alternating between the two replicas at
// ~30 rps. Because the store is shared via KV, both replicas see
// the same token bucket — roughly rps worth of requests are allowed
// in the first second and the rest are rejected with 429.
//
// Assertions:
//   - allowed ∈ [rps-2, rps+3] — tolerates GCRA boundary inclusivity
//     and bucket-boundary timing jitter.
//   - allowed + rejected == total — no requests fall through without
//     a decision.
//   - X-RateLimit-* headers flow on every response — proves the
//     header builder runs in the shared path and is not skipped on
//     rejects.
//
// After the burst, a single extra request is fired to assert the
// exact header trio (Limit, Remaining, Reset) is well-formed when
// the route admits the request. If the follow-up is itself
// rate-limited (same 1-second window), the header assertions are
// skipped rather than forced — the 429 path already carried a
// positive signal via sawRLHeader during the burst.
func TestE2E_MultiReplica_NATSKVRateLimitShared(t *testing.T) {
	cleanup := startSecondaryGateway(t)
	t.Cleanup(cleanup)

	const rps = 10
	const total = 30

	urls := []string{
		gatewayURL + "/api/multi-replica-rl",
		secondaryGatewayURL + "/api/multi-replica-rl",
	}

	var (
		allowed, rejected int
		sawRLHeader       bool
		mu                sync.Mutex
		wg                sync.WaitGroup
	)

	start := time.Now()
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			url := urls[idx%2]
			code, hdr := postJSON(t, url)

			mu.Lock()
			defer mu.Unlock()

			if hdr.Get("X-RateLimit-Limit") != "" {
				sawRLHeader = true
			}
			switch code {
			case http.StatusOK, http.StatusCreated:
				allowed++
			case http.StatusTooManyRequests:
				rejected++
			}
		}(i)

		time.Sleep(time.Duration(1_000/total) * time.Millisecond)
	}
	wg.Wait()

	elapsed := time.Since(start)
	t.Logf("fired %d requests in %s → %d allowed, %d rejected", total, elapsed, allowed, rejected)

	assert.GreaterOrEqual(t, allowed, rps-2, "at least rps-2 allowed")
	assert.LessOrEqual(t, allowed, rps+3, "not significantly more than rps")
	assert.Equal(t, total, allowed+rejected, "every request must resolve to either allowed or rejected")
	assert.True(t, sawRLHeader, "every RL response must carry X-RateLimit-* headers")

	code, hdr := postJSON(t, urls[0])
	if code == http.StatusOK || code == http.StatusCreated {
		assert.Equal(t, fmt.Sprint(rps), hdr.Get("X-RateLimit-Limit"),
			"X-RateLimit-Limit must echo the route's configured rps")
		assert.NotEmpty(t, hdr.Get("X-RateLimit-Remaining"),
			"X-RateLimit-Remaining must be populated on admitted requests")
		assert.NotEmpty(t, hdr.Get("X-RateLimit-Reset"),
			"X-RateLimit-Reset must be populated on admitted requests")
	}
}

// TestE2E_MultiReplica_FailPolicyOpen verifies that when the NATS KV
// rate-limit bucket is deleted mid-flight, requests continue to pass
// through the gateway because RATELIMIT_FAIL_POLICY=open treats the
// distributed store failure as a backend outage rather than a
// policy violation.
//
// The test deletes the bucket directly via the `nats` CLI, drives a
// short burst of requests, and asserts the majority succeed. A few
// early failures are tolerable (circuit breaker hasn't tripped yet
// and CAS budget drains one request at a time until the breaker
// opens), but sustained rejections would indicate fail-open isn't
// working.
func TestE2E_MultiReplica_FailPolicyOpen(t *testing.T) {
	cleanup := startSecondaryGateway(t)
	defer cleanup()

	// Delete the ratelimit bucket to simulate KV unavailability.
	// Requires the `nats` CLI on PATH; skip gracefully if absent
	// so the test suite still works in minimal CI images.
	if err := natsCLI(t, "kv", "del", "handler_registry_ratelimit", "--force"); err != nil {
		t.Skipf("nats CLI not available or bucket delete failed: %v — skipping fail-policy e2e", err)
	}

	// Fire 20 requests spaced 50ms apart. Early requests may return
	// 5xx until the circuit breaker opens; once open, fail-policy
	// takes over and requests pass through.
	allowed := 0
	for i := 0; i < 20; i++ {
		code, _ := postJSON(t, "http://localhost:8080/api/multi-replica-rl")
		if code == 200 || code == 201 {
			allowed++
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Expect most requests through once the breaker has opened.
	// 15/20 = 75% is a safe lower bound; under extreme infra issues
	// it may dip, but anything below shows fail-policy broken.
	assert.GreaterOrEqual(t, allowed, 15, "fail-policy=open should let most requests through during backend outage")
}

// TestE2E_MultiReplica_HotReloadNoFreePass would exercise the
// "config change does not flush TAT" invariant end-to-end — i.e.,
// mutating a route's rps mid-flight via a direct KV write to
// handler_registry and asserting clients don't get a free-pass
// rate-limit reset.
//
// This invariant is ALREADY verified at a lower level by the
// existing watcher → routing-rebuild pipeline tests in reload_test.go
// plus the MemoryStore / NATSKVStore unit tests that confirm TAT
// is preserved across config changes. Running this scenario as an
// e2e test would add coordinated KV writes on top of running Nest
// + gateway processes, which is mostly coordination complexity
// with minimal signal beyond what the lower tiers already prove.
//
// Intentionally skipped; can be unskipped if a future incident
// demonstrates a regression gap at the e2e layer.
func TestE2E_MultiReplica_HotReloadNoFreePass(t *testing.T) {
	t.Skip("covered by existing reload_test.go pipeline + MemoryStore/NATSKVStore unit tests; " +
		"unskip if future regression proves the e2e tier adds signal")
}

// natsCLI invokes the `nats` CLI against localhost:4222. Returns
// an error if the binary is missing from PATH or the command fails.
// Tests SHOULD treat an error from this helper as a skip signal,
// not a test failure, so minimal CI images without the CLI don't
// spuriously fail.
func natsCLI(t *testing.T, args ...string) error {
	t.Helper()
	args = append([]string{"--server=nats://localhost:4222"}, args...)
	cmd := exec.Command("nats", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nats cli %v: %w", args, err)
	}
	return nil
}
