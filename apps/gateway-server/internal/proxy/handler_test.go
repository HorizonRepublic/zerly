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
// under test. Tests assert on .subject to verify call ordering and on
// .payload to inspect the encoded envelope.
type recordedCall struct {
	subject string
	payload []byte
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

func (f *fakeRequester) Request(subject string, payload []byte, _ time.Duration) ([]byte, error) {
	// Record the call with a defensive copy of the payload — the
	// handler pools its encode buffer and will overwrite these bytes
	// before the test assertion runs.
	recorded := recordedCall{subject: subject, payload: append([]byte(nil), payload...)}
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
