//go:build e2e

// Package e2e — response-builder contract scenarios. This file is
// the sibling of e2e_test.go and auth_test.go and reuses their
// `waitForGateway` helper plus the shared `gatewayURL` constant.
// Spin the stack up per the README protocol in this directory
// before running `go test -tags=e2e`.
//
// These tests pin the observable wire behaviour of
// `@GatewayResponse()` — cookies, status overrides, redirects, and
// verifier-side header merges — against the live gateway-server so
// regressions in the envelope encoder or the Go-side mergeAuthHeaders
// path surface immediately.
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noRedirectClient returns an http.Client that refuses to follow
// redirects, surfacing the raw 302 to the caller so we can assert
// on the Location header and status code directly. The stdlib
// default client transparently follows up to 10 redirects, which
// would turn a 302-to-Google into an unrelated DNS-dependent
// failure on the test host.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// findCookie returns the first cookie on resp whose Name matches
// the supplied name, or nil if none is present. The stdlib
// `resp.Cookies()` parses every `Set-Cookie` header on the
// response into a typed `*http.Cookie`, which is the right level
// to assert on — we care about name/value/flags, not byte-for-byte
// header formatting.
func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}

	return nil
}

// TestE2E_Response_LoginSetsCookieAnd201 exercises the canonical
// login-with-cookie shape: `POST /auth/login` returns HTTP 201
// (overridden from the default 200 via `res.status(201)`), sets a
// `sid` cookie whose value is derived from the submitted name, and
// returns the synthesised user payload in the body. The cookie
// flags (HttpOnly, SameSite, Path, Max-Age) are part of the
// observable contract — a session cookie that loses HttpOnly is a
// security regression we want to catch immediately.
func TestE2E_Response_LoginSetsCookieAnd201(t *testing.T) {
	waitForGateway(t)

	body := strings.NewReader(`{"name":"probe"}`)
	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/auth/login", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	sid := findCookie(resp, "sid")
	require.NotNil(t, sid, "login must emit a `sid` cookie")
	assert.Equal(t, "demo-probe", sid.Value)
	assert.True(t, sid.HttpOnly, "session cookie must be HttpOnly")
	assert.Equal(t, http.SameSiteLaxMode, sid.SameSite)
	assert.Equal(t, "/", sid.Path)
	assert.Equal(t, 3600, sid.MaxAge)

	var user map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	assert.Equal(t, "user-probe", user["id"])
	assert.Equal(t, "probe@example.test", user["email"])

	roles, ok := user["roles"].([]any)
	require.True(t, ok, "roles must be an array")
	assert.Equal(t, []any{"user"}, roles)
}

// TestE2E_Response_LoginAdminProducesAdminRoles pins the second
// branch of the login handler: the literal name `admin` produces
// an `['admin', 'user']` roles array, mirroring the verifier's
// admin branch. Serves as a regression for any accidental
// flattening of readonly tuple types across the envelope encoder.
func TestE2E_Response_LoginAdminProducesAdminRoles(t *testing.T) {
	waitForGateway(t)

	body := strings.NewReader(`{"name":"admin"}`)
	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/auth/login", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	sid := findCookie(resp, "sid")
	require.NotNil(t, sid)
	assert.Equal(t, "demo-admin", sid.Value)

	var user map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	assert.Equal(t, "user-admin", user["id"])

	roles, ok := user["roles"].([]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []any{"admin", "user"}, roles)
}

// TestE2E_Response_LogoutClearsCookie pins the `clearCookie`
// contract: `POST /auth/logout` emits a `Set-Cookie` whose
// `Max-Age` is zero (or negative, per RFC 6265 §4.1.2.2 — the
// stdlib normalises both to `MaxAge < 0` for "delete now"). The
// body is a trivial `{ ok: true }` success envelope, and the
// default 200 status is preserved because the handler does not
// override it.
func TestE2E_Response_LogoutClearsCookie(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/auth/logout", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	sid := findCookie(resp, "sid")
	require.NotNil(t, sid, "logout must emit a deletion `sid` cookie")
	// The stdlib collapses both `Max-Age=0` and any past `Expires`
	// into `MaxAge < 0` on the parsed cookie. Either signal is a
	// valid "delete me" per RFC 6265; we accept either so the
	// test is resilient to cookie serialization tweaks in the
	// envelope encoder.
	assert.True(t, sid.MaxAge < 0 || !sid.Expires.IsZero(),
		"clearCookie must emit Max-Age<=0 or a past Expires")
	assert.Equal(t, "/", sid.Path)

	var envelope map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
	assert.Equal(t, true, envelope["ok"])
}

// TestE2E_Response_GoogleRedirectReturns302WithLocation pins the
// OAuth2 start-redirect contract: `GET /auth/google/start` returns
// HTTP 302 with a `Location` header pointing at the canned
// Google authorize URL. The handler returns JavaScript `null`,
// which the envelope encoder serialises as the JSON token `null`
// — browsers ignore the body on a 302 regardless, so the exact
// payload is observable but semantically inert. We use a
// no-redirect client so the 302 surfaces verbatim instead of
// following through to Google.
func TestE2E_Response_GoogleRedirectReturns302WithLocation(t *testing.T) {
	waitForGateway(t)

	client := noRedirectClient()

	resp, err := client.Get(gatewayURL + "/auth/google/start")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t,
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=demo&response_type=code&scope=openid",
		resp.Header.Get("Location"),
	)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	// Handler `return null` serialises to the JSON token `null`;
	// the 302 Location header carries the actionable semantics
	// and any browser will ignore the body.
	assert.Equal(t, "null", strings.TrimSpace(string(body)))
}

// TestE2E_Response_VerifierRotatesCookieOnMe pins the verifier-
// side cookie merge contract from the Phase E design: a verifier
// that mutates `@GatewayResponse()` during its `verify` call MUST
// have those mutations merged into the final HTTP response, even
// though the route handler itself never touches the response
// builder. This is the "session rotation" use case —
// `Bearer demo-rotate-probe` on `/me` returns the standard user
// claims in the body AND a freshly rotated `sid` cookie whose
// value encodes the rotation.
func TestE2E_Response_VerifierRotatesCookieOnMe(t *testing.T) {
	waitForGateway(t)

	req, err := http.NewRequest(http.MethodGet, gatewayURL+"/me", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer demo-rotate-probe")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	sid := findCookie(resp, "sid")
	require.NotNil(t, sid, "verifier-side rotation must reach the client")
	assert.Equal(t, "demo-rotate-probe-rotated", sid.Value)
	assert.True(t, sid.HttpOnly)
	assert.Equal(t, "/", sid.Path)

	var user map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	assert.Equal(t, "user-rotate-probe", user["id"])
	assert.Equal(t, "rotate-probe@example.test", user["email"])
}

// TestE2E_Response_LoginCookieNotSecureInLocalDev pins the local-
// dev boundary of the cookie security contract: the example-app
// deliberately sets `secure: false` on the login cookie because
// it runs on plain HTTP. This test makes that boundary visible —
// if someone ever flips the demo to `secure: true`, this test
// catches it and forces a conscious documentation update rather
// than silently breaking local dev.
//
// Production consumers MUST invert this and assert Secure is
// true; the test name is intentionally explicit about which
// environment it pins.
func TestE2E_Response_LoginCookieNotSecureInLocalDev(t *testing.T) {
	waitForGateway(t)

	body := strings.NewReader(`{"name":"probe"}`)
	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/auth/login", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	sid := findCookie(resp, "sid")
	require.NotNil(t, sid)
	assert.False(t, sid.Secure,
		"local-dev example-app login cookie must not carry Secure; flip to true for prod deployments")
}
