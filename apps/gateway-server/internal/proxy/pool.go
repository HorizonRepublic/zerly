package proxy

import (
	"bytes"
	"sync"
)

// Initial map capacities for pooled GatewayRequest instances. These
// numbers track the 95th percentile of typical requests observed in
// load-test fixtures and are tuned to avoid a realloc in the common
// case while keeping the steady-state pool footprint small.
const (
	initialParamsCap  = 4
	initialQueryCap   = 4
	initialHeadersCap = 16
)

// initialBufferCap is the initial byte capacity for the JSON
// marshalling buffer pool. 4 KiB covers the vast majority of envelope
// sizes without resizing while keeping the per-entry pool footprint
// bounded.
const initialBufferCap = 4096

// envelopePool reuses GatewayRequest instances across requests. Every
// acquired envelope is reset before use by acquireEnvelope and must be
// returned via releaseEnvelope once the NATS reply has been processed.
var envelopePool = sync.Pool{
	New: func() any {
		return &GatewayRequest{
			Params:  make(map[string]string, initialParamsCap),
			Query:   make(map[string]QueryValue, initialQueryCap),
			Headers: make(map[string]string, initialHeadersCap),
		}
	},
}

// acquireEnvelope fetches a pooled GatewayRequest and resets it so
// callers observe a zero-valued struct regardless of its prior history.
// The returned pointer MUST be released via releaseEnvelope on every
// code path — including error paths — or the pool footprint grows
// without bound.
func acquireEnvelope() *GatewayRequest {
	envelope, _ := envelopePool.Get().(*GatewayRequest)
	envelope.reset()
	return envelope
}

// releaseEnvelope returns an envelope to the pool. It is safe to call
// with a nil receiver to simplify defer statements.
func releaseEnvelope(envelope *GatewayRequest) {
	if envelope == nil {
		return
	}
	envelopePool.Put(envelope)
}

// bufferPool reuses byte buffers for JSON marshalling. Each buffer is
// pre-allocated with initialBufferCap bytes to cover the common case
// without a resize.
var bufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, initialBufferCap))
	},
}

// acquireBuffer fetches a pooled bytes.Buffer and resets it so callers
// see an empty buffer. The returned buffer MUST be released via
// releaseBuffer.
func acquireBuffer() *bytes.Buffer {
	buffer, _ := bufferPool.Get().(*bytes.Buffer)
	buffer.Reset()
	return buffer
}

// releaseBuffer returns a buffer to the pool. Safe with nil.
func releaseBuffer(buffer *bytes.Buffer) {
	if buffer == nil {
		return
	}
	bufferPool.Put(buffer)
}
