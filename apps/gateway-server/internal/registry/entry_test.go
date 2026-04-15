package registry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandlerEntry_ParseLegacyRoute pins backward compatibility: a
// route entry written by the previous SDK version (no auth, no
// verifier field) must still parse cleanly so rolling deploys can
// introduce the new gateway-server alongside existing Nest services
// without breaking their registrations.
func TestHandlerEntry_ParseLegacyRoute(t *testing.T) {
	raw := []byte(`{"http":{"method":"GET","path":"/users/:id"}}`)

	var sut HandlerEntry
	require.NoError(t, json.Unmarshal(raw, &sut))

	require.NotNil(t, sut.HTTP)
	assert.Equal(t, "GET", sut.HTTP.Method)
	assert.Equal(t, "/users/:id", sut.HTTP.Path)
	assert.Nil(t, sut.Auth)
	assert.Nil(t, sut.Verifier)
}

func TestHandlerEntry_ParseRouteWithRequiredAuth(t *testing.T) {
	raw := []byte(`{"http":{"method":"POST","path":"/users"},"auth":{"verifier":"jwt","optional":false}}`)

	var sut HandlerEntry
	require.NoError(t, json.Unmarshal(raw, &sut))

	require.NotNil(t, sut.HTTP)
	require.NotNil(t, sut.Auth)
	assert.Equal(t, "jwt", sut.Auth.Verifier)
	assert.False(t, sut.Auth.Optional)
	assert.Nil(t, sut.Verifier)
}

// TestHandlerEntry_ParseRouteWithOptionalAuth covers the optional-auth
// case. An empty verifier id means "use the default verifier", which
// the routing builder resolves at table build time.
func TestHandlerEntry_ParseRouteWithOptionalAuth(t *testing.T) {
	raw := []byte(`{"http":{"method":"GET","path":"/articles/:id"},"auth":{"optional":true}}`)

	var sut HandlerEntry
	require.NoError(t, json.Unmarshal(raw, &sut))

	require.NotNil(t, sut.Auth)
	assert.Equal(t, "", sut.Auth.Verifier, "empty verifier id means default")
	assert.True(t, sut.Auth.Optional)
}

func TestHandlerEntry_ParseVerifierWithDefault(t *testing.T) {
	raw := []byte(`{"verifier":{"id":"jwt","default":true}}`)

	var sut HandlerEntry
	require.NoError(t, json.Unmarshal(raw, &sut))

	assert.Nil(t, sut.HTTP)
	assert.Nil(t, sut.Auth)
	require.NotNil(t, sut.Verifier)
	assert.Equal(t, "jwt", sut.Verifier.ID)
	assert.True(t, sut.Verifier.Default)
}

func TestHandlerEntry_ParseVerifierWithoutDefault(t *testing.T) {
	raw := []byte(`{"verifier":{"id":"session"}}`)

	var sut HandlerEntry
	require.NoError(t, json.Unmarshal(raw, &sut))

	require.NotNil(t, sut.Verifier)
	assert.Equal(t, "session", sut.Verifier.ID)
	assert.False(t, sut.Verifier.Default)
}

// TestHandlerEntry_ParsePureRpc covers handlers that are registered
// in KV (because they use @MessagePattern) but expose no gateway
// surface. They survive the parse and are later silently skipped by
// both the routing builder and the verifier registry.
func TestHandlerEntry_ParsePureRpc(t *testing.T) {
	raw := []byte(`{}`)

	var sut HandlerEntry
	require.NoError(t, json.Unmarshal(raw, &sut))

	assert.Nil(t, sut.HTTP)
	assert.Nil(t, sut.Auth)
	assert.Nil(t, sut.Verifier)
}
