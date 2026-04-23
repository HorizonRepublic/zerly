package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeTAT_RoundTrip(t *testing.T) {
	t0 := time.Unix(0, 1_735_837_293_847_123_456)
	enc := encodeTAT(t0)
	require.Len(t, enc, tatEncodedLength)
	assert.Equal(t, tatVersion1, enc[0])

	dec, err := decodeTAT(enc)
	require.NoError(t, err)
	assert.True(t, t0.Equal(dec))
}

func TestDecodeTAT_RejectsWrongLength(t *testing.T) {
	_, err := decodeTAT([]byte{0x01, 0, 0})
	assert.Error(t, err)
}

func TestDecodeTAT_RejectsUnknownVersion(t *testing.T) {
	bad := []byte{0xFF, 0, 0, 0, 0, 0, 0, 0, 0}
	_, err := decodeTAT(bad)
	assert.Error(t, err)
}

func TestEncodeTAT_ZeroTime(t *testing.T) {
	z := time.Time{}
	enc := encodeTAT(z)
	dec, err := decodeTAT(enc)
	require.NoError(t, err)
	assert.True(t, z.Equal(dec))
}
