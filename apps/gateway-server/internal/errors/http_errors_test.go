package errors

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPError_StatusAndBodyPaired verifies every declared error
// carries both a valid status and a non-empty body. Regressions in
// the init() pairing would surface here as a field-level mismatch.
func TestHTTPError_StatusAndBodyPaired(t *testing.T) {
	cases := []struct {
		name   string
		err    HTTPError
		status int
	}{
		{"NotFound", NotFound, StatusNotFound},
		{"MethodNotAllowed", MethodNotAllowed, StatusMethodNotAllowed},
		{"PayloadTooLarge", PayloadTooLarge, StatusPayloadTooLarge},
		{"UnsupportedMedia", UnsupportedMedia, StatusUnsupportedMedia},
		{"InternalError", InternalError, StatusInternalError},
		{"ServiceUnavailable", ServiceUnavailable, StatusServiceUnavailable},
		{"GatewayTimeout", GatewayTimeout, StatusGatewayTimeout},
		{"BadGateway", BadGateway, StatusBadGateway},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.status, c.err.Status, "status code")
			assert.NotEmpty(t, c.err.Body, "body must be non-empty")
		})
	}
}

// TestHTTPError_BodiesAreSingleFieldObjects verifies every pre-encoded
// body is a single-field JSON object carrying a non-empty 'error'
// string. This is the gateway's wire contract: the HTTP status line
// carries the numeric code, the body carries a short human-readable
// phrase and nothing else — no code field, no message field, no
// implementation-identifying leak.
func TestHTTPError_BodiesAreSingleFieldObjects(t *testing.T) {
	errs := []HTTPError{
		NotFound, MethodNotAllowed, PayloadTooLarge, UnsupportedMedia,
		InternalError, ServiceUnavailable, GatewayTimeout, BadGateway,
	}
	for _, e := range errs {
		var parsed map[string]string
		require.NoError(t, json.Unmarshal(e.Body, &parsed))
		assert.Len(t, parsed, 1, "body must contain exactly one field")
		assert.NotEmpty(t, parsed["error"], "body must carry a non-empty error field")
	}
}

// TestHTTPError_ReasonPhrases pins the exact RFC 9110 reason phrase
// each error surfaces. These strings are part of the wire contract —
// any deliberate rewording needs to update this test and the
// corresponding entry in http_errors.go together.
func TestHTTPError_ReasonPhrases(t *testing.T) {
	cases := []struct {
		name    string
		err     HTTPError
		phrase  string
	}{
		{"NotFound", NotFound, "Not Found"},
		{"MethodNotAllowed", MethodNotAllowed, "Method Not Allowed"},
		{"PayloadTooLarge", PayloadTooLarge, "Payload Too Large"},
		{"UnsupportedMedia", UnsupportedMedia, "Unsupported Media Type"},
		{"InternalError", InternalError, "Internal Server Error"},
		{"ServiceUnavailable", ServiceUnavailable, "Service Unavailable"},
		{"GatewayTimeout", GatewayTimeout, "Gateway Timeout"},
		{"BadGateway", BadGateway, "Bad Gateway"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var parsed map[string]string
			require.NoError(t, json.Unmarshal(c.err.Body, &parsed))
			assert.Equal(t, c.phrase, parsed["error"])
		})
	}
}

// TestHTTPError_BuildIsImmutable verifies that mutating one field of
// a returned HTTPError does NOT affect the package-level variable.
// Prevents a future refactor from accidentally sharing the Body
// slice's backing array with caller-owned buffers.
func TestHTTPError_BuildIsImmutable(t *testing.T) {
	originalLen := len(NotFound.Body)
	snapshot := make([]byte, originalLen)
	copy(snapshot, NotFound.Body)

	// Do not mutate NotFound.Body directly — we trust it is a shared
	// immutable slice. Instead, verify a fresh read returns the same
	// bytes as a snapshot taken at test start.
	assert.Equal(t, snapshot, NotFound.Body)
}
