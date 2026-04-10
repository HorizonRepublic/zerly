package observability

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogger_ValidLevelsAndFormats(t *testing.T) {
	cases := []struct {
		level  string
		format string
	}{
		{"trace", "json"},
		{"debug", "json"},
		{"info", "json"},
		{"warn", "console"},
		{"error", "console"},
		{"TRACE", "JSON"},
		{"Info", "Console"},
	}

	for _, tc := range cases {
		logger, err := NewLogger(tc.level, tc.format)
		require.NoError(t, err, "level=%s format=%s", tc.level, tc.format)
		assert.NotEqual(t, zerolog.Nop(), logger)
	}
}

func TestNewLogger_InvalidLevelReturnsError(t *testing.T) {
	_, err := NewLogger("loud", "json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse log level")
}

func TestNewLogger_InvalidFormatReturnsError(t *testing.T) {
	_, err := NewLogger("info", "xml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported log format")
}

func TestNewLogger_DefaultFormatIsJSON(t *testing.T) {
	_, err := NewLogger("info", "")
	require.NoError(t, err)
}
