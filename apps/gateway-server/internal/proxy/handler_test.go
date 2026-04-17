package proxy

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gerrors "github.com/HorizonRepublic/zerly/apps/gateway-server/internal/errors"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/routing"
)

// fakeTable implements routing.Table for unit tests by keying on
// "METHOD PATH". Kept intentionally minimal — extra behaviour would
// only add indirection to what should be a hermetic test fixture.
type fakeTable struct {
	routes map[string]routing.Route
}

func (f *fakeTable) Lookup(method, path string) (routing.Route, map[string]string, bool) {
	key := method + " " + path
	r, ok := f.routes[key]
	if !ok {
		return routing.Route{}, nil, false
	}
	return r, map[string]string{}, true
}

func (f *fakeTable) Methods(string) []string { return nil }

// recordedCall captures a single NATS request issued by the handler
// under test. Tests assert on .subject to verify call ordering, on
// .payload to inspect the encoded envelope, and on .timeout to verify
// per-route timeout overrides.
type recordedCall struct {
	subject string
	payload []byte
	timeout time.Duration
}

// programmedReply is a canned (reply, err) tuple keyed to a specific
// subject in fakeRequester.programmed.
type programmedReply struct {
	reply []byte
	err   error
}

// fakeRequester implements NatsRequester. Supports two modes:
//
//   - Default: .reply and .err are returned for every subject. This
//     is the shape existing single-subject tests rely on.
//   - Programmed: .program(subject, reply, err) installs a per-subject
//     canned reply consulted first. If a request subject has no
//     programmed entry the default (reply, err) is used.
//
// Every call is appended to .requests so tests can assert the order
// and payloads of NATS requests the handler issued.
type fakeRequester struct {
	reply      []byte
	err        error
	programmed map[string]programmedReply
	requests   []recordedCall
}

func newFakeNats() *fakeRequester {
	return &fakeRequester{programmed: map[string]programmedReply{}}
}

func (f *fakeRequester) program(subject string, reply []byte, err error) {
	if f.programmed == nil {
		f.programmed = map[string]programmedReply{}
	}
	f.programmed[subject] = programmedReply{reply: reply, err: err}
}

func (f *fakeRequester) Request(subject string, payload []byte, timeout time.Duration) ([]byte, error) {
	recorded := recordedCall{
		subject: subject,
		payload: append([]byte(nil), payload...),
		timeout: timeout,
	}
	f.requests = append(f.requests, recorded)

	if p, ok := f.programmed[subject]; ok {
		return p.reply, p.err
	}

	return f.reply, f.err
}

func buildHandler(table routing.Table, reply []byte, err error) *Handler {
	return NewHandler(HandlerConfig{
		Table:   func() routing.Table { return table },
		Nats:    &fakeRequester{reply: reply, err: err},
		Encoder: NewDefaultEncoder(),
		Decoder: NewDefaultDecoder(),
		Timeout: 30 * time.Second,
		Logger:  zerolog.Nop(),
	})
}

func emptyServeInput(method, path string) *ServeInput {
	return &ServeInput{
		Method:    method,
		Path:      path,
		Query:     map[string]QueryValue{},
		Headers:   map[string]string{},
		RequestID: "r1",
	}
}

func TestHandler_HappyPath(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	reply := []byte(`{"status":200,"headers":{},"body":{"ok":true}}`)
	h := buildHandler(table, reply, nil)

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, 200, result.Status)
	assert.Equal(t, []string{"r1"}, result.Headers["x-request-id"])
	assert.Equal(t, []string{"application/json"}, result.Headers["content-type"])
	assert.JSONEq(t, `{"ok":true}`, string(result.Body))
}

func TestHandler_Returns404WhenRouteNotFound(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{}}
	h := buildHandler(table, nil, nil)

	result := h.Handle(emptyServeInput("GET", "/unknown"))

	assert.Equal(t, 404, result.Status)
	assert.Equal(t, gerrors.NotFound.Body, result.Body)
}

func TestHandler_Returns504OnTimeout(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, nil, natsgo.ErrTimeout)

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, 504, result.Status)
	assert.Equal(t, gerrors.GatewayTimeout.Body, result.Body)
}

func TestHandler_Returns503OnNatsError(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, nil, errors.New("connection refused"))

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, 503, result.Status)
	assert.Equal(t, gerrors.ServiceUnavailable.Body, result.Body)
}

func TestHandler_Returns502OnMalformedReply(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, []byte(`not json`), nil)

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, 502, result.Status)
	assert.Equal(t, gerrors.BadGateway.Body, result.Body)
}

func TestHandler_SuccessReplyPreservesStatusAndHeaders(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"POST /users": {Subject: "svc.cmd.users.create", PathTemplate: "/users", Method: "POST"},
	}}
	reply := []byte(`{"status":201,"headers":{"x-custom":["yes"]},"body":{"id":"1"}}`)
	h := buildHandler(table, reply, nil)

	in := emptyServeInput("POST", "/users")
	in.Body = []byte(`{"name":"Alice"}`)

	result := h.Handle(in)

	assert.Equal(t, 201, result.Status)
	assert.Equal(t, []string{"yes"}, result.Headers["x-custom"])
	assert.Equal(
		t,
		[]string{"r1"},
		result.Headers["x-request-id"],
		"x-request-id is always gateway-owned",
	)
	require.NotNil(t, result.Body)
}

// stubTable builds a one-entry routing.Table keyed on the given
// route's method+template. Used by auth-flow tests that only need a
// single path to match.
func stubTable(route routing.Route) routing.Table {
	return &fakeTable{routes: map[string]routing.Route{
		route.Method + " " + route.PathTemplate: route,
	}}
}

// newAuthHandler wires a Handler configured for the auth-flow tests:
// subject-routed fakeRequester, default JSON encoder/decoder, silent
// logger.
func newAuthHandler(table routing.Table, nats *fakeRequester) *Handler {
	return NewHandler(HandlerConfig{
		Table:   func() routing.Table { return table },
		Nats:    nats,
		Encoder: NewDefaultEncoder(),
		Decoder: NewDefaultDecoder(),
		Timeout: time.Second,
		Logger:  zerolog.Nop(),
	})
}

func authServeInput(method, path string) *ServeInput {
	return &ServeInput{
		Method:    method,
		Path:      path,
		Query:     map[string]QueryValue{},
		Headers:   map[string]string{},
		RequestID: "r1",
	}
}

func TestHandler_CallsVerifierBeforeRoute(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.users.me"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth: &routing.RouteAuth{
			VerifierSubject: verifierSubject,
		},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":200,"headers":{},"body":{"userId":"u1","roles":["admin"]}}`),
		nil,
	)
	nats.program(
		routeSubject,
		[]byte(`{"status":200,"headers":{},"body":{"greeting":"hi"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	in := authServeInput("GET", "/users/me")
	in.Headers = map[string]string{"authorization": "Bearer xyz"}

	result := sut.Handle(in)

	require.Equal(t, 200, result.Status)
	assert.Contains(t, string(result.Body), "greeting")

	// Verifier ran first, route ran second, exactly two NATS requests.
	require.Len(t, nats.requests, 2)
	assert.Equal(t, verifierSubject, nats.requests[0].subject)
	assert.Equal(t, routeSubject, nats.requests[1].subject)

	// Main route envelope must carry the claims under auth.
	var routePayload map[string]any
	require.NoError(t, json.Unmarshal(nats.requests[1].payload, &routePayload))
	assert.Equal(t, map[string]any{"userId": "u1", "roles": []any{"admin"}}, routePayload["auth"])
}

func TestHandler_ShortCircuitsOn401FromVerifier(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.users.me"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":401,"headers":{"www-authenticate":["Bearer"]},"body":{"error":"Unauthorized"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(authServeInput("GET", "/users/me"))

	assert.Equal(t, 401, result.Status)
	assert.Equal(t, []string{"Bearer"}, result.Headers["www-authenticate"])

	// Route was NOT called.
	require.Len(t, nats.requests, 1)
	assert.Equal(t, verifierSubject, nats.requests[0].subject)
}

func TestHandler_OptionalAuthContinuesOn401(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.articles.get"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/articles/:id",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject, Optional: true},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":401,"headers":{},"body":{"error":"Unauthorized"}}`),
		nil,
	)
	nats.program(
		routeSubject,
		[]byte(`{"status":200,"headers":{},"body":{"public":true}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	// The fakeTable does not actually parse `:id` — its Lookup keys on
	// literal method+template, so feed the template path through.
	result := sut.Handle(authServeInput("GET", "/articles/:id"))

	assert.Equal(t, 200, result.Status)
	require.Len(t, nats.requests, 2)

	// Main envelope must NOT contain an auth field when claims are nil.
	var routePayload map[string]any
	require.NoError(t, json.Unmarshal(nats.requests[1].payload, &routePayload))
	_, hasAuth := routePayload["auth"]
	assert.False(t, hasAuth)
}

func TestHandler_OptionalAuthStillForwards403(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.articles.get"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/articles/:id",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject, Optional: true},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":403,"headers":{},"body":{"error":"Forbidden"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(authServeInput("GET", "/articles/:id"))

	assert.Equal(t, 403, result.Status)
	require.Len(t, nats.requests, 1, "route was not called")
}

func TestHandler_VerifierNoRespondersReturns503(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.users.me"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject},
	}

	nats := newFakeNats()
	nats.program(verifierSubject, nil, errors.New("nats: no responders available for request"))

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(authServeInput("GET", "/users/me"))

	assert.Equal(t, 503, result.Status)
}

func TestHandler_MergesVerifierAndRouteCookies(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.users.me"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":200,"headers":{"set-cookie":["rotated=new; HttpOnly"]},"body":{"userId":"u1"}}`),
		nil,
	)
	nats.program(
		routeSubject,
		[]byte(`{"status":200,"headers":{"set-cookie":["theme=dark; Path=/"]},"body":{"greeting":"hi"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(authServeInput("GET", "/users/me"))

	require.Equal(t, 200, result.Status)
	// Spec §6.6: verifier values FIRST, route values AFTER.
	assert.Equal(
		t,
		[]string{"rotated=new; HttpOnly", "theme=dark; Path=/"},
		result.Headers["set-cookie"],
	)
}

func TestHandler_VerifierOnlyCookiePassesThrough(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.users.me"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":200,"headers":{"set-cookie":["rotated=new; HttpOnly"]},"body":{"userId":"u1"}}`),
		nil,
	)
	nats.program(
		routeSubject,
		[]byte(`{"status":200,"headers":{},"body":{"greeting":"hi"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(authServeInput("GET", "/users/me"))

	require.Equal(t, 200, result.Status)
	assert.Equal(t, []string{"rotated=new; HttpOnly"}, result.Headers["set-cookie"])
}

func TestHandler_RouteOnlyCookieUnchanged(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.users.me"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":200,"headers":{},"body":{"userId":"u1"}}`),
		nil,
	)
	nats.program(
		routeSubject,
		[]byte(`{"status":200,"headers":{"set-cookie":["theme=dark; Path=/"]},"body":{"greeting":"hi"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(authServeInput("GET", "/users/me"))

	require.Equal(t, 200, result.Status)
	assert.Equal(t, []string{"theme=dark; Path=/"}, result.Headers["set-cookie"])
}

func TestHandler_RouteHeaderWinsOverVerifierForSingleValue(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.users.me"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":200,"headers":{"cache-control":["no-store"]},"body":{"userId":"u1"}}`),
		nil,
	)
	nats.program(
		routeSubject,
		[]byte(`{"status":200,"headers":{"cache-control":["private"]},"body":{"greeting":"hi"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(authServeInput("GET", "/users/me"))

	require.Equal(t, 200, result.Status)
	// Single-value conflict → route reply owns the slot.
	assert.Equal(t, []string{"private"}, result.Headers["cache-control"])
}

func TestHandler_VerifierOnlyHeaderPassesThrough(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.users.me"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":200,"headers":{"x-verifier-trace":["vtrace-abc"]},"body":{"userId":"u1"}}`),
		nil,
	)
	nats.program(
		routeSubject,
		[]byte(`{"status":200,"headers":{"x-route-trace":["rtrace-xyz"]},"body":{"greeting":"hi"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(authServeInput("GET", "/users/me"))

	require.Equal(t, 200, result.Status)
	assert.Equal(t, []string{"vtrace-abc"}, result.Headers["x-verifier-trace"])
	assert.Equal(t, []string{"rtrace-xyz"}, result.Headers["x-route-trace"])
}

func TestHandler_OptionalAuth401DoesNotMergeVerifierHeaders(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.articles.get"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/articles/:id",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject, Optional: true},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":401,"headers":{"x-verifier-trace":["vtrace"],"set-cookie":["leak=bad"]},"body":{}}`),
		nil,
	)
	nats.program(
		routeSubject,
		[]byte(`{"status":200,"headers":{},"body":{"public":true}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(authServeInput("GET", "/articles/:id"))

	require.Equal(t, 200, result.Status)
	_, traceSeen := result.Headers["x-verifier-trace"]
	assert.False(t, traceSeen, "verifier headers on 401 swallow path must not reach the client")
	_, cookieSeen := result.Headers["set-cookie"]
	assert.False(t, cookieSeen, "verifier cookies on 401 swallow path must not reach the client")
}

func TestHandler_GatewayRequestIDBeatsVerifierSpoofing(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.users.me"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject},
	}

	nats := newFakeNats()
	nats.program(
		verifierSubject,
		[]byte(`{"status":200,"headers":{"x-request-id":["forged-by-verifier"]},"body":{"userId":"u1"}}`),
		nil,
	)
	nats.program(
		routeSubject,
		[]byte(`{"status":200,"headers":{},"body":{"greeting":"hi"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	in := authServeInput("GET", "/users/me")
	in.RequestID = "req-0001"

	result := sut.Handle(in)

	require.Equal(t, 200, result.Status)
	assert.Equal(t, []string{"req-0001"}, result.Headers["x-request-id"])
}

func TestHandler_OverwritesUpstreamRequestID(t *testing.T) {
	// Upstream services MUST NOT be able to set x-request-id — the
	// gateway always stamps its own value so request-id tracking
	// cannot be spoofed by a compromised handler.
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	reply := []byte(`{"status":200,"headers":{"x-request-id":["spoofed"]},"body":null}`)
	h := buildHandler(table, reply, nil)

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, []string{"r1"}, result.Headers["x-request-id"])
}

// --- CORS preflight tests ---

func TestHandler_PreflightReturns204WithCORSHeaders(t *testing.T) {
	cors := &registry.CORSMeta{
		Origins:     []string{"https://example.com"},
		Methods:     []string{"GET", "POST"},
		Headers:     []string{"Authorization", "Content-Type"},
		Credentials: true,
		MaxAge:      3600,
	}
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET", CORS: cors,
		},
	}}
	h := buildHandler(table, nil, nil)

	in := emptyServeInput("OPTIONS", "/users")
	in.Headers["origin"] = "https://example.com"
	in.Headers["access-control-request-method"] = "GET"

	result := h.Handle(in)

	assert.Equal(t, 204, result.Status)
	assert.Equal(t, []string{"https://example.com"}, result.Headers["Access-Control-Allow-Origin"])
	assert.Equal(t, []string{"GET, POST"}, result.Headers["Access-Control-Allow-Methods"])
	assert.Equal(t, []string{"Authorization, Content-Type"}, result.Headers["Access-Control-Allow-Headers"])
	assert.Equal(t, []string{"true"}, result.Headers["Access-Control-Allow-Credentials"])
	assert.Equal(t, []string{"3600"}, result.Headers["Access-Control-Max-Age"])
	assert.Nil(t, result.Body)
}

func TestHandler_PreflightReturns404WhenNoCORSConfig(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET",
		},
	}}
	h := buildHandler(table, nil, nil)

	in := emptyServeInput("OPTIONS", "/users")
	in.Headers["origin"] = "https://example.com"
	in.Headers["access-control-request-method"] = "GET"

	result := h.Handle(in)

	assert.Equal(t, 404, result.Status)
}

func TestHandler_PreflightReturns404WithoutACRM(t *testing.T) {
	cors := &registry.CORSMeta{Origins: []string{"*"}}
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET", CORS: cors,
		},
	}}
	h := buildHandler(table, nil, nil)

	in := emptyServeInput("OPTIONS", "/users")
	in.Headers["origin"] = "https://example.com"

	result := h.Handle(in)

	assert.Equal(t, 404, result.Status)
}

func TestHandler_PreflightReturns404OnOriginMismatch(t *testing.T) {
	cors := &registry.CORSMeta{Origins: []string{"https://allowed.com"}}
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET", CORS: cors,
		},
	}}
	h := buildHandler(table, nil, nil)

	in := emptyServeInput("OPTIONS", "/users")
	in.Headers["origin"] = "https://evil.com"
	in.Headers["access-control-request-method"] = "GET"

	result := h.Handle(in)

	assert.Equal(t, 404, result.Status)
}

// --- Rate limiting tests ---

// fakeRateLimiter implements ratelimit.Store for unit tests.
type fakeRateLimiter struct {
	allowed bool
	calls   []rateLimitCall
}

type rateLimitCall struct {
	key   string
	rps   int
	burst int
}

func (f *fakeRateLimiter) Allow(key string, rps int, burst int) bool {
	f.calls = append(f.calls, rateLimitCall{key: key, rps: rps, burst: burst})

	return f.allowed
}

func TestHandler_RateLimitReturns429(t *testing.T) {
	rl := &fakeRateLimiter{allowed: false}
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET",
			RateLimit: &registry.RateLimitMeta{RPS: 10, Burst: 20},
		},
	}}
	nats := newFakeNats()
	nats.reply = []byte(`{"status":200,"headers":{},"body":null}`)

	h := NewHandler(HandlerConfig{
		Table:       func() routing.Table { return table },
		Nats:        nats,
		Encoder:     NewDefaultEncoder(),
		Decoder:     NewDefaultDecoder(),
		Timeout:     30 * time.Second,
		Logger:      zerolog.Nop(),
		RateLimiter: rl,
	})

	in := emptyServeInput("GET", "/users")
	in.RemoteAddr = "1.2.3.4"

	result := h.Handle(in)

	assert.Equal(t, 429, result.Status)
	assert.Equal(t, gerrors.TooManyRequests.Body, result.Body)
	assert.Equal(t, []string{"1"}, result.Headers["retry-after"])

	require.Len(t, rl.calls, 1)
	assert.Equal(t, "GET:/users:1.2.3.4", rl.calls[0].key)
	assert.Equal(t, 10, rl.calls[0].rps)
	assert.Equal(t, 20, rl.calls[0].burst)

	assert.Empty(t, nats.requests, "NATS must not be called when rate-limited")
}

func TestHandler_RateLimitDefaultBurstIs2xRPS(t *testing.T) {
	rl := &fakeRateLimiter{allowed: false}
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET",
			RateLimit: &registry.RateLimitMeta{RPS: 5},
		},
	}}

	h := NewHandler(HandlerConfig{
		Table:       func() routing.Table { return table },
		Nats:        newFakeNats(),
		Encoder:     NewDefaultEncoder(),
		Decoder:     NewDefaultDecoder(),
		Timeout:     30 * time.Second,
		Logger:      zerolog.Nop(),
		RateLimiter: rl,
	})

	h.Handle(emptyServeInput("GET", "/users"))

	require.Len(t, rl.calls, 1)
	assert.Equal(t, 10, rl.calls[0].burst, "default burst = 2 * RPS")
}

func TestHandler_RateLimitSkippedWhenNoStore(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET",
			RateLimit: &registry.RateLimitMeta{RPS: 10},
		},
	}}
	reply := []byte(`{"status":200,"headers":{},"body":null}`)
	h := buildHandler(table, reply, nil)

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, 200, result.Status, "request proceeds when RateLimiter is nil")
}

// --- Per-route timeout tests ---

func TestHandler_PerRouteTimeoutOverridesGlobal(t *testing.T) {
	routeTimeout := 5 * time.Second
	globalTimeout := 30 * time.Second

	nats := newFakeNats()
	nats.reply = []byte(`{"status":200,"headers":{},"body":null}`)

	table := &fakeTable{routes: map[string]routing.Route{
		"GET /slow": {
			Subject: "svc.cmd.slow", PathTemplate: "/slow",
			Method: "GET", Timeout: routeTimeout,
		},
	}}

	h := NewHandler(HandlerConfig{
		Table:   func() routing.Table { return table },
		Nats:    nats,
		Encoder: NewDefaultEncoder(),
		Decoder: NewDefaultDecoder(),
		Timeout: globalTimeout,
		Logger:  zerolog.Nop(),
	})

	result := h.Handle(emptyServeInput("GET", "/slow"))

	assert.Equal(t, 200, result.Status)
	require.Len(t, nats.requests, 1)
	assert.Equal(t, routeTimeout, nats.requests[0].timeout)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(nats.requests[0].payload, &payload))
	meta, ok := payload["meta"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(routeTimeout.Milliseconds()), meta["timeoutMs"])
}

func TestHandler_ZeroRouteTimeoutUsesGlobal(t *testing.T) {
	globalTimeout := 30 * time.Second

	nats := newFakeNats()
	nats.reply = []byte(`{"status":200,"headers":{},"body":null}`)

	table := &fakeTable{routes: map[string]routing.Route{
		"GET /fast": {
			Subject: "svc.cmd.fast", PathTemplate: "/fast",
			Method: "GET",
		},
	}}

	h := NewHandler(HandlerConfig{
		Table:   func() routing.Table { return table },
		Nats:    nats,
		Encoder: NewDefaultEncoder(),
		Decoder: NewDefaultDecoder(),
		Timeout: globalTimeout,
		Logger:  zerolog.Nop(),
	})

	result := h.Handle(emptyServeInput("GET", "/fast"))

	assert.Equal(t, 200, result.Status)
	require.Len(t, nats.requests, 1)
	assert.Equal(t, globalTimeout, nats.requests[0].timeout)
}

// --- Static headers tests ---

func TestHandler_StaticRouteHeadersAppearOnResponse(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET",
			Headers: map[string]string{
				"x-custom-header": "static-value",
				"cache-control":   "public, max-age=60",
			},
		},
	}}
	reply := []byte(`{"status":200,"headers":{},"body":null}`)
	h := buildHandler(table, reply, nil)

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, 200, result.Status)
	assert.Equal(t, []string{"static-value"}, result.Headers["x-custom-header"])
	assert.Equal(t, []string{"public, max-age=60"}, result.Headers["cache-control"])
}

func TestHandler_EnvelopeHeadersOverrideStaticHeaders(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET",
			Headers: map[string]string{
				"cache-control": "public, max-age=60",
				"x-fallback":    "from-config",
			},
		},
	}}
	reply := []byte(`{"status":200,"headers":{"cache-control":["no-store"]},"body":null}`)
	h := buildHandler(table, reply, nil)

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, 200, result.Status)
	assert.Equal(t, []string{"no-store"}, result.Headers["cache-control"],
		"envelope header wins over static header")
	assert.Equal(t, []string{"from-config"}, result.Headers["x-fallback"],
		"static header applied when no conflict")
}

// --- CORS response headers on non-OPTIONS requests ---

func TestHandler_CORSResponseHeadersOnNonOptions(t *testing.T) {
	cors := &registry.CORSMeta{
		Origins:     []string{"https://example.com"},
		Credentials: true,
	}
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET", CORS: cors,
		},
	}}
	reply := []byte(`{"status":200,"headers":{},"body":null}`)
	h := buildHandler(table, reply, nil)

	in := emptyServeInput("GET", "/users")
	in.Headers["origin"] = "https://example.com"

	result := h.Handle(in)

	assert.Equal(t, 200, result.Status)
	assert.Equal(t, []string{"https://example.com"}, result.Headers["Access-Control-Allow-Origin"])
	assert.Equal(t, []string{"true"}, result.Headers["Access-Control-Allow-Credentials"])
	assert.Equal(t, []string{"Origin"}, result.Headers["Vary"])
}

func TestHandler_CORSResponseHeadersOmittedOnOriginMismatch(t *testing.T) {
	cors := &registry.CORSMeta{Origins: []string{"https://allowed.com"}}
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET", CORS: cors,
		},
	}}
	reply := []byte(`{"status":200,"headers":{},"body":null}`)
	h := buildHandler(table, reply, nil)

	in := emptyServeInput("GET", "/users")
	in.Headers["origin"] = "https://evil.com"

	result := h.Handle(in)

	assert.Equal(t, 200, result.Status)
	_, hasCORS := result.Headers["Access-Control-Allow-Origin"]
	assert.False(t, hasCORS, "CORS headers must not appear when origin does not match")
}

func TestHandler_CORSResponseHeadersOmittedWhenNoCORSConfig(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET",
		},
	}}
	reply := []byte(`{"status":200,"headers":{},"body":null}`)
	h := buildHandler(table, reply, nil)

	in := emptyServeInput("GET", "/users")
	in.Headers["origin"] = "https://example.com"

	result := h.Handle(in)

	assert.Equal(t, 200, result.Status)
	_, hasCORS := result.Headers["Access-Control-Allow-Origin"]
	assert.False(t, hasCORS)
}

// --- Rate limit keyBy integration tests ---

// oncePerKeyLimiter allows exactly one request per unique key, then
// denies all subsequent requests for the same key. This models the
// "second request is rate-limited" scenario without coupling to a
// real token-bucket implementation.
type oncePerKeyLimiter struct {
	seen  map[string]bool
	calls []rateLimitCall
}

func newOncePerKeyLimiter() *oncePerKeyLimiter {
	return &oncePerKeyLimiter{seen: map[string]bool{}}
}

func (o *oncePerKeyLimiter) Allow(key string, rps int, burst int) bool {
	o.calls = append(o.calls, rateLimitCall{key: key, rps: rps, burst: burst})
	if o.seen[key] {
		return false
	}

	o.seen[key] = true

	return true
}

func TestHandler_RateLimitKeyByHeader(t *testing.T) {
	rl := newOncePerKeyLimiter()
	nats := newFakeNats()
	nats.reply = []byte(`{"status":200,"headers":{},"body":null}`)

	table := &fakeTable{routes: map[string]routing.Route{
		"GET /api": {
			Subject: "svc.cmd.api", PathTemplate: "/api",
			Method: "GET",
			RateLimit: &registry.RateLimitMeta{
				RPS: 10, Burst: 20,
				KeyBy: []string{"header:x-api-key", "ip"},
			},
		},
	}}

	h := NewHandler(HandlerConfig{
		Table:       func() routing.Table { return table },
		Nats:        nats,
		Encoder:     NewDefaultEncoder(),
		Decoder:     NewDefaultDecoder(),
		Timeout:     30 * time.Second,
		Logger:      zerolog.Nop(),
		RateLimiter: rl,
	})

	// First request: IP=1.1.1.1, x-api-key=shared-key → allowed
	in1 := emptyServeInput("GET", "/api")
	in1.RemoteAddr = "1.1.1.1"
	in1.Headers["x-api-key"] = "shared-key"

	r1 := h.Handle(in1)

	assert.Equal(t, 200, r1.Status)

	// Second request: different IP, same x-api-key → rate-limited
	// because keyBy resolves on header:x-api-key before falling back
	// to ip.
	in2 := emptyServeInput("GET", "/api")
	in2.RemoteAddr = "2.2.2.2"
	in2.Headers["x-api-key"] = "shared-key"

	r2 := h.Handle(in2)

	assert.Equal(t, 429, r2.Status)
	require.Len(t, rl.calls, 2)
	assert.Equal(t, "GET:/api:shared-key", rl.calls[0].key)
	assert.Equal(t, "GET:/api:shared-key", rl.calls[1].key,
		"both requests keyed on header value, not IP")
}

func TestHandler_RateLimitKeyByUserField(t *testing.T) {
	rl := newOncePerKeyLimiter()
	nats := newFakeNats()

	verifierSubject := "auth-svc__microservice.cmd.auth.verifier.jwt"
	routeSubject := "users-svc__microservice.cmd.users.me"

	nats.program(
		verifierSubject,
		[]byte(`{"status":200,"headers":{},"body":{"id":"user-123","email":"u@test.com"}}`),
		nil,
	)
	nats.program(
		routeSubject,
		[]byte(`{"status":200,"headers":{},"body":{"greeting":"hi"}}`),
		nil,
	)

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth: &routing.RouteAuth{
			VerifierSubject: verifierSubject,
		},
		RateLimit: &registry.RateLimitMeta{
			RPS: 5, Burst: 10,
			KeyBy: []string{"user:id", "ip"},
		},
	}

	h := NewHandler(HandlerConfig{
		Table:       func() routing.Table { return stubTable(route) },
		Nats:        nats,
		Encoder:     NewDefaultEncoder(),
		Decoder:     NewDefaultDecoder(),
		Timeout:     30 * time.Second,
		Logger:      zerolog.Nop(),
		RateLimiter: rl,
	})

	// First request: IP=10.0.0.1, user:id=user-123 → allowed
	in1 := authServeInput("GET", "/users/me")
	in1.RemoteAddr = "10.0.0.1"
	in1.Headers["authorization"] = "Bearer tok1"

	r1 := h.Handle(in1)

	assert.Equal(t, 200, r1.Status)

	// Second request: different IP, same user:id → rate-limited
	in2 := authServeInput("GET", "/users/me")
	in2.RemoteAddr = "10.0.0.2"
	in2.Headers["authorization"] = "Bearer tok2"

	r2 := h.Handle(in2)

	assert.Equal(t, 429, r2.Status)
	require.Len(t, rl.calls, 2)
	assert.Equal(t, "GET:/users/me:user-123", rl.calls[0].key)
	assert.Equal(t, "GET:/users/me:user-123", rl.calls[1].key,
		"both requests keyed on user:id, not IP")
}

func TestHandler_RateLimitKeyByCookie(t *testing.T) {
	rl := newOncePerKeyLimiter()
	nats := newFakeNats()
	nats.reply = []byte(`{"status":200,"headers":{},"body":null}`)

	table := &fakeTable{routes: map[string]routing.Route{
		"GET /dashboard": {
			Subject: "svc.cmd.dashboard", PathTemplate: "/dashboard",
			Method: "GET",
			RateLimit: &registry.RateLimitMeta{
				RPS: 10, Burst: 20,
				KeyBy: []string{"cookie:session", "ip"},
			},
		},
	}}

	h := NewHandler(HandlerConfig{
		Table:       func() routing.Table { return table },
		Nats:        nats,
		Encoder:     NewDefaultEncoder(),
		Decoder:     NewDefaultDecoder(),
		Timeout:     30 * time.Second,
		Logger:      zerolog.Nop(),
		RateLimiter: rl,
	})

	// First request with session cookie
	in1 := emptyServeInput("GET", "/dashboard")
	in1.RemoteAddr = "3.3.3.3"
	in1.Headers["cookie"] = "session=abc; theme=dark"

	r1 := h.Handle(in1)

	assert.Equal(t, 200, r1.Status)

	// Second request from a different IP but same session cookie
	in2 := emptyServeInput("GET", "/dashboard")
	in2.RemoteAddr = "4.4.4.4"
	in2.Headers["cookie"] = "session=abc"

	r2 := h.Handle(in2)

	assert.Equal(t, 429, r2.Status)
	require.Len(t, rl.calls, 2)
	assert.Equal(t, "GET:/dashboard:abc", rl.calls[0].key)
	assert.Equal(t, "GET:/dashboard:abc", rl.calls[1].key,
		"both requests keyed on cookie value, not IP")
}

func TestHandler_RateLimitKeyByFallsBackToIP(t *testing.T) {
	rl := newOncePerKeyLimiter()
	nats := newFakeNats()
	nats.reply = []byte(`{"status":200,"headers":{},"body":null}`)

	table := &fakeTable{routes: map[string]routing.Route{
		"GET /api": {
			Subject: "svc.cmd.api", PathTemplate: "/api",
			Method: "GET",
			RateLimit: &registry.RateLimitMeta{
				RPS: 10, Burst: 20,
				KeyBy: []string{"header:x-api-key"},
			},
		},
	}}

	h := NewHandler(HandlerConfig{
		Table:       func() routing.Table { return table },
		Nats:        nats,
		Encoder:     NewDefaultEncoder(),
		Decoder:     NewDefaultDecoder(),
		Timeout:     30 * time.Second,
		Logger:      zerolog.Nop(),
		RateLimiter: rl,
	})

	// Request WITHOUT x-api-key header → falls back to IP
	in := emptyServeInput("GET", "/api")
	in.RemoteAddr = "5.5.5.5"

	result := h.Handle(in)

	assert.Equal(t, 200, result.Status)
	require.Len(t, rl.calls, 1)
	assert.Equal(t, "GET:/api:5.5.5.5", rl.calls[0].key,
		"falls back to IP when header:x-api-key is absent")
}

// --- CORS edge case tests ---

func TestHandler_CORSResponseOmittedWhenNoOriginHeader(t *testing.T) {
	cors := &registry.CORSMeta{
		Origins:     []string{"https://app.example.com"},
		Credentials: true,
		MaxAge:      3600,
	}
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET", CORS: cors,
		},
	}}
	reply := []byte(`{"status":200,"headers":{},"body":{"ok":true}}`)
	h := buildHandler(table, reply, nil)

	// Server-to-server call: no Origin header
	in := emptyServeInput("GET", "/users")

	result := h.Handle(in)

	assert.Equal(t, 200, result.Status)
	assert.JSONEq(t, `{"ok":true}`, string(result.Body))
	_, hasCORS := result.Headers["Access-Control-Allow-Origin"]
	assert.False(t, hasCORS, "no CORS headers when Origin header is absent")
	_, hasVary := result.Headers["Vary"]
	assert.False(t, hasVary, "no Vary header when Origin header is absent")
}

func TestHandler_PreflightOnRouteWithoutCORSConfig(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {
			Subject: "svc.cmd.users.list", PathTemplate: "/users",
			Method: "GET",
		},
	}}
	h := buildHandler(table, nil, nil)

	in := emptyServeInput("OPTIONS", "/users")
	in.Headers["origin"] = "https://example.com"
	in.Headers["access-control-request-method"] = "GET"

	result := h.Handle(in)

	assert.Equal(t, 404, result.Status,
		"preflight on a route without CORS config returns 404")
}
