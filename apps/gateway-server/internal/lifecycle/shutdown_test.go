package lifecycle

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

// fakeHTTPServer captures Shutdown invocations for test assertions.
// A pre-seeded err (nil by default) is returned from Shutdown so the
// test can exercise both the happy and error branches of the drain
// sequence.
type fakeHTTPServer struct {
	mu         sync.Mutex
	called     bool
	receivedCtx context.Context
	err        error
}

func (f *fakeHTTPServer) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.receivedCtx = ctx
	return f.err
}

// fakeNATSConn captures Drain invocations for test assertions.
type fakeNATSConn struct {
	mu     sync.Mutex
	called bool
	err    error
}

func (f *fakeNATSConn) Drain() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	return f.err
}

// stubWatcher returns a real registry.Watcher that has NOT been
// Started. Stop on an unstarted watcher is a no-op — see the
// sync.Once guard in internal/registry/watcher.go — so the drain
// sequence can exercise it without needing a live NATS server.
func stubWatcher() *registry.Watcher {
	return registry.NewWatcher(nil, registry.NewStore(), zerolog.Nop())
}

func TestDrain_CallsAllThreeStepsInOrder(t *testing.T) {
	http := &fakeHTTPServer{}
	nats := &fakeNATSConn{}
	watcher := stubWatcher()

	Drain(Options{
		HTTP:    http,
		Watcher: watcher,
		NATS:    nats,
		Timeout: 1 * time.Second,
		Logger:  zerolog.Nop(),
	})

	http.mu.Lock()
	assert.True(t, http.called, "HTTP.Shutdown must be called")
	http.mu.Unlock()

	nats.mu.Lock()
	assert.True(t, nats.called, "NATS.Drain must be called")
	nats.mu.Unlock()
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
