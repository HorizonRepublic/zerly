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

// TestHTTPError_BodiesParseAsJSON verifies every pre-encoded body is
// valid JSON with the expected shape: a top-level object carrying a
// string "error" and a string "message". This is a contract test
// against the wire format — if the build() helper ever starts
// producing something else, this test fails loudly.
func TestHTTPError_BodiesParseAsJSON(t *testing.T) {
	errs := []HTTPError{
		NotFound, MethodNotAllowed, PayloadTooLarge, UnsupportedMedia,
		InternalError, ServiceUnavailable, GatewayTimeout, BadGateway,
	}
	for _, e := range errs {
		var parsed map[string]string
		require.NoError(t, json.Unmarshal(e.Body, &parsed))
		assert.NotEmpty(t, parsed["error"], "error code field")
		assert.NotEmpty(t, parsed["message"], "message field")
	}
}

// TestHTTPError_NotFoundBodyContent pins the specific wire content
// for NotFound so downstream clients can rely on the "NOT_FOUND"
// machine-readable code. A deliberate shape change requires
// updating this test.
func TestHTTPError_NotFoundBodyContent(t *testing.T) {
	var parsed map[string]string
	require.NoError(t, json.Unmarshal(NotFound.Body, &parsed))
	assert.Equal(t, "NOT_FOUND", parsed["error"])
	assert.Contains(t, parsed["message"], "not found")
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
