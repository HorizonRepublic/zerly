package ratelimit

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/sony/gobreaker"
)

const (
	// tatVersion1 is the version byte prefix in the current TAT
	// encoding. Future versions MUST bump this constant and the
	// decoder MUST dispatch on the byte. Callers MUST NOT peek past
	// the version byte without checking it first.
	tatVersion1 byte = 0x01

	// tatEncodedLength is the fixed wire length of a version-1
	// encoded TAT value: 1 version byte + 8 bytes of
	// big-endian int64 nanoseconds.
	tatEncodedLength = 9
)

// encodeTAT serializes a TAT timestamp for storage in a NATS KV
// bucket as [version(1 byte)][UnixNano int64 big-endian(8 bytes)].
//
// The version byte reserves forward-compatibility room for future
// state expansion (e.g., adding createdAt or a config hash without
// breaking existing buckets). Callers MUST use decodeTAT to read
// values back — never reach past the version byte directly.
//
// Requires a real, post-1970 timestamp. Calling with time.Time{}
// produces an undefined round-trip (time.Unix cannot reconstruct
// year-0001 from its UnixNano representation); callers MUST NOT do so.
func encodeTAT(tat time.Time) []byte {
	out := make([]byte, tatEncodedLength)
	out[0] = tatVersion1
	binary.BigEndian.PutUint64(out[1:], uint64(tat.UnixNano()))
	return out
}

// decodeTAT parses a TAT byte sequence produced by encodeTAT. Returns
// an error for wrong length or unknown version byte — callers MUST
// treat a decode error as "bucket data is corrupt" and fall back to
// the fresh-bucket path (currentTAT = zero time).
func decodeTAT(b []byte) (time.Time, error) {
	if len(b) != tatEncodedLength {
		return time.Time{}, fmt.Errorf("ratelimit: TAT length got %d want %d", len(b), tatEncodedLength)
	}
	if b[0] != tatVersion1 {
		return time.Time{}, fmt.Errorf("ratelimit: TAT unknown version 0x%02x", b[0])
	}
	ns := int64(binary.BigEndian.Uint64(b[1:]))

	return time.Unix(0, ns), nil
}

// kvEntry abstracts the subset of a NATS KeyValue entry the store
// needs: value bytes and the revision used for optimistic CAS.
type kvEntry interface {
	Value() []byte
	Revision() uint64
}

// kvAPI is the minimal NATS KeyValue surface consumed by
// NATSKVStore. Abstracted so tests can swap in a deterministic
// in-memory fake without a JetStream dependency.
//
// Create MUST return errCASConflict when the key already exists.
// Update MUST return errCASConflict when revision does not match
// the stored revision. Get MUST return errKVKeyNotFound when the
// key is absent.
type kvAPI interface {
	Get(ctx context.Context, key string) (kvEntry, error)
	Create(ctx context.Context, key string, value []byte) (uint64, error)
	Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error)
}

// Package-internal sentinel errors the kvAPI implementation signals
// to NATSKVStore. The store translates them to decisions (fresh
// bucket, CAS retry) without surfacing backend-specific shapes.
var (
	errKVKeyNotFound = errors.New("ratelimit: kv key not found")
	errCASConflict   = errors.New("ratelimit: kv cas conflict")
)

// ErrCircuitOpen is returned by NATSKVStore.Allow when the backend
// circuit breaker is in open or half-open-rejecting state. The
// gateway MUST consult FailPolicy to decide whether this surfaces
// as HTTP 503 or an allow pass-through.
var ErrCircuitOpen = errors.New("ratelimit: circuit open")

// ErrCASBudgetExhausted is returned when the CAS retry loop runs
// past its wall-clock budget without winning a write. Indicates
// sustained contention on a single key; callers SHOULD treat it
// the same as a transient backend outage for fail-policy purposes.
var ErrCASBudgetExhausted = errors.New("ratelimit: cas budget exhausted")

// ErrCASMaxAttempts is returned when the CAS retry loop hits its
// hard attempt cap. Defensive bound; reaching it implies a broken
// KV (every attempt races), not ordinary contention.
var ErrCASMaxAttempts = errors.New("ratelimit: cas max attempts")

const (
	// casBudget caps the wall-clock time a single Allow call may
	// spend retrying on CAS conflicts. Chosen so that even under
	// heavy contention on one key the gateway's per-request latency
	// stays bounded.
	casBudget = 10 * time.Millisecond

	// maxCASAttempts is a hard defensive bound on retry count. The
	// wall-clock budget is the primary stop signal; this guard
	// exists to catch pathological loops (e.g., a KV that always
	// returns conflict) before they burn the whole budget.
	maxCASAttempts = 64

	// breakerFailures is the number of consecutive failures that
	// trip the circuit breaker from closed to open.
	breakerFailures uint32 = 10

	// breakerTimeout is the cool-down after which an open breaker
	// transitions to half-open and probes the backend again.
	breakerTimeout = 5 * time.Second
)

// Breaker state values exposed via the circuit_state gauge counter.
const (
	breakerStateClosed   int64 = 0
	breakerStateHalfOpen int64 = 1
	breakerStateOpen     int64 = 2
)

// NATSKVStore is a GCRA rate limiter whose TAT lives in a NATS
// JetStream KV bucket. Semantically identical to MemoryStore: the
// same (key, rps, burst) inputs produce the same decision, so a
// deployment can start on MemoryStore and migrate to NATSKVStore
// without behavioral drift.
//
// Cross-replica correctness is enforced by optimistic CAS on the
// KV revision: each Allow reads the current TAT + revision,
// computes the new TAT via Check, and writes back with the revision
// as a precondition. Lost CAS means another replica advanced the
// TAT for the same key; the loop retries with a jittered backoff
// until the budget is exhausted.
//
// A circuit breaker guards the KV backend. On sustained failure,
// Allow short-circuits with ErrCircuitOpen instead of hammering a
// dead JetStream cluster; the gateway's FailPolicy decides whether
// that maps to HTTP 503 or allow-on-failure.
type NATSKVStore struct {
	kv      kvAPI
	breaker *gobreaker.CircuitBreaker
	logger  zerolog.Logger
	budget  time.Duration

	counters struct {
		allowed             atomic.Int64
		rejected            atomic.Int64
		casRetries          atomic.Int64
		budgetExhausted     atomic.Int64
		circuitState        atomic.Int64
		breakerTransitions  atomic.Int64
		circuitRejected     atomic.Int64
	}
}

// natskvOption customizes NATSKVStore construction. Options compose:
// pass any subset to newNATSKVStoreFromKV (or the public constructor).
type natskvOption func(*natskvOptions)

type natskvOptions struct {
	budget          time.Duration
	breakerFailures uint32
	breakerTimeout  time.Duration
	logger          zerolog.Logger
}

// withCASBudget overrides the default CAS wall-clock budget. Tests
// use a tight budget to exercise the exhaustion path; production
// defaults to casBudget.
func withCASBudget(d time.Duration) natskvOption {
	return func(o *natskvOptions) { o.budget = d }
}

// withBreakerFailures overrides the consecutive-failure threshold
// that trips the circuit breaker from closed to open.
func withBreakerFailures(n uint32) natskvOption {
	return func(o *natskvOptions) { o.breakerFailures = n }
}

// withLogger plumbs a zerolog.Logger into breaker state-change logs.
func withLogger(l zerolog.Logger) natskvOption {
	return func(o *natskvOptions) { o.logger = l }
}

// newNATSKVStoreFromKV constructs a store against any kvAPI
// implementation. Production code wires a real JetStream KeyValue
// adapter; tests wire an in-memory fake. The separation lets the
// CAS + breaker logic be covered without a live NATS.
func newNATSKVStoreFromKV(kv kvAPI, opts ...natskvOption) *NATSKVStore {
	cfg := natskvOptions{
		budget:          casBudget,
		breakerFailures: breakerFailures,
		breakerTimeout:  breakerTimeout,
		logger:          zerolog.Nop(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	s := &NATSKVStore{
		kv:     kv,
		logger: cfg.logger,
		budget: cfg.budget,
	}
	s.breaker = newBreaker(cfg.breakerFailures, cfg.breakerTimeout, cfg.logger, &s.counters.circuitState, &s.counters.breakerTransitions)
	return s
}

// newBreaker builds a gobreaker.CircuitBreaker with a
// consecutive-failure trip rule and a hook that mirrors the
// breaker's state into the provided atomic gauge for metric export.
func newBreaker(failures uint32, timeout time.Duration, logger zerolog.Logger, stateGauge, transitions *atomic.Int64) *gobreaker.CircuitBreaker {
	settings := gobreaker.Settings{
		Name:    "ratelimit-natskv",
		Timeout: timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= failures
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			transitions.Add(1)
			stateGauge.Store(stateToInt(to))
			logger.Warn().
				Str("event", "ratelimit.circuit.statechange").
				Str("name", name).
				Str("from", from.String()).
				Str("to", to.String()).
				Msg("circuit breaker state change")
		},
	}
	return gobreaker.NewCircuitBreaker(settings)
}

func stateToInt(s gobreaker.State) int64 {
	switch s {
	case gobreaker.StateClosed:
		return breakerStateClosed
	case gobreaker.StateHalfOpen:
		return breakerStateHalfOpen
	case gobreaker.StateOpen:
		return breakerStateOpen
	default:
		return breakerStateClosed
	}
}

// Allow implements Store by running GCRA against a TAT stored in the
// KV bucket. The backend call is wrapped by the circuit breaker: an
// open breaker short-circuits with ErrCircuitOpen before any network
// I/O. Errors from allowInternal (budget exhausted, max attempts,
// propagated KV errors) propagate to the caller and are counted as
// breaker failures.
func (s *NATSKVStore) Allow(ctx context.Context, key string, rps, burst int) (Decision, error) {
	result, err := s.breaker.Execute(func() (any, error) {
		return s.allowInternal(ctx, key, rps, burst)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			s.counters.circuitRejected.Add(1)
			return Decision{}, ErrCircuitOpen
		}
		return Decision{}, err
	}
	d, ok := result.(Decision)
	if !ok {
		return Decision{}, fmt.Errorf("ratelimit: unexpected breaker result type %T", result)
	}
	if d.Allowed {
		s.counters.allowed.Add(1)
	} else {
		s.counters.rejected.Add(1)
	}
	return d, nil
}

// allowInternal is the CAS retry loop invoked through the breaker.
// Each attempt: read the current TAT + revision, run Check, and on
// allow either Create (fresh key) or Update (existing revision). A
// lost CAS increments casRetries and retries with backoff+jitter
// until the wall-clock budget or attempt cap is hit.
func (s *NATSKVStore) allowInternal(ctx context.Context, key string, rps, burst int) (Decision, error) {
	deadline := time.Now().Add(s.budget)
	for attempt := 0; attempt < maxCASAttempts; attempt++ {
		if time.Now().After(deadline) {
			s.counters.budgetExhausted.Add(1)
			return Decision{}, ErrCASBudgetExhausted
		}
		if attempt > 0 {
			if !sleepCtx(ctx, nextBackoff(attempt)) {
				return Decision{}, ctx.Err()
			}
		}

		entry, err := s.kv.Get(ctx, key)
		var currentTAT time.Time
		var rev uint64
		switch {
		case err == nil:
			currentTAT, _ = decodeTAT(entry.Value())
			rev = entry.Revision()
		case errors.Is(err, errKVKeyNotFound):
			// Fresh bucket; currentTAT stays zero, rev stays 0.
		default:
			return Decision{}, fmt.Errorf("nats-kv get: %w", err)
		}

		decision, newTAT := Check(currentTAT, time.Now(), rps, burst)
		if !decision.Allowed {
			return decision, nil
		}

		encoded := encodeTAT(newTAT)
		if rev == 0 {
			_, err = s.kv.Create(ctx, key, encoded)
		} else {
			_, err = s.kv.Update(ctx, key, encoded, rev)
		}
		if err == nil {
			return decision, nil
		}
		if errors.Is(err, errCASConflict) {
			s.counters.casRetries.Add(1)
			continue
		}
		return Decision{}, fmt.Errorf("nats-kv write: %w", err)
	}
	return Decision{}, ErrCASMaxAttempts
}

// FlushPrefix is a no-op at this layer; the production constructor
// wires a bucket-scoped flush path.
func (s *NATSKVStore) FlushPrefix(_ context.Context, _ string) error {
	return nil
}

// Close releases resources. Idempotent.
func (s *NATSKVStore) Close() error { return nil }

// nextBackoff returns a jittered backoff for CAS retry attempt N
// (1-indexed). Base grows as min(1ms << (attempt-1), 32ms), with
// full jitter in [0, base]. Jitter avoids synchronized retries
// across concurrent callers on the same hot key.
func nextBackoff(attempt int) time.Duration {
	const (
		baseStep = time.Millisecond
		maxBase  = 32 * time.Millisecond
	)
	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 30 {
		shift = 30
	}
	base := baseStep << shift
	if base > maxBase {
		base = maxBase
	}
	return time.Duration(rand.Int64N(int64(base) + 1))
}

// sleepCtx sleeps for d or until ctx is canceled. Returns true if
// the full duration elapsed, false if ctx was canceled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Counters returns a point-in-time snapshot of the store's internal
// metrics. Each value is read atomically so concurrent Allow calls
// cannot produce a torn read. Intended for OpenTelemetry plumbing.
func (s *NATSKVStore) Counters() map[string]int64 {
	return map[string]int64{
		"ratelimit_natskv_decisions_allowed":  s.counters.allowed.Load(),
		"ratelimit_natskv_decisions_rejected": s.counters.rejected.Load(),
		"ratelimit_natskv_cas_retries":        s.counters.casRetries.Load(),
		"ratelimit_natskv_budget_exhausted":   s.counters.budgetExhausted.Load(),
		"ratelimit_natskv_circuit_state":      s.counters.circuitState.Load(),
		"ratelimit_natskv_breaker_transitions": s.counters.breakerTransitions.Load(),
		"ratelimit_natskv_circuit_rejected":   s.counters.circuitRejected.Load(),
	}
}
