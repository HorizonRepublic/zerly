package lifecycle

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

// stepRecorder is a shared monotonic counter the fake collaborators
// use to stamp the order in which Drain invokes them. The sequence
// numbers expose the strict HTTP→watcher→NATS contract that the
// shutdown sequence promises.
type stepRecorder struct {
	counter atomic.Int64
}

func (r *stepRecorder) next() int64 { return r.counter.Add(1) }

// fakeHTTPServer captures Shutdown invocations for test assertions.
// A pre-seeded err (nil by default) is returned from Shutdown so the
// test can exercise both the happy and error branches of the drain
// sequence.
type fakeHTTPServer struct {
	mu          sync.Mutex
	called      bool
	receivedCtx context.Context
	err         error
	recorder    *stepRecorder
	order       int64
}

func (f *fakeHTTPServer) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.receivedCtx = ctx
	if f.recorder != nil {
		f.order = f.recorder.next()
	}
	return f.err
}

// fakeNATSConn captures Drain invocations for test assertions.
type fakeNATSConn struct {
	mu       sync.Mutex
	called   bool
	err      error
	recorder *stepRecorder
	order    int64
	// blockUntil, when non-nil, holds the goroutine inside Drain until
	// the channel is closed or the receive races a return. Used by the
	// drain-timeout test to simulate a hung NATS connection.
	blockUntil chan struct{}
}

func (f *fakeNATSConn) Drain() error {
	if f.blockUntil != nil {
		<-f.blockUntil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	if f.recorder != nil {
		f.order = f.recorder.next()
	}
	return f.err
}

// stubWatcher returns a real registry.Watcher that has NOT been
// Started. Stop on an unstarted watcher is a no-op — see the
// sync.Once guard in internal/registry/watcher.go — so the drain
// sequence can exercise it without needing a live NATS server.
func stubWatcher() *registry.Watcher {
	return registry.NewWatcher(nil, registry.NewStore(), zerolog.Nop())
}

// recordingWatcher is a WatcherStopper used by the ordering test to
// stamp the drain step's sequence number into a shared recorder.
type recordingWatcher struct {
	mu       sync.Mutex
	called   bool
	recorder *stepRecorder
	order    int64
}

func (r *recordingWatcher) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.called = true
	if r.recorder != nil {
		r.order = r.recorder.next()
	}
}

func TestDrain_CallsAllThreeStepsInOrder(t *testing.T) {
	recorder := &stepRecorder{}
	http := &fakeHTTPServer{recorder: recorder}
	nats := &fakeNATSConn{recorder: recorder}
	watcher := &recordingWatcher{recorder: recorder}

	Drain(Options{
		HTTP:    http,
		Watcher: watcher,
		NATS:    nats,
		Timeout: 1 * time.Second,
		Logger:  zerolog.Nop(),
	})

	http.mu.Lock()
	require.True(t, http.called, "HTTP.Shutdown must be called")
	httpOrder := http.order
	http.mu.Unlock()

	watcher.mu.Lock()
	require.True(t, watcher.called, "Watcher.Stop must be called")
	watcherOrder := watcher.order
	watcher.mu.Unlock()

	nats.mu.Lock()
	require.True(t, nats.called, "NATS.Drain must be called")
	natsOrder := nats.order
	nats.mu.Unlock()

	// Drain MUST quiesce HTTP first (stop accepting new work), watcher
	// second (so a late KV delta cannot mutate the routing table after
	// we have stopped serving), and NATS last (so any in-flight
	// upstream replies have a chance to land before we close the
	// socket). Reordering breaks the no-request-left-behind guarantee
	// during rolling deployments.
	assert.Less(t, httpOrder, watcherOrder, "HTTP.Shutdown must run before Watcher.Stop")
	assert.Less(t, watcherOrder, natsOrder, "Watcher.Stop must run before NATS.Drain")
}

func TestDrain_HTTPErrorDoesNotAbortSequence(t *testing.T) {
	http := &fakeHTTPServer{err: errors.New("http shutdown boom")}
	nats := &fakeNATSConn{}
	watcher := stubWatcher()

	Drain(Options{
		HTTP:    http,
		Watcher: watcher,
		NATS:    nats,
		Timeout: 1 * time.Second,
		Logger:  zerolog.Nop(),
	})

	nats.mu.Lock()
	assert.True(t, nats.called, "NATS drain must still run even after HTTP.Shutdown failure")
	nats.mu.Unlock()
}

func TestDrain_NATSErrorDoesNotPanic(t *testing.T) {
	http := &fakeHTTPServer{}
	nats := &fakeNATSConn{err: errors.New("nats drain boom")}
	watcher := stubWatcher()

	assert.NotPanics(t, func() {
		Drain(Options{
			HTTP:    http,
			Watcher: watcher,
			NATS:    nats,
			Timeout: 1 * time.Second,
			Logger:  zerolog.Nop(),
		})
	})
}

func TestDrain_AppliesTimeoutToHTTPShutdown(t *testing.T) {
	// Verify the context passed into HTTP.Shutdown carries the
	// configured timeout as its deadline. This is the only step
	// that actually consumes the context; confirming it here keeps
	// the timeout wiring honest.
	http := &fakeHTTPServer{}
	nats := &fakeNATSConn{}
	watcher := stubWatcher()

	const timeout = 500 * time.Millisecond
	Drain(Options{
		HTTP:    http,
		Watcher: watcher,
		NATS:    nats,
		Timeout: timeout,
		Logger:  zerolog.Nop(),
	})

	http.mu.Lock()
	defer http.mu.Unlock()
	require.NotNil(t, http.receivedCtx)
	deadline, ok := http.receivedCtx.Deadline()
	require.True(t, ok, "HTTP.Shutdown must receive a context with a deadline")
	remaining := time.Until(deadline)
	assert.Greater(t, remaining, time.Duration(0))
	assert.LessOrEqual(t, remaining, timeout)
}

func TestDrain_NATSDrainTimeoutDoesNotBlockShutdown(t *testing.T) {
	// Verify a hung NATS Drain cannot stall the gateway past the
	// configured timeout. The fake's Drain blocks indefinitely on
	// blockUntil so the only way Drain returns is via the goroutine
	// timeout branch in drainNATS.
	http := &fakeHTTPServer{}
	nats := &fakeNATSConn{blockUntil: make(chan struct{})}
	watcher := stubWatcher()

	const timeout = 50 * time.Millisecond
	const slack = 250 * time.Millisecond

	done := make(chan struct{})
	go func() {
		Drain(Options{
			HTTP:    http,
			Watcher: watcher,
			NATS:    nats,
			Timeout: timeout,
			Logger:  zerolog.Nop(),
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout + slack):
		// Unblock the goroutine so we do not leak it past the test.
		close(nats.blockUntil)
		t.Fatalf("Drain did not return within timeout+slack (%v)", timeout+slack)
	}

	// Release the orphan goroutine so the test process does not retain
	// it — drain returned via the timeout branch but the worker is
	// still parked on the channel send.
	close(nats.blockUntil)
}

func TestDrain_StopsWatcherIdempotently(t *testing.T) {
	// Calling Drain twice against the same watcher must not panic.
	// The watcher's sync.Once guards Stop, so the second Drain
	// should observe a no-op.
	http := &fakeHTTPServer{}
	nats := &fakeNATSConn{}
	watcher := stubWatcher()

	opts := Options{
		HTTP:    http,
		Watcher: watcher,
		NATS:    nats,
		Timeout: 1 * time.Second,
		Logger:  zerolog.Nop(),
	}

	assert.NotPanics(t, func() {
		Drain(opts)
		Drain(opts)
	})
}
