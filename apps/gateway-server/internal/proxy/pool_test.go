package proxy

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAcquireEnvelope_ReturnsZeroedInstance(t *testing.T) {
	envelope := acquireEnvelope()
	defer releaseEnvelope(envelope)

	assert.Equal(t, RouteContext{}, envelope.Route)
	assert.Empty(t, envelope.Params)
	assert.Empty(t, envelope.Query)
	assert.Empty(t, envelope.Headers)
	assert.Nil(t, envelope.Body)
	assert.Equal(t, RequestMeta{}, envelope.Meta)
}

func TestAcquireEnvelope_PreAllocatesMaps(t *testing.T) {
	envelope := acquireEnvelope()
	defer releaseEnvelope(envelope)

	// Writing must not panic on a fresh envelope — the pool's New
	// function must have allocated backing maps with the documented
	// initial capacities.
	assert.NotPanics(t, func() {
		envelope.Params["id"] = "42"
		envelope.Query["q"] = "v"
		envelope.Headers["h"] = "v"
	})
}

func TestReleaseEnvelope_IsNilSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		releaseEnvelope(nil)
	})
}

func TestAcquireReleaseCycle_ResetsPriorState(t *testing.T) {
	// A previously-used envelope returned to the pool must not leak
	// state into the next acquirer. We can't assert the same instance
	// is returned (sync.Pool is non-deterministic) but we can assert
	// that whatever comes out is zeroed.
	first := acquireEnvelope()
	first.Route.Method = "POST"
	first.Params["leaked"] = "yes"
	first.Headers["x-leaked"] = "yes"
	releaseEnvelope(first)

	second := acquireEnvelope()
	defer releaseEnvelope(second)
	assert.Equal(t, "", second.Route.Method)
	assert.NotContains(t, second.Params, "leaked")
	assert.NotContains(t, second.Headers, "x-leaked")
}

func TestAcquireBuffer_ReturnsEmptyBuffer(t *testing.T) {
	buffer := acquireBuffer()
	defer releaseBuffer(buffer)

	assert.Equal(t, 0, buffer.Len())
}

func TestAcquireBufferCycle_ResetsPriorState(t *testing.T) {
	first := acquireBuffer()
	first.WriteString("hello")
	releaseBuffer(first)

	second := acquireBuffer()
	defer releaseBuffer(second)
	assert.Equal(t, 0, second.Len(), "buffer must be reset between acquires")
}

func TestReleaseBuffer_IsNilSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		releaseBuffer(nil)
	})
}

func TestBufferPool_BackingArrayType(t *testing.T) {
	// Defensive: make sure the pool's New yields a non-nil buffer
	// with the configured initial capacity. This would fail if a
	// future refactor accidentally returns (*bytes.Buffer)(nil).
	buffer := acquireBuffer()
	defer releaseBuffer(buffer)

	assert.NotNil(t, buffer)
	_, ok := any(buffer).(*bytes.Buffer)
	assert.True(t, ok, "pool must return *bytes.Buffer")
}
