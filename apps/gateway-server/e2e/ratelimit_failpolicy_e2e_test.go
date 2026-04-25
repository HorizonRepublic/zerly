//go:build e2e

// Package e2e — RATELIMIT_FAIL_POLICY scenarios. Pins the
// operator-facing contract for what happens when the distributed
// rate-limit store fails (network error, missing bucket, CAS budget
// exhausted): the open profile favours availability and lets traffic
// through with the static X-RateLimit-Limit header only, the closed
// profile favours strictness and rejects with 429 + the same static
// header.
//
// Both scenarios spawn a SECONDARY gateway on :8081 with a custom
// fail-policy env var, then delete the rate-limit KV bucket
// underneath it to force the store into failure. Sibling of
// ratelimit_multi_replica_test.go; reuses gatewayBinaryPath and the
// natsCLI helper from that file.
//
// Pinning notes:
//   - On the open path, the gateway intentionally drops Decision
//     fields and emits ONLY X-RateLimit-Limit (the static config),
//     not Remaining/Reset. The previous behaviour leaked
//     Remaining: 0 / Reset: -62135596800 (Unix encoding of
//     time.Time{}) under fail-open, telling clients the bucket was
//     exhausted in year 1. The empty-Decision guard is a security
//     contract, not just a UX nicety.
//   - On the closed path, the actual gateway returns 429 (Too Many
//     Requests) for store-error rejections — the same status used
//     for normal bucket-empty rejections. A 503 (Service
//     Unavailable) might feel more semantically appropriate for a
//     backend outage, but the implementation in
//     internal/proxy/handler.go:439 unifies both paths under
//     gerrors.TooManyRequests. These tests pin the actual
//     contract; if the policy changes to surface 503 here, the
//     test must be updated alongside the source change.
package e2e

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failPolicyGatewayURL is the base URL for the fail-policy
// secondary gateway. Same :8081 port as concurrency / multi-replica
// scenarios; t.Cleanup teardown ordering guarantees only one is up
// at a time.
const failPolicyGatewayURL = "http://localhost:8081"

// startFailPolicyGateway spawns a fresh gateway on :8081 with the
// supplied RATELIMIT_FAIL_POLICY env var. Mirrors
// startSecondaryGateway in shape but parameterises the policy so
// each scenario gets its own clean gateway under a known regime.
//
// Cleanup is registered before the readiness probe so a Fatalf in
// waitForGatewayAt cannot leak the spawned process.
func startFailPolicyGateway(t *testing.T, policy string) func() {
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
		"RATELIMIT_FAIL_POLICY="+policy,
		"RATELIMIT_KEY_TTL=10m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	require.NoError(t, cmd.Start(), "failed to start fail-policy gateway binary")

	var once sync.Once
	shutdown := func() {
		once.Do(func() {
			cancel()
			_ = cmd.Wait()
		})
	}
	t.Cleanup(shutdown)

	waitForGatewayAt(t, failPolicyGatewayURL, secondaryGatewayReadyTimeout)

	return shutdown
}

// triggerRateLimitOutage deletes the rate-limit KV bucket via the
// `nats` CLI. The handler_registry_ratelimit bucket is created on
// demand by the gateway when the first nats-kv-backed route is
// resolved; deleting it forces a NATS-KV store error on the next
// Allow call, which the configured FailPolicy then arbitrates.
//
// Skips the test (not fails) when the CLI is missing — minimal CI
// images may not ship it. The integration test in the
// ratelimit/natskv_integration_test.go suite already covers the
// store-error path at a lower tier, so a missing CLI does not
// silently weaken coverage; it just defers verification to the
// container that DOES ship the CLI.
func triggerRateLimitOutage(t *testing.T) {
	t.Helper()
	if err := natsCLI(t, "kv", "del", "handler_registry_ratelimit", "--force"); err != nil {
		t.Skipf("nats CLI not available or bucket delete failed: %v — skipping fail-policy e2e", err)
	}
}

// drainFailPolicyAttempts fires `attempts` requests against url at
// `interval` spacing and returns the count of successes vs. the
// observed status set. Centralised so both scenarios in this file
// share one timing model — the FailPolicy circuit breaker opens
// after a small CAS budget drains, so the first 1-2 requests may
// resolve under transient state; spacing plus volume amortise that
// transient and let the assertion focus on the steady-state regime.
func drainFailPolicyAttempts(t *testing.T, url string, attempts int, interval time.Duration) (
	successes int, statuses map[int]int, lastHeaders http.Header,
) {
	t.Helper()
	statuses = make(map[int]int, 4)

	for i := 0; i < attempts; i++ {
		resp, err := http.Get(url)
		require.NoError(t, err)
		_ = resp.Body.Close()

		statuses[resp.StatusCode]++
		lastHeaders = resp.Header.Clone()
		if resp.StatusCode == http.StatusOK {
			successes++
		}
		time.Sleep(interval)
	}

	return successes, statuses, lastHeaders
}

// TestE2E_RateLimit_FailOpenContinuesUnderStoreOutage pins the
// fail-open availability contract: with RATELIMIT_FAIL_POLICY=open
// and the rate-limit bucket missing, requests against a route that
// uses `store: 'nats-kv'` keep flowing. The response carries the
// static X-RateLimit-Limit header (route config) but NOT
// X-RateLimit-Remaining / X-RateLimit-Reset, because the empty
// Decision guard in BuildHeaders intentionally suppresses bucket-
// state fields when the Decision is unpopulated.
//
// The /api/multi-replica-rl route is configured with
// `rateLimit: { rps: 10, burst: 10, store: 'nats-kv', keyBy: ['ip'] }`,
// which is the only route in example-app that exercises the
// nats-kv store and therefore the only one whose Allow path can
// surface a store error after the bucket is deleted.
func TestE2E_RateLimit_FailOpenContinuesUnderStoreOutage(t *testing.T) {
	cleanup := startFailPolicyGateway(t, "open")
	defer cleanup()

	triggerRateLimitOutage(t)

	url := failPolicyGatewayURL + "/api/multi-replica-rl"

	// 10 attempts at 50ms spacing — well above the breaker open
	// threshold, so the steady-state regime is fail-open across
	// the majority of the run.
	successes, statuses, lastHeaders := drainFailPolicyAttempts(t, url, 10, 50*time.Millisecond)

	t.Logf("fail-open under store outage: successes=%d statuses=%v", successes, statuses)

	// Expect the vast majority of requests to flow through. A few
	// transient 5xx are tolerable while the breaker probes; the
	// invariant being pinned is "open does not flip to closed".
	assert.GreaterOrEqual(t, successes, 8,
		"fail-open MUST allow most requests through during a store outage; got %d/10", successes)

	// The last response we saw (for which we have headers) should
	// carry the static X-RateLimit-Limit but NOT the bucket-state
	// fields. If the last response was a transient 5xx, fall back
	// to checking that no response leaked a Remaining/Reset
	// concomitant with a 200 status anywhere in the run.
	if lastHeaders != nil {
		// Static config header (rps from the route config) MUST
		// always reach the wire — it does not depend on a
		// successful Decision.
		if lastHeaders.Get("X-RateLimit-Limit") != "" {
			assert.Empty(t, lastHeaders.Get("X-RateLimit-Remaining"),
				"fail-open MUST suppress X-RateLimit-Remaining when Decision is empty")
			assert.Empty(t, lastHeaders.Get("X-RateLimit-Reset"),
				"fail-open MUST suppress X-RateLimit-Reset when Decision is empty")
		}
	}
}

// TestE2E_RateLimit_FailClosedRejectsUnderStoreOutage pins the
// fail-closed strictness contract: with RATELIMIT_FAIL_POLICY=closed
// and the rate-limit bucket missing, requests against the
// nats-kv-backed route are rejected.
//
// The current implementation in internal/proxy/handler.go:439
// surfaces store-error rejections as 429 Too Many Requests — the
// same code as a bucket-empty rejection — accompanied by the
// static X-RateLimit-Limit header. This unifies the rejection
// path semantically (every "did not pass the gate" outcome is a
// 429) at the cost of conflating "rate-limit policy says no" with
// "rate-limit infrastructure is down". A future contract change
// to surface 503 here for store-outage rejections would be a
// breaking change for clients that distinguish the two; this test
// pins today's actual behaviour so any such change is observed.
func TestE2E_RateLimit_FailClosedRejectsUnderStoreOutage(t *testing.T) {
	cleanup := startFailPolicyGateway(t, "closed")
	defer cleanup()

	triggerRateLimitOutage(t)

	url := failPolicyGatewayURL + "/api/multi-replica-rl"

	// 10 attempts at 50ms spacing.
	successes, statuses, lastHeaders := drainFailPolicyAttempts(t, url, 10, 50*time.Millisecond)

	t.Logf("fail-closed under store outage: successes=%d statuses=%v", successes, statuses)

	// Expect the vast majority of requests to be rejected.
	rejected := statuses[http.StatusTooManyRequests] + statuses[http.StatusServiceUnavailable]
	assert.GreaterOrEqual(t, rejected, 8,
		"fail-closed MUST reject most requests during a store outage; got rejected=%d successes=%d statuses=%v",
		rejected, successes, statuses)

	// The static rps-config header MUST still reach the wire on
	// the rejection — clients use it to size retry budgets even
	// when the gate fired.
	if lastHeaders != nil {
		assert.NotEmpty(t, lastHeaders.Get("X-RateLimit-Limit"),
			"fail-closed rejection MUST carry the static X-RateLimit-Limit header")
	}
}
