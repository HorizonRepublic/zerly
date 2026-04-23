//go:build e2e

// Package e2e — auth-contract scenarios. This file is the sibling of
// e2e_test.go and reuses its `waitForGateway` helper plus the shared
// `gatewayURL` constant. Spin the stack up per the README protocol in
// this directory before running `go test -tags=e2e`.
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_Auth_HappyPath_ValidBearerReturns200WithClaims exercises
// the core success path: a valid `Bearer demo-alice` token reaches
// the default verifier, which synthesises a demo user and returns
// it verbatim. The gateway forwards the claims into the main
// envelope under `auth`, the route handler echoes them back via
// `@GatewayUser()`, and the client sees a 200 with the expected
// user shape.
func TestE2E_Auth_HappyPath_ValidBearerReturns200WithClaims(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodGet, gatewayURL+"/me", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer demo-alice")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var user map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	assert.Equal(t, "user-alice", user["id"])
	assert.Equal(t, "alice@example.test", user["email"])

	roles, ok := user["roles"].([]any)
	require.True(t, ok, "roles must be an array")
	assert.Equal(t, []any{"user"}, roles)
}

// TestE2E_Auth_AdminTokenProducesAdminRoles pins the second branch
// of the demo verifier: `demo-admin` gets both `admin` and `user`
// roles. Serves as a regression for any accidental flattening of
// readonly roles arrays across the NATS wire.
func TestE2E_Auth_AdminTokenProducesAdminRoles(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodGet, gatewayURL+"/me", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer demo-admin")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var user map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))

	roles, ok := user["roles"].([]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []any{"admin", "user"}, roles)
}

// TestE2E_Auth_MissingCredentialReturns401 covers the most common
// rejection: no Authorization header at all. The verifier throws
// UnauthorizedException, the gateway forwards the 401 verbatim
// (including the Nest-default body shape), and the client sees
// the expected status.
func TestE2E_Auth_MissingCredentialReturns401(t *testing.T) {
	waitForGateway(t)

	resp, err := http.Get(gatewayURL + "/me")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	// The verifier's `UnauthorizedException('Missing or invalid bearer token')`
	// serializes through Nest's default filter and GatewayExceptionFilter;
	// the rendered body contains that exact phrase.
	assert.Contains(t, string(body), "Missing or invalid bearer token")
}

// TestE2E_Auth_InvalidCredentialReturns401 covers the "credential
// present but not recognised" branch: anything that does not start
// with `demo-` triggers UnauthorizedException on the verifier side.
func TestE2E_Auth_InvalidCredentialReturns401(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodGet, gatewayURL+"/me", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer nope")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestE2E_Auth_BannedUserReturns403 pins the 401/403 semantic
// separation: a valid `demo-*` token whose name is `banned` has
// a recognisable identity — the verifier returns ForbiddenException
// instead of UnauthorizedException, and the gateway forwards 403.
// This is the "I know you, you can't do this" path.
func TestE2E_Auth_BannedUserReturns403(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodGet, gatewayURL+"/me", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer demo-banned")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Account suspended")
}

// TestE2E_OptionalAuth_AnonymousProceeds covers the optional-auth
// anonymous path: the client presents no credential, the verifier
// throws 401, and the gateway proceeds to the main handler with
// `user = undefined`. The handler returns baseline content with
// `viewer: null`.
func TestE2E_OptionalAuth_AnonymousProceeds(t *testing.T) {
	waitForGateway(t)

	resp, err := http.Get(gatewayURL + "/articles/42")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var article map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&article))
	assert.Equal(t, "42", article["id"])
	assert.Nil(t, article["viewer"], "anonymous optional-auth → null viewer")
}

// TestE2E_OptionalAuth_AuthenticatedEnrichesResponse covers the
// flip side: a valid bearer token on the same optional-auth route
// flows through to the handler and surfaces the caller identity
// under `viewer`, proving the happy-path claims injection works
// identically whether auth is required or optional.
func TestE2E_OptionalAuth_AuthenticatedEnrichesResponse(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodGet, gatewayURL+"/articles/42", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer demo-alice")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var article map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&article))

	viewer, ok := article["viewer"].(map[string]any)
	require.True(t, ok, "authenticated optional-auth → populated viewer")
	assert.Equal(t, "user-alice", viewer["id"])
}

// TestE2E_OptionalAuth_BannedUserStillForwards403 pins the core
// optional-auth invariant: it swallows 401 but never 403. A banned
// user on an optional-auth route must still see 403 — otherwise
// the handler would be asked to render content for someone
// explicitly denied access.
func TestE2E_OptionalAuth_BannedUserStillForwards403(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodGet, gatewayURL+"/articles/42", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer demo-banned")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}
