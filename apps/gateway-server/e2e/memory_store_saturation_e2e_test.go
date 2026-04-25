//go:build e2e

// Package e2e — MemoryStore cardinality-cap saturation scenario.
// Pins the operator-facing contract for RATELIMIT_MEMORY_MAX_ENTRIES:
// once the in-process MemoryStore reaches the cap, brand-new keys
// surface ErrMemoryStoreSaturated from Allow(), which the gateway
// then routes through the configured RATELIMIT_FAIL_POLICY.
//
// This test runs the closed FailPolicy under a tight cap=10 cap
// against a synthetic route keyed by an `X-API-Key` header. 15
// distinct keys are sent; the first 10 land in the bucket map, the
// remaining 5 hit the saturation guard and are rejected. The
// rejection status is whatever the implementation surfaces for a
// FailPolicy=closed store-error rejection — see
// ratelimit_failpolicy_e2e_test.go for the matching contract pin
// (today: 503 Service Unavailable + the static X-RateLimit-Limit
// header). The rejected-count tally accepts both 429 and 503 to
// stay resilient against future per-failure-mode refinements.
//
// The harness reuses the synthetic-handler / KV-Put pattern from
// reload_test.go and the secondary-gateway spawn pattern from
// ratelimit_multi_replica_test.go. The cap is a per-process knob
// so the secondary on :8081 is required — the primary :8080 runs
// with the production-default 1_000_000.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryStoreSaturationGatewayURL targets the secondary gateway
// spawned with RATELIMIT_MEMORY_MAX_ENTRIES=10 +
// RATELIMIT_FAIL_POLICY=closed. Same :8081 port as other multi-
// replica scenarios; t.Cleanup tears down before the next test.
const memoryStoreSaturationGatewayURL = "http://localhost:8081"

// startMemorySaturationGateway spawns a fresh gateway on :8081
// with a tight memory cap and the closed FailPolicy. Mirrors
// startSecondaryGateway in shape; cleanup is registered before
// the readiness probe.
func startMemorySaturationGateway(t *testing.T, maxEntries int) func() {
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
		"RATELIMIT_FAIL_POLICY=closed",
		"RATELIMIT_KEY_TTL=10m",
		fmt.Sprintf("RATELIMIT_MEMORY_MAX_ENTRIES=%d", maxEntries),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	require.NoError(t, cmd.Start(), "failed to start memory-cap gateway binary")

	var once sync.Once
	shutdown := func() {
		once.Do(func() {
			cancel()
			_ = cmd.Wait()
		})
	}
	t.Cleanup(shutdown)

	waitForGatewayAt(t, memoryStoreSaturationGatewayURL, secondaryGatewayReadyTimeout)

	return shutdown
}

// TestE2E_MemoryStoreSaturationFailPolicy pins the closed-policy
// rejection contract on MemoryStore saturation.
//
// Setup:
//
//   - Secondary gateway on :8081 with RATELIMIT_MEMORY_MAX_ENTRIES=10
//     and RATELIMIT_FAIL_POLICY=closed.
//   - Synthetic route /rl/memory-cap registered via direct KV Put.
//     keyBy: ['header:x-api-key', 'ip']; rps:10, burst:10 so a
//     single request per key sits comfortably inside its own
//     bucket budget — any rejection observed is from the
//     cap-exhaustion guard, not from the bucket-empty branch.
//   - 15 sequential requests, each carrying a DISTINCT
//     X-API-Key header value.
//
// Expected behaviour: keys 1..10 admit cleanly (bucket created,
// fresh tokens). Keys 11..15 hit the cap; MemoryStore.Allow
// returns ErrMemoryStoreSaturated; FailPolicy=closed surfaces
// rejection (503 Service Unavailable + X-RateLimit-Limit per the
// current contract; tally accepts 429 too for forward-compat).
//
// Pinning intent: a regression that loosened the cap (e.g., via
// LoadOrStore unconditionally allocating before the size check)
// would let all 15 keys through and the test would fail loud. A
// regression that flipped fail-closed to fail-open under
// saturation would also surface here.
//
// The synthetic handler is wired AGAINST THE PRIMARY example-app
// connection pool via fx.nc; both gateways share the same NATS
// cluster, so a request that survives the secondary's memory cap
// resolves to the synthetic subscriber regardless of which
// gateway proxied it. This decouples the cap test from any state
// the primary's MemoryStore may carry.
func TestE2E_MemoryStoreSaturationFailPolicy(t *testing.T) {
	cleanup := startMemorySaturationGateway(t, 10)
	defer cleanup()

	fx := newNATSFixture(t)

	const (
		service = "e2e-mem-cap"
		pattern = "mem.cap.probe"
		path    = "/rl/memory-cap"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	t.Cleanup(func() { _ = fx.kv.Delete(fx.ctx, key) })

	// Synthetic handler — answers any request that reaches it
	// with a 200. Requests rejected by the cap never reach this
	// subscriber.
	sub, err := fx.nc.Subscribe(subject, func(msg *nats.Msg) {
		reply := gatewayReply{
			Status:  http.StatusOK,
			Headers: map[string][]string{},
			Body:    json.RawMessage(`{"ok":true}`),
		}
		replyBytes, marshalErr := json.Marshal(reply)
		if marshalErr != nil {
			return
		}
		_ = msg.Respond(replyBytes)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// Register the synthetic route. rps/burst are generous so a
	// single request per key sits comfortably inside its own
	// bucket — the only way to surface a rejection in this test
	// is via the saturation guard, not the GCRA gate.
	entry := kvEntryWithRateLimit{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		RateLimit: &kvRateLimitMeta{
			RPS:   10,
			Burst: 10,
			KeyBy: []string{"header:x-api-key", "ip"},
		},
	}
	entryBytes, err := json.Marshal(entry)
	require.NoError(t, err)

	_, err = fx.kv.Put(fx.ctx, key, entryBytes)
	require.NoError(t, err, "put memory-cap KV entry")

	// Wait for the secondary gateway to incorporate the route.
	// The same waitForRouteStatus shape used in reload_test.go,
	// but pointed at the secondary gateway URL.
	waitForRouteStatusAt(t, memoryStoreSaturationGatewayURL, path, http.StatusOK)

	// Issue 15 requests, each with a distinct X-API-Key. Track
	// per-status counts and the headers observed on the last
	// rejection so the assertion can pin both the count and the
	// header surface.
	const distinctKeys = 15
	statuses, lastRejected := burstWithDistinctAPIKeys(t,
		memoryStoreSaturationGatewayURL+path, distinctKeys)
	t.Logf("memory-store saturation: statuses=%v", statuses)

	// First 10 must admit. The exact count may vary by 1 if the
	// route's pre-warm probe in waitForRouteStatusAt admitted a
	// key before the load phase; tolerate that with a >=9 lower
	// bound on the OK count.
	assert.GreaterOrEqual(t, statuses[http.StatusOK], 9,
		"with cap=10 and %d distinct keys, at least 9 must admit", distinctKeys)

	// At least 4 must be rejected (5 saturation + tolerance for
	// pre-warm). The rejection status is whatever the
	// implementation surfaces for closed-FailPolicy store-error
	// rejection — today 503 Service Unavailable.
	rejected := statuses[http.StatusTooManyRequests] + statuses[http.StatusServiceUnavailable]
	assert.GreaterOrEqual(t, rejected, 4,
		"with cap=10 and %d distinct keys, at least 4 must be rejected; statuses=%v",
		distinctKeys, statuses)

	// The rejection MUST carry the static X-RateLimit-Limit
	// header. The bucket-state fields (Remaining/Reset) MAY be
	// suppressed depending on the rejection branch — both the
	// store-error and the saturation branch fall under the
	// FailPolicy.Apply path which drops the partial Decision.
	if lastRejected != nil {
		assert.NotEmpty(t, lastRejected.Get("X-RateLimit-Limit"),
			"rejection on memory saturation must carry static X-RateLimit-Limit")
	}
}

// burstWithDistinctAPIKeys fires `count` sequential requests
// against url, each with a unique X-API-Key header value. Returns
// the per-status counts and the last rejected response's headers
// (or nil if no request was rejected).
func burstWithDistinctAPIKeys(t *testing.T, url string, count int) (map[int]int, http.Header) {
	t.Helper()

	statuses := make(map[int]int, 4)
	var lastRejected http.Header

	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < count; i++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", fmt.Sprintf("tenant-%03d", i))

		resp, err := client.Do(req)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		statuses[resp.StatusCode]++
		if resp.StatusCode != http.StatusOK {
			lastRejected = resp.Header.Clone()
		}
	}

	return statuses, lastRejected
}

// waitForRouteStatusAt polls the supplied baseURL until GET path
// returns `want` or reloadWaitTimeout elapses. Mirrors
// waitForRouteStatus from reload_test.go but takes the base URL
// as a parameter so it can target the secondary gateway directly.
//
// The pre-warm probe carries an X-API-Key header so it lands in
// its own bucket on the secondary gateway and does not consume a
// slot from the load-phase keys below.
func waitForRouteStatusAt(t *testing.T, baseURL, path string, want int) {
	t.Helper()

	deadline := time.Now().Add(reloadWaitTimeout)
	client := &http.Client{Timeout: 2 * time.Second}
	url := baseURL + path

	var lastStatus int
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", "__warmup__")

		resp, err := client.Do(req)
		if err == nil {
			lastStatus = resp.StatusCode
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == want {
				return
			}
		}
		time.Sleep(reloadPollInterval)
	}

	t.Fatalf("gateway at %s never observed status %d on %s within %s (last=%d)",
		baseURL, want, path, reloadWaitTimeout, lastStatus)
}
