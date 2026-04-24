package nats

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildDisconnectErrHandler_NilErrLogsAtInfo pins the level
// switch on a graceful disconnect: nats.go invokes the handler with
// a nil error during clean shutdown (Drain, Close on a healthy
// socket). Logging that at ERROR floods alert pipelines on every
// pod restart.
func TestBuildDisconnectErrHandler_NilErrLogsAtInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	handler := buildDisconnectErrHandler(logger)
	handler(nil, nil)

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))

	assert.Equal(t, "info", record["level"], "graceful disconnect must log at info")
	assert.Equal(t, "nats disconnected gracefully", record["message"])
	assert.NotContains(t, record, zerolog.ErrorFieldName)
}

// TestBuildDisconnectErrHandler_NonNilErrLogsAtError covers the
// genuine-fault branch: a transport error MUST surface at ERROR with
// the cause attached so operators see the failure in their alert
// stream.
func TestBuildDisconnectErrHandler_NonNilErrLogsAtError(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	handler := buildDisconnectErrHandler(logger)
	handler(nil, errors.New("connection reset"))

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))

	assert.Equal(t, "error", record["level"], "transport fault must log at error")
	assert.Equal(t, "nats disconnected with error", record["message"])
	assert.Equal(t, "connection reset", record[zerolog.ErrorFieldName])
}
