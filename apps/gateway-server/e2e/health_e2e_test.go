//go:build e2e

// Package e2e — Kubernetes liveness/readiness probe scenarios. Pins
// the K8s contract for `/healthz` and `/readyz` end-to-end against
// the live gateway: liveness is unconditional, readiness flips to
// 200 once NATS + the initial registry snapshot have landed, and
// both probes bypass every middleware (concurrency cap, trusted
// proxy chain, rate-limit gate) so a saturated pod can still report
// its true health status to the orchestrator.
//
// Sibling of e2e_test.go and contract_test.go; reuses the package-
// level `gatewayURL` constant and `waitForGateway` helper. The
// `/__probe__` path the readiness helper hits intentionally
// short-circuits to 404 via the catch-all — it does NOT pre-warm
// /readyz, so the assertions below verify the dedicated endpoint.
package e2e

import (
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// healthReadyDeadline is the maximum time the readiness probe is
// allowed to take to report ready after the harness has otherwise
// observed the gateway's HTTP listener accept connections. Five
// seconds is a wide margin — under normal local-stack startup the
// initial registry snapshot lands well within a single 100ms watcher
// tick, so a 5s budget gives jitter headroom without masking a real
// regression in the bootstrap pipeline.
const healthReadyDeadline = 5 * time.Second

// TestE2E_Health_LivezAlwaysReturns200 pins the unconditional
// liveness contract: as long as the process is alive and the
// goroutine scheduler dispatches the request, /healthz returns
// 200 OK with no body. The probe is also exercised under a small
// load burst so a regression that accidentally chained the probe
// behind the concurrency cap, the trusted-proxy chain, or any
// other request-side middleware would surface as a 503/429 here.
func TestE2E_Health_LivezAlwaysReturns200(t *testing.T) {
	waitForGateway(t)

	resp, err := http.Get(gatewayURL + "/healthz")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"liveness probe must always succeed on a running process")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Empty(t, body, "liveness body must be empty per K8s contract")

	// Burst phase — fire 50 parallel requests. The probe MUST stay
	// at 200 throughout. A regression that placed /healthz behind
	// the concurrency limiter or the rate-limit gate would surface
	// as a 503/429 burst here.
	const burst = 50
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		statuses = make(map[int]int, 2)
	)
	wg.Add(burst)
	for i := 0; i < burst; i++ {
		go func() {
			defer wg.Done()
			r, err := http.Get(gatewayURL + "/healthz")
			if err != nil {
				return
			}
			_ = r.Body.Close()

			mu.Lock()
			defer mu.Unlock()
			statuses[r.StatusCode]++
		}()
	}
	wg.Wait()

	assert.Equal(t, burst, statuses[http.StatusOK],
		"every parallel /healthz must return 200; statuses=%v", statuses)
}

// TestE2E_Health_ReadyzReturnsTrueAfterSnapshotLands pins the
// readiness contract: once the gateway accepts HTTP connections AND
// the initial registry snapshot has been processed, /readyz returns
// 200. Under the three-process harness (NATS + example-app +
// gateway-server up) the snapshot lands within a single watcher
// tick, so the probe converges to 200 well inside healthReadyDeadline.
//
// A nil ReadinessSignal would degrade to "always ready" (see
// readyHandler in transport/http/server.go); the live gateway
// wires a real atomic-bool signal that bootstrap flips after the
// first snapshot, so a regression to "snapshot never lands" would
// surface here as a deadline timeout.
func TestE2E_Health_ReadyzReturnsTrueAfterSnapshotLands(t *testing.T) {
	waitForGateway(t)

	deadline := time.Now().Add(healthReadyDeadline)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(gatewayURL + "/readyz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("/readyz did not report 200 within %s — initial registry snapshot likely never landed", healthReadyDeadline)
}

// TestE2E_Health_ProbesBypassRateLimit pins the K8s contract that
// liveness and readiness probes MUST NOT be subject to the
// rate-limit gate. The /rate-limit/basic route has rps=1, burst=1
// so a back-to-back hit exhausts its bucket; the subsequent
// /healthz and /readyz requests run in parallel against the same
// gateway and the same loopback peer IP, but their handlers are
// registered BEFORE the catch-all in transport/http/server.go and
// therefore short-circuit upstream of the proxy handler that owns
// the rate-limit gate.
//
// A regression that chained the probes through the proxy handler
// would land them in the same per-IP bucket as the contract route
// and one of them would observe 429.
func TestE2E_Health_ProbesBypassRateLimit(t *testing.T) {
	waitForGateway(t)

	// Drain any lingering bucket so the first hit on the
	// rate-limited route below is guaranteed to land on a fresh
	// token.
	time.Sleep(1100 * time.Millisecond)

	first, err := http.Get(gatewayURL + "/rate-limit/basic")
	require.NoError(t, err)
	_ = first.Body.Close()
	require.Equal(t, http.StatusOK, first.StatusCode,
		"first request must succeed within burst")

	second, err := http.Get(gatewayURL + "/rate-limit/basic")
	require.NoError(t, err)
	_ = second.Body.Close()
	require.Equal(t, http.StatusTooManyRequests, second.StatusCode,
		"bucket must be exhausted before probe assertions")

	// Hit /healthz and /readyz in parallel — both must return 200
	// even though the loopback peer's per-IP bucket on the
	// contract route is exhausted.
	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		liveStatus  int
		readyStatus int
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		r, err := http.Get(gatewayURL + "/healthz")
		require.NoError(t, err)
		_ = r.Body.Close()
		mu.Lock()
		defer mu.Unlock()
		liveStatus = r.StatusCode
	}()
	go func() {
		defer wg.Done()
		r, err := http.Get(gatewayURL + "/readyz")
		require.NoError(t, err)
		_ = r.Body.Close()
		mu.Lock()
		defer mu.Unlock()
		readyStatus = r.StatusCode
	}()
	wg.Wait()

	assert.Equal(t, http.StatusOK, liveStatus,
		"/healthz must bypass per-IP rate-limit even when peer's contract-route bucket is exhausted")
	assert.Equal(t, http.StatusOK, readyStatus,
		"/readyz must bypass per-IP rate-limit even when peer's contract-route bucket is exhausted")
}
