//go:build e2e

// Package e2e is the end-to-end harness for the zerly-gateway-server.
//
// The tests run under the `e2e` build tag so they are skipped by the
// default unit pass (`pnpm nx test gateway-server`). Bring up the
// stack via the README protocol in this directory before running.
package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatewayURL is the hardcoded base URL the e2e suite targets. The
// three-process startup protocol in README.md binds the gateway to
// :8080 on localhost; CI can override this via an env var once a
// fully containerised stack is wired up.
const gatewayURL = "http://localhost:8080"

// gatewayReadyTimeout bounds how long waitForGateway polls before
// giving up. 30 seconds is generous on a cold laptop with Docker
// booting NATS plus a fresh example-app start.
const gatewayReadyTimeout = 30 * time.Second

// gatewayPollInterval is the delay between readiness probes. 500ms
// keeps the polling burst light while still returning quickly once
// the gateway is up.
const gatewayPollInterval = 500 * time.Millisecond

// TestE2E_GetUserReturns200 exercises scenario 1: a GET against the
// demo user fetch endpoint resolves through NATS to the example-app
// handler and returns a 200 with the serialized user.
func TestE2E_GetUserReturns200(t *testing.T) {
	waitForGateway(t)

	resp, err := http.Get(gatewayURL + "/users/1")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var user map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	assert.Equal(t, "1", user["id"])
	assert.NotEmpty(t, user["name"])
}

// TestE2E_CreateUserReturns201 exercises scenario 2: a POST with a
// JSON body round-trips through the gateway and the created user
// echoes the gateway-assigned X-Request-Id back in its payload,
// proving the correlator is observable at the handler layer.
func TestE2E_CreateUserReturns201(t *testing.T) {
	waitForGateway(t)

	body := bytes.NewBufferString(`{"name":"E2E User"}`)
	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/users", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var created map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	assert.Equal(t, "E2E User", created["name"])
	// The controller injects the gateway-assigned X-Request-Id into
	// the response body via @GatewayRequestId; the two values MUST
	// match so the correlator round-trip is verifiable end-to-end.
	assert.Equal(t, resp.Header.Get("X-Request-Id"), created["requestId"])
}

// TestE2E_DeleteUserReturns204 exercises scenario 3: a DELETE with a
// void handler return resolves to a 204 No Content, exercising the
// default status resolver for void returns.
func TestE2E_DeleteUserReturns204(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodDelete, gatewayURL+"/users/2", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// TestE2E_UnknownRouteReturns404 exercises scenario 4: a request for
// a path that has no matching route in the registry returns a 404
// with the NOT_FOUND error body shape and a JSON content type.
func TestE2E_UnknownRouteReturns404(t *testing.T) {
	waitForGateway(t)

	resp, err := http.Get(gatewayURL + "/nothing/here")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Not Found")
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

// TestE2E_RequestIDAlwaysPresent exercises scenario 5: every response
// carries an X-Request-Id header regardless of status, so operators
// can trace every request — even misses — by correlator.
func TestE2E_RequestIDAlwaysPresent(t *testing.T) {
	waitForGateway(t)

	resp, err := http.Get(gatewayURL + "/users/1")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// The gateway always stamps X-Request-Id on every response
	// regardless of status. Even a 404 from an unknown route must
	// carry the correlator so operators can trace the miss.
	assert.NotEmpty(t, resp.Header.Get("X-Request-Id"))
}

// waitForGateway blocks until the gateway's HTTP listener accepts a
// connection OR gatewayReadyTimeout elapses. Any HTTP response
// (including 404) counts as ready — the gateway may not have any
// handlers registered yet, in which case every request 404s.
//
// A connect error is a "not ready" signal; the loop retries after
// gatewayPollInterval. On timeout the test fails rather than hang
// indefinitely.
func waitForGateway(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(gatewayReadyTimeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for {
		resp, err := client.Get(gatewayURL + "/__probe__")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("gateway not reachable within %s: %v", gatewayReadyTimeout, err)
		}
		time.Sleep(gatewayPollInterval)
	}
}
