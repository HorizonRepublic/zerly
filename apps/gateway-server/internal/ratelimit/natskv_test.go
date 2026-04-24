package ratelimit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeTAT_RoundTrip(t *testing.T) {
	t0 := time.Unix(0, 1_735_837_293_847_123_456)
	enc := encodeTAT(t0)
	require.Len(t, enc, tatEncodedLength)
	assert.Equal(t, tatVersion1, enc[0])

	dec, err := decodeTAT(enc)
	require.NoError(t, err)
	assert.True(t, t0.Equal(dec))
}

func TestDecodeTAT_RejectsWrongLength(t *testing.T) {
	_, err := decodeTAT([]byte{0x01, 0, 0})
	assert.Error(t, err)
}

func TestDecodeTAT_RejectsUnknownVersion(t *testing.T) {
	bad := []byte{0xFF, 0, 0, 0, 0, 0, 0, 0, 0}
	_, err := decodeTAT(bad)
	assert.Error(t, err)
}

// fakeEntry implements kvEntry for tests.
type fakeEntry struct {
	value    []byte
	revision uint64
}

func (e fakeEntry) Value() []byte    { return e.value }
func (e fakeEntry) Revision() uint64 { return e.revision }

// fakeKV implements kvAPI for tests: in-memory map with per-call
// error injection and call counters. Safe for concurrent use.
type fakeKV struct {
	mu      sync.Mutex
	entries map[string]fakeEntry
	nextRev uint64

	writeErr      error
	conflictCount int

	getCalls    int
	createCalls int
	updateCalls int
}

func newFakeKV() *fakeKV {
	return &fakeKV{entries: map[string]fakeEntry{}, nextRev: 1}
}

func (k *fakeKV) setWriteError(err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.writeErr = err
}

func (k *fakeKV) setCASConflictCount(n int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.conflictCount = n
}

func (k *fakeKV) setInitial(key string, value []byte) {
	k.mu.Lock()
	defer k.mu.Unlock()
	rev := k.nextRev
	k.nextRev++
	k.entries[key] = fakeEntry{value: value, revision: rev}
}

func (k *fakeKV) Get(_ context.Context, key string) (kvEntry, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.getCalls++
	e, ok := k.entries[key]
	if !ok {
		return nil, errKVKeyNotFound
	}
	return e, nil
}

func (k *fakeKV) Create(_ context.Context, key string, value []byte) (uint64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.createCalls++
	if k.writeErr != nil {
		return 0, k.writeErr
	}
	if k.conflictCount > 0 {
		k.conflictCount--
		return 0, errCASConflict
	}
	if _, exists := k.entries[key]; exists {
		return 0, errCASConflict
	}
	rev := k.nextRev
	k.nextRev++
	k.entries[key] = fakeEntry{value: value, revision: rev}
	return rev, nil
}

func (k *fakeKV) Update(_ context.Context, key string, value []byte, revision uint64) (uint64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.updateCalls++
	if k.writeErr != nil {
		return 0, k.writeErr
	}
	if k.conflictCount > 0 {
		k.conflictCount--
		return 0, errCASConflict
	}
	cur, ok := k.entries[key]
	if !ok {
		return 0, errKVKeyNotFound
	}
	if cur.revision != revision {
		return 0, errCASConflict
	}
	rev := k.nextRev
	k.nextRev++
	k.entries[key] = fakeEntry{value: value, revision: rev}
	return rev, nil
}

// testNATSKVStore builds a store wired to the provided fake KV with
// deterministic defaults. The default CAS budget is generous (1s)
// so tests pass on slow CI runners; override via withCASBudget.
func testNATSKVStore(t *testing.T, kv kvAPI, opts ...natskvOption) *NATSKVStore {
	t.Helper()
	base := []natskvOption{withCASBudget(time.Second), withBreakerFailures(100)}
	return newNATSKVStoreFromKV(kv, append(base, opts...)...)
}

func TestNATSKVStore_FirstRequestCreatesEntry(t *testing.T) {
	kv := newFakeKV()
	sut := testNATSKVStore(t, kv)

	d, err := sut.Allow(context.Background(), "k", 100, 5)

	require.NoError(t, err)
	assert.True(t, d.Allowed)
	assert.Equal(t, 1, kv.getCalls)
	assert.Equal(t, 1, kv.createCalls)
	assert.Equal(t, 0, kv.updateCalls)
	assert.Equal(t, int64(1), sut.counters.allowed.Load())
}

func TestNATSKVStore_SecondRequestUsesUpdate(t *testing.T) {
	kv := newFakeKV()
	sut := testNATSKVStore(t, kv)
	ctx := context.Background()

	_, err := sut.Allow(ctx, "k", 100, 5)
	require.NoError(t, err)

	d, err := sut.Allow(ctx, "k", 100, 5)

	require.NoError(t, err)
	assert.True(t, d.Allowed)
	assert.Equal(t, 2, kv.getCalls)
	assert.Equal(t, 1, kv.createCalls)
	assert.Equal(t, 1, kv.updateCalls)
	assert.Equal(t, int64(2), sut.counters.allowed.Load())
}

func TestNATSKVStore_RejectsWithoutWrite(t *testing.T) {
	kv := newFakeKV()
	sut := testNATSKVStore(t, kv)
	ctx := context.Background()

	// Drain the burst by hammering the key until a reject appears.
	// rps=1 burst=2 means at most 2 tokens stored; a tight loop
	// guarantees the 3rd call rejects without any timing assumptions.
	rejected := false
	var createsAtReject, updatesAtReject int
	for i := 0; i < 10; i++ {
		d, err := sut.Allow(ctx, "k", 1, 2)
		require.NoError(t, err)
		if !d.Allowed {
			rejected = true
			createsAtReject = kv.createCalls
			updatesAtReject = kv.updateCalls
			break
		}
	}
	require.True(t, rejected, "expected at least one reject")

	// A follow-up reject MUST NOT issue any write.
	d, err := sut.Allow(ctx, "k", 1, 2)
	require.NoError(t, err)
	assert.False(t, d.Allowed)

	assert.Equal(t, createsAtReject, kv.createCalls, "no create on reject")
	assert.Equal(t, updatesAtReject, kv.updateCalls, "no update on reject")
	assert.GreaterOrEqual(t, sut.counters.rejected.Load(), int64(2))
}

func TestNATSKVStore_CASConflictRetries(t *testing.T) {
	kv := newFakeKV()
	// Seed a real encoded TAT so decode succeeds.
	kv.setInitial("k", encodeTAT(time.Now().Add(-time.Second)))
	kv.setCASConflictCount(2)

	sut := testNATSKVStore(t, kv)

	d, err := sut.Allow(context.Background(), "k", 100, 5)

	require.NoError(t, err)
	assert.True(t, d.Allowed)
	assert.Equal(t, int64(2), sut.counters.casRetries.Load())
}

func TestNATSKVStore_BudgetExhausted(t *testing.T) {
	kv := newFakeKV()
	kv.setInitial("k", encodeTAT(time.Now().Add(-time.Second)))
	kv.setCASConflictCount(1000)

	sut := testNATSKVStore(t, kv, withCASBudget(5*time.Millisecond))

	_, err := sut.Allow(context.Background(), "k", 100, 5)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCASBudgetExhausted)
	assert.Equal(t, int64(1), sut.counters.budgetExhausted.Load())
}

// TestNATSKVStore_CorruptTATRecoversAndLogs guards the decodeTAT
// fail-soft path: a corrupt KV entry (wrong version byte, truncated
// layout) MUST NOT panic or silently pass the request through. The
// store falls back to a fresh bucket (so the request is evaluated
// against a zero TAT, which is correct GCRA semantics for "no prior
// state") while emitting a structured WARN so operators see the drift.
func TestNATSKVStore_CorruptTATRecoversAndLogs(t *testing.T) {
	kv := newFakeKV()
	// Seed an entry whose version byte does not match tatVersion1 —
	// decodeTAT rejects it and the store must fall back to fresh.
	kv.setInitial("k", []byte{0xFF, 0, 0, 0, 0, 0, 0, 0, 0})

	var logBuf bytes.Buffer
	sink := zerolog.New(&logBuf)

	sut := testNATSKVStore(t, kv, withLogger(sink))

	d, err := sut.Allow(context.Background(), "k", 100, 5)

	require.NoError(t, err)
	assert.True(t, d.Allowed, "fresh bucket allows the first request")
	assert.Equal(t, int64(1), sut.counters.corruptTAT.Load(),
		"corrupt-TAT counter must tick so operators see the recovery")
	assert.Contains(t, sut.Counters(), "ratelimit_natskv_corrupt_tat",
		"corrupt-TAT counter must appear in the Counters snapshot for OTel export")

	// Walk the JSON-structured log lines and assert the warn record
	// carries enough context (key, event, decoder error) for an
	// operator to locate the corrupt entry.
	var sawCorruptWarn bool
	for _, line := range bytes.Split(bytes.TrimSpace(logBuf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}

		var record map[string]any
		require.NoError(t, json.Unmarshal(line, &record), "log sink must emit valid JSON")

		if record["event"] == "ratelimit.kv.corrupt_tat" {
			sawCorruptWarn = true
			assert.Equal(t, "warn", record["level"])
			assert.Equal(t, "k", record["key"])
			assert.NotEmpty(t, record["error"])
		}
	}

	assert.True(t, sawCorruptWarn,
		"a WARN record with event=ratelimit.kv.corrupt_tat must be emitted")
}

func TestNATSKVStore_BreakerOpensAfterFailures(t *testing.T) {
	kv := newFakeKV()
	kv.setWriteError(errors.New("nats down"))

	sut := testNATSKVStore(t, kv, withBreakerFailures(3))
	ctx := context.Background()

	// Three consecutive failures trip the breaker.
	for i := 0; i < 3; i++ {
		_, err := sut.Allow(ctx, "k", 100, 5)
		require.Error(t, err)
	}

	getsBefore := kv.getCalls

	_, err := sut.Allow(ctx, "k", 100, 5)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCircuitOpen)
	assert.Equal(t, getsBefore, kv.getCalls, "breaker short-circuits backend calls")
	assert.Equal(t, int64(1), sut.counters.circuitRejected.Load(), "circuit-open rejection increments counter")
}
