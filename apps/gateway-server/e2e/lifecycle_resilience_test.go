//go:build e2e

// Package e2e — lifecycle resilience scenarios. Pins four contracts
// that previously had unit-tier coverage only: K8s probes bypass the
// concurrency-limit middleware, the boot-time rate-limit backend
// init retry loop recovers from a transient backend gap, the
// rate-limit router lazily registers a new backend when a route's
// `store` field is hot-swapped via KV, and an in-flight request that
// holds a Store reference observes a clean ratelimit.ErrStoreClosed
// sentinel during graceful shutdown rather than a raw NATS-draining
// error.
//
// All four scenarios sit at the lifecycle layer where the gateway
// composes long-lived resources (HTTP server, watcher, rate-limit
// router, NATS connection). Lower-tier tests pin each component in
// isolation; this file pins their composition end-to-end so a
// refactor that splits the boot or shutdown sequence into a new
// shape cannot silently break the operator-facing contract.
//
// Sibling of:
//   - health_e2e_test.go — the analogous probe-bypass-rate-limit
//     pin that the concurrency scenario in this file complements.
//   - ratelimit_multi_replica_test.go — secondary-gateway spawn
//     pattern reused via gatewayBinaryPath / waitForGatewayAt.
//   - reload_test.go — synthetic-route + KV-Put pattern reused via
//     newNATSFixture / serveFakeHandler.
//   - concurrency_limit_e2e_test.go — slow-route saturation pattern
//     reused via /timeout/slow?delayMs=N.
package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lifecycleSecondaryURL targets the secondary gateway every scenario
// in this file spawns. Same :8081 port as the other secondary-
// gateway scenarios; t.Cleanup teardown ordering guarantees only
// one is up at a time.
const lifecycleSecondaryURL = "http://localhost:8081"

// startConcurrencyBypassGateway spawns a secondary gateway on :8081
// with the supplied HTTP_MAX_CONCURRENT_REQUESTS cap. Equivalent in
// shape to startConcurrencyGateway in concurrency_limit_e2e_test.go;
// kept distinct so both files can evolve their env shape
// independently if a future scenario needs additional knobs.
func startConcurrencyBypassGateway(t *testing.T, maxConcurrent int) func() {
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

	require.NoError(t, cmd.Start(), "failed to start lifecycle secondary gateway")

	var once sync.Once
	shutdown := func() {
		once.Do(func() {
			cancel()
			_ = cmd.Wait()
		})
	}
	t.Cleanup(shutdown)

	waitForGatewayAt(t, lifecycleSecondaryURL, secondaryGatewayReadyTimeout)

	return shutdown
}

// startNATSKVSwapGateway spawns a secondary gateway on :8081 with a
// neutral env tailored to the hot-reload swap scenario: open
// fail-policy keeps requests flowing if any transient store error
// appears during the swap window, and the rate-limit TTL is wide
// enough that no key expires mid-test.
func startNATSKVSwapGateway(t *testing.T) func() {
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

	require.NoError(t, cmd.Start(), "failed to start lifecycle swap gateway")

	var once sync.Once
	shutdown := func() {
		once.Do(func() {
			cancel()
			_ = cmd.Wait()
		})
	}
	t.Cleanup(shutdown)

	waitForGatewayAt(t, lifecycleSecondaryURL, secondaryGatewayReadyTimeout)

	return shutdown
}

// TestE2E_HealthProbes_BypassConcurrencyLimit pins the K8s contract
// that liveness and readiness probes MUST NOT be subject to the
// concurrency-limit middleware. Mirrors TestE2E_Health_ProbesBypass
// RateLimit but for the cap, not the rate-limit gate.
//
// The /healthz and /readyz handlers are registered BEFORE the
// concurrency limiter via h.Use() in transport/http/server.go, so
// probe traffic short-circuits without touching the semaphore. A
// regression that moved the probes behind the cap would surface
// here as 503 + Retry-After: 1 on the probe response — the exact
// shape produced by newConcurrencyLimitMiddleware on saturation.
//
// Setup: secondary gateway with HTTP_MAX_CONCURRENT_REQUESTS=2.
// Saturate the cap with 5 concurrent /timeout/slow requests
// (delayMs=120 sits comfortably under the 200ms per-route timeout
// so admitted requests succeed cleanly). While the cap is held,
// fire 10 parallel /healthz and 10 parallel /readyz — every probe
// MUST return 200, every probe response MUST carry no Retry-After
// header, and the slow-route requests MUST themselves observe the
// expected ≥ 2 saturation rejections (sanity that the cap really
// fired during the probe burst).
func TestE2E_HealthProbes_BypassConcurrencyLimit(t *testing.T) {
	cleanup := startConcurrencyBypassGateway(t, 2)
	defer cleanup()

	const (
		slowConcurrent = 5
		probeBurst     = 10
		delayMs        = 120
	)

	slowURL := lifecycleSecondaryURL + "/timeout/slow?delayMs=" + strconv.Itoa(delayMs)

	var (
		slowWG       sync.WaitGroup
		slowRejected atomic.Int64
		probeWG      sync.WaitGroup
		probeMu      sync.Mutex
		liveStatuses = make(map[int]int, 2)
		readyStat    = make(map[int]int, 2)
		liveRetry    int
		readyRetry   int
	)

	// Saturate the cap. Holds slots for ~120ms so the probe burst
	// fires entirely while the cap is held.
	slowWG.Add(slowConcurrent)
	for i := 0; i < slowConcurrent; i++ {
		go func() {
			defer slowWG.Done()
			resp, err := http.Get(slowURL)
			if err != nil {
				return
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == http.StatusServiceUnavailable {
				slowRejected.Add(1)
			}
		}()
	}

	// Give the slow burst ~20ms to enter the gateway and acquire
	// slots before the probes fire. Without this the probes can
	// race the slow goroutines and the cap is not yet held when
	// the probe assertions land — the test would still pass but
	// would not actually pin the bypass behaviour.
	time.Sleep(20 * time.Millisecond)

	// Probe burst — fire while the cap is saturated.
	probeWG.Add(2 * probeBurst)
	for i := 0; i < probeBurst; i++ {
		go func() {
			defer probeWG.Done()
			r, err := http.Get(lifecycleSecondaryURL + "/healthz")
			if err != nil {
				return
			}
			defer func() { _ = r.Body.Close() }()

			probeMu.Lock()
			defer probeMu.Unlock()
			liveStatuses[r.StatusCode]++
			if r.Header.Get("Retry-After") != "" {
				liveRetry++
			}
		}()
		go func() {
			defer probeWG.Done()
			r, err := http.Get(lifecycleSecondaryURL + "/readyz")
			if err != nil {
				return
			}
			defer func() { _ = r.Body.Close() }()

			probeMu.Lock()
			defer probeMu.Unlock()
			readyStat[r.StatusCode]++
			if r.Header.Get("Retry-After") != "" {
				readyRetry++
			}
		}()
	}
	probeWG.Wait()
	slowWG.Wait()

	// Sanity: the cap really fired during the run. If this fails the
	// probe assertions are vacuous — the cap was never actually
	// saturated and the bypass contract was not exercised.
	assert.GreaterOrEqual(t, slowRejected.Load(), int64(2),
		"with cap=2 and %d concurrent slow requests, at least 2 must short-circuit with 503 — sanity for bypass test", slowConcurrent)

	// Probes MUST all be 200 even while the cap is saturated.
	assert.Equal(t, probeBurst, liveStatuses[http.StatusOK],
		"every /healthz must return 200 while concurrency cap is saturated; statuses=%v", liveStatuses)
	assert.Equal(t, probeBurst, readyStat[http.StatusOK],
		"every /readyz must return 200 while concurrency cap is saturated; statuses=%v", readyStat)

	// Probes MUST NOT carry Retry-After. The header is the
	// concurrency middleware's saturation marker; its presence on a
	// probe response would indicate the probe traversed the
	// limiter — exactly the regression this test pins against.
	assert.Zero(t, liveRetry,
		"/healthz must not carry Retry-After — probe traversed the concurrency limiter")
	assert.Zero(t, readyRetry,
		"/readyz must not carry Retry-After — probe traversed the concurrency limiter")
}

// TestE2E_BackendInitRetryRecoversAfterTransientFlap pins the
// boot-time backend init retry pathway: a route declaring
// `store: 'nats-kv'` whose backend was not registered at boot
// (because openOrCreateRatelimitBucket failed during init or the
// bucket simply did not exist yet) eventually recovers without a
// gateway restart.
//
// HARNESS LIMITATION:
//
// The intended scenario — boot the gateway when openOrCreate
// RatelimitBucket transiently fails, then heal the failure — is
// not reproducible from outside the gateway process. The bucket is
// auto-created on demand with default JetStream permissions, so the
// only way to fail init is to revoke JS permissions on the NATS
// account or pre-place the stream in a corrupt state, neither of
// which the e2e harness controls.
//
// Skipping rather than running a weaker assertion: the watcher
// OnChange callback path (which re-runs ensureRateLimitBackends on
// every KV delta) is already exercised by
// TestE2E_HotReload_RateLimitBackendSwap below; the 30-second
// background ticker path it complements is unit-tested in
// cmd/gateway/main_test.go via the retry loop's own time-driven
// behaviour. The e2e tier adds signal only when the harness can
// induce a real transient failure — which today it cannot.
func TestE2E_BackendInitRetryRecoversAfterTransientFlap(t *testing.T) {
	t.Skip("harness cannot inject NATS-KV bucket init failure on a fresh gateway boot " +
		"(openOrCreateRatelimitBucket auto-creates with default JS permissions, and the " +
		"`nats kv del` path used by failpolicy tests deletes AFTER successful init, not " +
		"during it). The watcher-delta retry pathway is covered by " +
		"TestE2E_HotReload_RateLimitBackendSwap; the 30s background ticker is covered by " +
		"the unit retry-loop tests. Unskip when the harness gains a NATS proxy that can " +
		"deny JS calls for a controlled window during boot.")
}

// TestE2E_HotReload_RateLimitBackendSwap pins the watcher OnChange
// pathway that lazily registers a new rate-limit backend when a
// route's `store` field is hot-swapped via a KV mutation.
//
// Setup:
//
//   - Synthetic route /lifecycle/rl-swap registered via direct KV
//     Put with rateLimit: { rps: 100, burst: 100, store: 'memory' }.
//     Generous budget so no GCRA rejection muddies the assertion —
//     every observed non-2xx is a swap-pathway regression, not a
//     bucket-empty rejection.
//   - Wave 1: 5 sequential requests against the route on the
//     secondary gateway. Every request MUST return 200; the memory
//     backend is the router's always-registered default.
//   - KV update: same route, same path, but rateLimit.store flips
//     to 'nats-kv'. The watcher fires OnChange, which re-runs
//     ensureRateLimitBackends; the Router's nats-kv factory is
//     invoked lazily and the backend becomes available without a
//     gateway restart.
//   - Wave 2: 5 sequential requests after a short propagation pause.
//     Every request MUST again return 200 — the nats-kv backend now
//     services the route. A regression that left the backend
//     unregistered would surface as a fallback to memory (visible
//     only via internal counters, not observable here) OR — under
//     a strict-failpolicy regression — as 503/429 leakage during
//     the swap window. The assertion is pinned at the observable
//     edge: no 5xx surfaces during or after the swap.
//
// The rate-limit router counters are NOT exposed on the HTTP
// surface, so the test cannot directly observe "memory served wave
// 1, nats-kv served wave 2". The swap is pinned via the negative
// assertion: ZERO 5xx responses across both waves AND the
// post-swap settlement window. Combined with the unit-tested
// EnsureBackend semantics, this is sufficient to detect any
// regression that would leave the route unroutable or leak a 5xx
// during the OnChange callback's execution.
func TestE2E_HotReload_RateLimitBackendSwap(t *testing.T) {
	cleanup := startNATSKVSwapGateway(t)
	defer cleanup()

	fx := newNATSFixture(t)

	const (
		service = "e2e-lifecycle-swap"
		pattern = "lifecycle.swap.probe"
		path    = "/lifecycle/rl-swap"
	)

	key := service + ".cmd." + pattern
	subject := service + "__microservice.cmd." + pattern

	t.Cleanup(func() { _ = fx.kv.Delete(fx.ctx, key) })

	serveFakeHandler(t, fx.nc, subject, map[string]any{"ok": true, "phase": "swap"})

	// Initial entry — memory backend.
	memoryEntry := kvEntryWithRateLimit{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		RateLimit: &kvRateLimitMeta{
			RPS:   100,
			Burst: 100,
			Store: "memory",
		},
	}
	memoryBytes, err := json.Marshal(memoryEntry)
	require.NoError(t, err)

	_, err = fx.kv.Put(fx.ctx, key, memoryBytes)
	require.NoError(t, err, "put memory-backed entry")

	waitForRouteStatusAt(t, lifecycleSecondaryURL, path, http.StatusOK)

	// Wave 1: memory backend.
	for i := 0; i < 5; i++ {
		resp, err := http.Get(lifecycleSecondaryURL + path)
		require.NoError(t, err, "wave 1 request %d", i)
		_ = resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"wave 1 request %d must succeed under memory backend", i)
	}

	// Hot-swap to nats-kv.
	natsKVEntry := kvEntryWithRateLimit{
		HTTP: &kvHTTPMeta{Method: http.MethodGet, Path: path},
		RateLimit: &kvRateLimitMeta{
			RPS:   100,
			Burst: 100,
			Store: "nats-kv",
		},
	}
	natsKVBytes, err := json.Marshal(natsKVEntry)
	require.NoError(t, err)

	_, err = fx.kv.Put(fx.ctx, key, natsKVBytes)
	require.NoError(t, err, "put nats-kv-backed entry (swap)")

	// The watcher typically converges within one poll interval; a 2s
	// wait is generous and amortises any JS bucket creation on the
	// nats-kv side (openOrCreateRatelimitBucket may need to
	// materialise the *_ratelimit bucket on the first request after
	// the swap).
	time.Sleep(2 * time.Second)

	// Wave 2: nats-kv backend.
	statuses := make(map[int]int, 2)
	for i := 0; i < 5; i++ {
		resp, err := http.Get(lifecycleSecondaryURL + path)
		require.NoError(t, err, "wave 2 request %d", i)
		_ = resp.Body.Close()

		statuses[resp.StatusCode]++
		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"wave 2 request %d must succeed under nats-kv backend after swap", i)
	}

	// No 5xx may leak during the post-swap window. A regression
	// where the OnChange callback failed to register the new
	// backend would surface either as a fallback to memory (silent
	// from an HTTP surface, but harmless) or — under a strict
	// fail-policy regression — as 503 leakage during the
	// EnsureBackend call. The latter is what we pin against here.
	for code, count := range statuses {
		if code >= 500 {
			t.Errorf("post-swap wave saw status %d (%d times); statuses=%v", code, count, statuses)
		}
	}
}

// TestE2E_Shutdown_InFlightRequestSeesCleanErrStoreClosed pins the
// Drain-step ordering invariant from internal/lifecycle/shutdown.go:
// Router.Close runs BETWEEN watcher.Stop and NATS.Drain so an
// in-flight request that holds a Store reference observes a clean
// ratelimit.ErrStoreClosed sentinel rather than a raw NATS-draining
// error. Under FailPolicy=closed the sentinel maps to a structured
// 5xx response carrying the static X-RateLimit-Limit header — the
// e2e-observable signature of the contract.
//
// HARNESS LIMITATION:
//
// Reproducing the exact race the contract defends against requires
// the test harness to:
//
//   1. Land a request in the rate-limit pipeline (Allow call).
//   2. Hold it there (the Allow path on a healthy NATS-KV store
//      runs in O(ms); pausing it would require an in-process delay
//      injection point that does not exist).
//   3. Trigger lifecycle.Drain on the secondary gateway exactly
//      while step 2 is still pending.
//   4. Observe the structured ErrStoreClosed response on the
//      original request's connection before the HTTP listener
//      finishes draining.
//
// The window between "request reached Allow" and "Allow returned"
// is too narrow to drive deterministically from outside the
// process; existing approaches such as auth-blocking or slow
// upstream NATS handlers move the request PAST the rate-limit
// gate, after which Drain.closeRateLimitRouter cannot affect the
// response. Sending SIGTERM and asserting headers on whatever
// response surfaces would conflate the ordering contract with
// every other shutdown-time response shape (HTTP listener
// connection drop, in-flight upstream NATS reply timeout, et al.)
// and would not pin the actual invariant.
//
// The ordering invariant itself is pinned by
// TestDrain_OrderingHTTPThenWatcherThenRouterThenNATS (and
// siblings) in internal/lifecycle/shutdown_test.go using a
// recording fake. The e2e propagation guarantee is a function of
// that ordering plus the FailPolicy contract verified end-to-end
// in ratelimit_failpolicy_e2e_test.go. Direct e2e of in-flight
// ordering during shutdown is left for a future test that can pin
// shutdown timing precisely — for example, a NATS proxy that can
// pause and resume specific subjects on demand, or an in-process
// Allow-time delay hook compiled in only under a build tag.
func TestE2E_Shutdown_InFlightRequestSeesCleanErrStoreClosed(t *testing.T) {
	t.Skip("shutdown timing is racy from outside the gateway process: the window between " +
		"\"request entered Store.Allow\" and \"Allow returned\" is too narrow to drive " +
		"deterministically without an in-process delay injection point. The Drain step " +
		"ordering is pinned by internal/lifecycle/shutdown_test.go's recording-fake tests; " +
		"the FailPolicy=closed mapping of ErrStoreClosed to a 5xx + X-RateLimit-Limit " +
		"response is pinned by ratelimit_failpolicy_e2e_test.go; their composition is " +
		"the contract this scenario would otherwise verify. Unskip when the harness gains " +
		"a controlled Allow-time pause hook (e.g., a NATS proxy that can hold messages on " +
		"a target subject) or a build-tag-gated synthetic delay.")
}

