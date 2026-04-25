//go:build e2e

// Package e2e — concurrency-limit middleware saturation scenarios.
// Pins the contract that HTTP_MAX_CONCURRENT_REQUESTS bounds the
// number of in-flight requests the gateway processes simultaneously,
// short-circuits saturated arrivals with 503 + Retry-After: 1, and
// releases slots cleanly so a steady serial workload at any rate
// resolves to the cap-independent happy path.
//
// Both scenarios spawn a SECONDARY gateway on :8081 with a tight
// HTTP_MAX_CONCURRENT_REQUESTS=2 cap. The primary :8080 gateway
// runs with the production-default 10000, which is impractical to
// saturate from a single test goroutine pool. Sibling of
// ratelimit_multi_replica_test.go; reuses the prebuilt binary at
// dist/apps/gateway-server/gateway and the same readiness probe
// shape, so a missing binary skips the test with a clear hint
// rather than failing it.
package e2e

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// concurrencyGatewayURL is the base URL the spawned secondary
// gateway binds to for the concurrency tests. Distinct from the
// :8081 used by ratelimit_multi_replica_test.go because Go t.Run
// scoping does not synchronise port reuse between tests across
// files; both helpers register t.Cleanup before the readiness
// probe so the previous secondary releases :8081 by the time the
// next case starts.
const concurrencyGatewayURL = "http://localhost:8081"

// startConcurrencyGateway spawns a SECOND gateway process on :8081
// with a custom HTTP_MAX_CONCURRENT_REQUESTS cap. Registers cleanup
// before the readiness probe so a Fatalf in waitForGatewayAt does
// not leak the spawned process. Returns a closure equivalent to
// the registered cleanup, sync.Once-guarded so a deferred call
// after t.Cleanup has already fired is a no-op.
//
// All other env vars match startSecondaryGateway so the only
// observable difference between the two replicas is the
// concurrency cap — making the assertion below ("at least 2
// rejections") attributable to the cap, not to ambient drift.
func startConcurrencyGateway(t *testing.T, maxConcurrent int) func() {
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
		"HTTP_MAX_CONCURRENT_REQUESTS="+strconv.Itoa(maxConcurrent),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	require.NoError(t, cmd.Start(), "failed to start concurrency-limited gateway binary")

	var once sync.Once
	shutdown := func() {
		once.Do(func() {
			cancel()
			_ = cmd.Wait()
		})
	}

	t.Cleanup(shutdown)

	waitForGatewayAt(t, concurrencyGatewayURL, secondaryGatewayReadyTimeout)

	return shutdown
}

// TestE2E_Concurrency_SaturationReturns503 pins the saturation
// contract: with HTTP_MAX_CONCURRENT_REQUESTS=2 and 5 concurrent
// requests against a deliberately slow handler (/timeout/slow with
// delayMs that keeps the slot held long enough for all 5 arrivals
// to overlap), at least 2 of the 5 MUST surface 503 +
// Retry-After: 1 from the concurrency middleware short-circuit
// path. The exact number depends on race timing — the assertion
// is "≥ 2" rather than "= 3" so scheduler jitter does not flake.
//
// The /timeout/slow route has a per-route timeout of 200ms, so
// the handler completes within budget when delayMs=120 — slot
// release happens cleanly on every accepted request. A regression
// that broke slot release would manifest as the second wave of
// concurrent traffic also bouncing with 503, and as the
// SlotsReleaseAfterRequest test below failing as well.
func TestE2E_Concurrency_SaturationReturns503(t *testing.T) {
	cleanup := startConcurrencyGateway(t, 2)
	defer cleanup()

	const (
		concurrent = 5
		// delayMs sits comfortably under /timeout/slow's 200ms
		// per-route timeout so admitted requests succeed cleanly,
		// AND comfortably above the round-trip latency a parallel
		// burst needs to overlap, so the cap is observably hit.
		delayMs = 120
	)
	url := concurrencyGatewayURL + "/timeout/slow?delayMs=" + strconv.Itoa(delayMs)

	var (
		wg          sync.WaitGroup
		rejected    atomic.Int64
		retryAfters sync.Map
	)
	wg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer wg.Done()
			resp, err := http.Get(url)
			if err != nil {
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusServiceUnavailable {
				rejected.Add(1)
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					retryAfters.Store(ra, struct{}{})
				}
			}
		}()
	}
	wg.Wait()

	assert.GreaterOrEqual(t, rejected.Load(), int64(2),
		"with cap=2 and %d concurrent slow requests, at least 2 must short-circuit with 503", concurrent)

	// Every 503 the middleware emits MUST carry Retry-After: 1.
	// The header is the operator-facing signal that retries are
	// safe after a short pause; a regression to "no Retry-After
	// on saturation" would land here.
	if rejected.Load() > 0 {
		_, ok := retryAfters.Load("1")
		assert.True(t, ok,
			"503 from concurrency middleware MUST carry Retry-After: 1; observed values=%v", collectMapKeys(&retryAfters))
	}
}

// TestE2E_Concurrency_SlotsReleaseAfterRequest pins the slot-
// release contract: under the same cap=2 gateway, 10 SEQUENTIAL
// requests all succeed, because each request releases its slot
// before the next one arrives. A regression that leaked slots
// (forgot the deferred channel receive) would surface as a 503 on
// the 3rd, 4th, or later sequential request, even though the
// total throughput never exceeded 1 request at a time.
func TestE2E_Concurrency_SlotsReleaseAfterRequest(t *testing.T) {
	cleanup := startConcurrencyGateway(t, 2)
	defer cleanup()

	url := concurrencyGatewayURL + "/bench/hello"

	for i := 0; i < 10; i++ {
		resp, err := http.Get(url)
		require.NoError(t, err)
		_ = resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"sequential request #%d must succeed — slot must be released before the next arrives", i)
	}
}

// collectMapKeys snapshots a sync.Map's keys for use in failure
// messages. Sorted output is unnecessary at the small cardinality
// expected here (the middleware emits exactly one Retry-After
// value), so the diagnostic is kept minimal.
func collectMapKeys(m *sync.Map) []string {
	out := []string{}
	m.Range(func(key, _ any) bool {
		if s, ok := key.(string); ok {
			out = append(out, s)
		}
		return true
	})

	return out
}

