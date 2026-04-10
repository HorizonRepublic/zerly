// Package codec is the gateway's single entry point for JSON marshalling.
//
// All JSON encoding and decoding in the gateway MUST go through this
// package so the underlying implementation (sonic today) can be swapped
// in a single place — for example, to plug in a protobuf codec later
// or to revert to encoding/json for debugging.
package codec

import "github.com/bytedance/sonic"

// Marshal serializes v to JSON using sonic's optimized encoder.
//
// The returned slice is freshly allocated on every call. Callers on the
// hot path that need to reuse buffers should use codec alongside a
// bytes.Buffer pool rather than calling Marshal directly.
func Marshal(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

// Unmarshal decodes JSON bytes into v. Returns the underlying sonic
// error on failure, which callers SHOULD wrap with fmt.Errorf and
// request context before logging.
func Unmarshal(data []byte, v any) error {
	return sonic.Unmarshal(data, v)
}
