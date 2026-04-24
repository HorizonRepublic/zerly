package ratelimit

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"
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

// FlushPrefix removes every key in the backing bucket whose name
// starts with prefix. Used by the gateway's hot-reload path to drop
// stale GCRA state after a route reconfiguration so that a tightened
// limit does not keep honoring a burst accumulated under the old
// config.
//
// When the store is backed by a non-JetStream kvAPI (the in-memory
// test fake), this is a no-op: test suites drive state directly and
// do not need a prefix sweep. Returning nil here keeps test wiring
// simple and matches the contract — "best-effort flush" — without
// leaking a fake-specific branch upward.
//
// On the JetStream path the method iterates ListKeys in streaming
// mode and issues a Delete (tombstone, not Purge) for each matching
// key. The first error aborts the sweep and is returned; already-
// deleted keys are reported by the iterator as future no-ops and do
// not surface as errors.
func (s *NATSKVStore) FlushPrefix(ctx context.Context, prefix string) error {
	adapter, ok := s.kv.(*jsKVAdapter)
	if !ok {
		return nil
	}

	lister, err := adapter.kv.ListKeys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return nil
		}

		return fmt.Errorf("nats-kv list: %w", err)
	}
	defer func() { _ = lister.Stop() }()

	for key := range lister.Keys() {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if err := adapter.kv.Delete(ctx, key); err != nil {
			return fmt.Errorf("nats-kv delete %q: %w", key, err)
		}
	}

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

// NATSKVStoreConfig is the production wiring for [NewNATSKVStore].
// Zero value is not valid: JS and HandlerBucket are mandatory.
//
// The store derives its own bucket name from HandlerBucket +
// BucketSuffix so that multiple rate-limit stores attached to
// different handler registries cannot collide in a shared NATS
// account. Replicas count is inherited from the handler registry
// bucket so the rate-limit state has the same durability class as
// the route metadata it protects — there is no useful deployment
// where routes are 3-replica and their rate-limit TAT is 1-replica.
type NATSKVStoreConfig struct {
	// JS is the JetStream handle used to create or open the
	// rate-limit bucket and to query the handler registry for its
	// replica count. MUST be a live, connected handle; constructor
	// does not retry.
	JS jetstream.JetStream

	// HandlerBucket is the name of the handler registry KV bucket
	// whose replica count the rate-limit bucket inherits. The
	// registry bucket MUST already exist — the rate-limit bucket is
	// always a downstream companion, never the first bucket in an
	// account. Empty value is rejected.
	HandlerBucket string

	// BucketSuffix is appended to HandlerBucket to form this store's
	// bucket name (e.g., "_ratelimit" → "handler_registry_ratelimit").
	// Callers MAY pass an empty suffix, but it is STRONGLY
	// discouraged — a shared bucket between routes and TAT state
	// courts subject-name collisions and makes bucket-level ops
	// (flush, status, replica changes) impossible to scope.
	BucketSuffix string

	// KeyTTL is written into the bucket's MaxAge so that a key idle
	// longer than this is automatically evicted. Chosen to be larger
	// than the GCRA tokens-to-fill time for the slowest expected
	// route (otherwise legitimate callers see the fresh-bucket path
	// after a quiet period), but small enough that a one-off spike
	// does not leak state for days. Zero value disables TTL — not
	// recommended for production.
	KeyTTL time.Duration

	// Logger is plumbed into breaker state-change logs and the
	// constructor's bucket create/reuse info lines. A nil logger
	// degrades silently; pass zerolog.Nop() explicitly if silence is
	// intended.
	Logger zerolog.Logger
}

// NewNATSKVStore builds a production-ready NATSKVStore backed by a
// JetStream KV bucket. On first call for a given HandlerBucket the
// rate-limit bucket is created with replica count inherited from the
// handler registry bucket; subsequent calls reuse the existing
// bucket (idempotent startup).
//
// Bucket configuration is fixed: History=1 (only the latest TAT
// matters), Storage=Memory (state is disposable and RAM-speed writes
// matter far more than persistence across a server restart), TTL=
// cfg.KeyTTL for automatic stale-key cleanup, and MaxValueSize sized
// to the current TAT wire length plus a small forward-compat slack.
//
// Returns an error if cfg.JS is nil, cfg.HandlerBucket is empty, the
// handler registry bucket is missing (the rate-limit bucket can
// only exist as a downstream companion), or any JetStream API call
// fails for reasons other than bucket-already-exists / bucket-not-
// found.
func NewNATSKVStore(ctx context.Context, cfg NATSKVStoreConfig) (*NATSKVStore, error) {
	if cfg.JS == nil {
		return nil, errors.New("ratelimit: NATSKVStoreConfig.JS is required")
	}
	if cfg.HandlerBucket == "" {
		return nil, errors.New("ratelimit: NATSKVStoreConfig.HandlerBucket is required")
	}

	replicas, err := inheritReplicas(ctx, cfg.JS, cfg.HandlerBucket)
	if err != nil {
		return nil, err
	}

	bucketName := cfg.HandlerBucket + cfg.BucketSuffix
	kv, created, err := openOrCreateRatelimitBucket(ctx, cfg.JS, bucketName, replicas, cfg.KeyTTL)
	if err != nil {
		return nil, err
	}

	logBucketInit(cfg.Logger, bucketName, replicas, cfg.KeyTTL, created)

	return newNATSKVStoreFromKV(&jsKVAdapter{kv: kv}, withLogger(cfg.Logger)), nil
}

// inheritReplicas reads the replica count of the handler registry
// bucket so the rate-limit bucket can match it. Uses the concrete
// KeyValueBucketStatus type to reach into StreamInfo — the
// KeyValueStatus interface does not expose replicas directly.
func inheritReplicas(ctx context.Context, js jetstream.JetStream, handlerBucket string) (int, error) {
	kv, err := js.KeyValue(ctx, handlerBucket)
	if err != nil {
		return 0, fmt.Errorf("ratelimit: open handler bucket %q: %w", handlerBucket, err)
	}

	status, err := kv.Status(ctx)
	if err != nil {
		return 0, fmt.Errorf("ratelimit: status of %q: %w", handlerBucket, err)
	}

	bucketStatus, ok := status.(*jetstream.KeyValueBucketStatus)
	if !ok {
		return 0, fmt.Errorf("ratelimit: status of %q: unexpected type %T", handlerBucket, status)
	}

	info := bucketStatus.StreamInfo()
	if info == nil {
		return 0, fmt.Errorf("ratelimit: status of %q: nil StreamInfo", handlerBucket)
	}

	return info.Config.Replicas, nil
}

// openOrCreateRatelimitBucket returns an existing rate-limit bucket
// or creates a fresh one with the provided replicas + TTL. The
// boolean return reports whether a creation happened (true) or the
// bucket was reused (false); callers use this for log framing only.
func openOrCreateRatelimitBucket(
	ctx context.Context,
	js jetstream.JetStream,
	bucket string,
	replicas int,
	ttl time.Duration,
) (jetstream.KeyValue, bool, error) {
	kv, err := js.KeyValue(ctx, bucket)
	if err == nil {
		return kv, false, nil
	}
	if !errors.Is(err, jetstream.ErrBucketNotFound) {
		return nil, false, fmt.Errorf("ratelimit: open bucket %q: %w", bucket, err)
	}

	kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:       bucket,
		History:      1,
		Storage:      jetstream.MemoryStorage,
		Replicas:     replicas,
		TTL:          ttl,
		MaxValueSize: int32(tatEncodedLength) + 64,
	})
	if err != nil {
		return nil, false, fmt.Errorf("ratelimit: create bucket %q: %w", bucket, err)
	}

	return kv, true, nil
}

// logBucketInit emits a single info line on bucket create or reuse
// so operators can confirm startup topology without tailing debug
// logs. Kept separate from the construction path so the happy-path
// function reads top-to-bottom without log noise.
func logBucketInit(logger zerolog.Logger, bucket string, replicas int, ttl time.Duration, created bool) {
	event := logger.Info().
		Str("event", "ratelimit.kv.bucket.init").
		Str("bucket", bucket).
		Int("replicas", replicas).
		Dur("max_age", ttl).
		Bool("created", created)
	if created {
		event.Msg("ratelimit KV bucket created")

		return
	}
	event.Msg("ratelimit KV bucket reused")
}

// jsKVAdapter wraps a concrete jetstream.KeyValue so it satisfies
// the package-internal kvAPI interface. Keeping the adapter thin
// lets the CAS loop stay backend-agnostic and lets tests swap in a
// fake without mocking JetStream.
type jsKVAdapter struct {
	kv jetstream.KeyValue
}

// Get returns the latest entry for key or errKVKeyNotFound if the
// key is absent. Any other error is propagated unchanged so the
// breaker counts it as a backend failure.
func (a *jsKVAdapter) Get(ctx context.Context, key string) (kvEntry, error) {
	entry, err := a.kv.Get(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, errKVKeyNotFound
		}

		return nil, err
	}

	return jsEntryAdapter{entry: entry}, nil
}

// Create writes a new entry or returns errCASConflict if the key
// already exists. JetStream reports this as ErrKeyExists, whose
// underlying APIError.ErrorCode is JSErrCodeStreamWrongLastSequence;
// errors.Is catches both the sentinel and the raw API error shape.
func (a *jsKVAdapter) Create(ctx context.Context, key string, value []byte) (uint64, error) {
	rev, err := a.kv.Create(ctx, key, value)
	if err != nil {
		if isJSRevisionConflict(err) {
			return 0, errCASConflict
		}

		return 0, err
	}

	return rev, nil
}

// Update writes a new revision of an existing key or returns
// errCASConflict on revision mismatch. JetStream Update uses
// WithExpectLastSequencePerSubject under the hood, so a stale
// revision surfaces as an APIError with ErrorCode
// JSErrCodeStreamWrongLastSequence — the same code as ErrKeyExists.
// isJSRevisionConflict classifies both paths uniformly.
func (a *jsKVAdapter) Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error) {
	rev, err := a.kv.Update(ctx, key, value, revision)
	if err != nil {
		if isJSRevisionConflict(err) {
			return 0, errCASConflict
		}

		return 0, err
	}

	return rev, nil
}

// jsEntryAdapter exposes the two fields NATSKVStore cares about —
// raw bytes and revision number — from a jetstream.KeyValueEntry.
// The entry's other metadata (Bucket, Key, Created, Delta) is
// intentionally dropped to keep the kvAPI surface minimal.
type jsEntryAdapter struct {
	entry jetstream.KeyValueEntry
}

// Value returns the stored TAT bytes.
func (e jsEntryAdapter) Value() []byte { return e.entry.Value() }

// Revision returns the monotonic sequence number used as the CAS
// precondition for subsequent Update calls.
func (e jsEntryAdapter) Revision() uint64 { return e.entry.Revision() }

// isJSRevisionConflict reports whether err is a CAS conflict
// signaled by JetStream. Matches on ErrKeyExists (Create path) and
// on any APIError carrying JSErrCodeStreamWrongLastSequence (Update
// path) — errors.Is(err, ErrKeyExists) resolves to true for both
// because APIError.Is compares ErrorCode and ErrKeyExists carries
// that very code.
//
// ErrKeyDeleted is also treated as a conflict: a key vanishing
// between Get and Update means some other writer beat us to the
// punch and a retry with a fresh Get is the correct response.
func isJSRevisionConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, jetstream.ErrKeyExists) {
		return true
	}
	if errors.Is(err, jetstream.ErrKeyDeleted) {
		return true
	}

	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence {
		return true
	}

	return false
}
