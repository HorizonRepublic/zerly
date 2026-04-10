package proxy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryValue_MarshalScalar(t *testing.T) {
	value := NewQueryValueString("alice")

	encoded, err := json.Marshal(value)

	require.NoError(t, err)
	assert.Equal(t, `"alice"`, string(encoded))
}

func TestQueryValue_MarshalEmptyScalar(t *testing.T) {
	value := NewQueryValueString("")

	encoded, err := json.Marshal(value)

	require.NoError(t, err)
	assert.Equal(t, `""`, string(encoded))
}

func TestQueryValue_MarshalMultiPreservesArrayShape(t *testing.T) {
	value := NewQueryValueStrings([]string{"a", "b", "c"})

	encoded, err := json.Marshal(value)

	require.NoError(t, err)
	assert.Equal(t, `["a","b","c"]`, string(encoded))
}

func TestQueryValue_MarshalSingleElementMultiStaysArray(t *testing.T) {
	// A repeated key that happens to have arrived with one observation
	// MUST still marshal as an array, so the NestJS handler's
	// Array.isArray() discriminator preserves the "repeated" semantics.
	value := NewQueryValueStrings([]string{"only"})

	encoded, err := json.Marshal(value)

	require.NoError(t, err)
	assert.Equal(t, `["only"]`, string(encoded))
}

func TestNewQueryValueStrings_NormalizesNilToEmptySlice(t *testing.T) {
	value := NewQueryValueStrings(nil)

	encoded, err := json.Marshal(value)

	require.NoError(t, err)
	assert.Equal(t, `[]`, string(encoded), "nil slice must normalize to empty array variant")
}

func TestQueryValue_UnmarshalScalar(t *testing.T) {
	var value QueryValue

	require.NoError(t, json.Unmarshal([]byte(`"alice"`), &value))
	assert.Equal(t, "alice", value.Single)
	assert.Nil(t, value.Multi)
}

func TestQueryValue_UnmarshalArray(t *testing.T) {
	var value QueryValue

	require.NoError(t, json.Unmarshal([]byte(`["a","b"]`), &value))
	assert.Equal(t, []string{"a", "b"}, value.Multi)
	assert.Equal(t, "", value.Single)
}

func TestQueryValue_UnmarshalRejectsNumber(t *testing.T) {
	var value QueryValue

	err := json.Unmarshal([]byte(`42`), &value)

	assert.Error(t, err, "non-string, non-array values must be rejected")
}

func TestQueryValue_UnmarshalRejectsObject(t *testing.T) {
	var value QueryValue

	err := json.Unmarshal([]byte(`{"k":"v"}`), &value)

	assert.Error(t, err)
}

func TestQueryValue_RoundTripInsideMap(t *testing.T) {
	original := map[string]QueryValue{
		"include": NewQueryValueString("profile"),
		"tags":    NewQueryValueStrings([]string{"go", "nats"}),
	}

	encoded, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded map[string]QueryValue
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, original, decoded)
}
