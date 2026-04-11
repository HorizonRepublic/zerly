// Package codec is the gateway's single entry point for JSON marshalling.
//
// All JSON encoding and decoding in the gateway MUST go through this
// package so the underlying implementation (sonic today) can be swapped
// in a single place — for example, to plug in a protobuf codec later
// or to revert to encoding/json for debugging.
//
// Two marshal entry points are exposed: Marshal returns a freshly
// allocated slice for ad-hoc callers and tests, while MarshalInto
// appends into a caller-owned scratch buffer for hot paths that pool
// their output slices.
package codec

import (
	"fmt"

	"github.com/bytedance/sonic"
	sonicenc "github.com/bytedance/sonic/encoder"
)

// Marshal serializes v to JSON using sonic's optimized encoder.
//
// The returned slice is freshly allocated on every call. Callers on the
// hot path that need to reuse buffers should prefer MarshalInto with a
// pooled scratch slice.
func Marshal(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

// MarshalInto serializes v into the caller-supplied buffer using
// sonic's buffer-appending encoder. The buffer is grown in place when
// v does not fit into its current capacity; the same backing array is
// reused when it does.
//
// Callers that own a pooled buffer MUST keep the pool entry pointing
// at the (possibly grown) slice header so future acquires observe the
// enlarged capacity. Passing a *[]byte from a sync.Pool of *[]byte
// satisfies this requirement naturally because the pointer value is
// stable across Put/Get cycles even when the slice header it points
// at has been reallocated.
//
// Prefer MarshalInto over Marshal on any hot path where the caller
// can hold a reusable buffer. Marshal is kept for ad-hoc callers and
// tests that do not care about allocation count.
func MarshalInto(buf *[]byte, v any) error {
	if err := sonicenc.EncodeInto(buf, v, 0); err != nil {
		return fmt.Errorf("codec marshal into buffer: %w", err)
	}
	return nil
}

// Unmarshal decodes JSON bytes into v. Returns the underlying sonic
// error on failure, which callers SHOULD wrap with fmt.Errorf and
// request context before logging.
func Unmarshal(data []byte, v any) error {
	return sonic.Unmarshal(data, v)
}
