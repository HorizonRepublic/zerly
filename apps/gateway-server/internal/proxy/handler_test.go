package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gerrors "github.com/HorizonRepublic/zerly/apps/gateway-server/internal/errors"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/ratelimit"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/routing"
)

// fakeTable implements routing.Table for unit tests by keying on
// "METHOD PATH". Kept intentionally minimal — extra behaviour would
// only add indirection to what should be a hermetic test fixture.
//
// methodsByPath lets tests that exercise the 405 Method-Not-Allowed
// surface pre-seed the per-path verb set without populating routes
// for every verb under test. When nil (the common case) Methods
// returns nil — the Lookup-miss path produces 404.
type fakeTable struct {
	routes        map[string]routing.Route
	methodsByPath map[string][]string
}

func (f *fakeTable) Lookup(method, path string) (routing.Route, map[string]string, bool) {
	key := method + " " + path
	r, ok := f.routes[key]
	if !ok {
		return routing.Route{}, nil, false
	}
	return r, map[string]string{}, true
}

func (f *fakeTable) Methods(path string) []string {
	if f.methodsByPath == nil {
		return nil
	}

	return f.methodsByPath[path]
}

// recordedCall captures a single NATS request issued by the handler
// under test. Tests assert on .subject to verify call ordering, on
// .payload to inspect the encoded envelope, on .timeout to verify
// per-route timeout overrides, and on .ctx to verify the inbound
// HTTP request context propagates into the NATS layer.
type recordedCall struct {
	ctx     context.Context
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

func (f *fakeRequester) Request(
	ctx context.Context,
	subject string,
	payload []byte,
	timeout time.Duration,
) ([]byte, error) {
	recorded := recordedCall{
		ctx:     ctx,
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

	result := h.Handle(context.Background(),emptyServeInput("GET", "/users"))

	assert.Equal(t, 200, result.Status)
	assert.Equal(t, []string{"r1"}, result.Headers["x-request-id"])
	assert.Equal(t, []string{"application/json"}, result.Headers["content-type"])
	assert.JSONEq(t, `{"ok":true}`, string(result.Body))
}

func TestHandler_Returns404WhenRouteNotFound(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{}}
	h := buildHandler(table, nil, nil)

	result := h.Handle(context.Background(),emptyServeInput("GET", "/unknown"))

	assert.Equal(t, 404, result.Status)
	assert.Equal(t, gerrors.NotFound.Body, result.Body)
}

func TestHandler_Returns405WithAllowHeaderWhenMethodMismatch(t *testing.T) {
	// Path is registered under GET and POST but the incoming request is
	// DELETE — RFC 9110 §15.5.6 requires a 405 with an Allow header
	// listing the supported verbs.
	table := &fakeTable{
		routes: map[string]routing.Route{
			"GET /users":  {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
			"POST /users": {Subject: "svc.cmd.users.create", PathTemplate: "/users", Method: "POST"},
		},
		methodsByPath: map[string][]string{
			"/users": {"GET", "POST"},
		},
	}
	h := buildHandler(table, nil, nil)

	result := h.Handle(context.Background(),emptyServeInput("DELETE", "/users"))

	assert.Equal(t, 405, result.Status)
	assert.Equal(t, gerrors.MethodNotAllowed.Body, result.Body)
	assert.Equal(t, []string{"GET, POST"}, result.Headers["Allow"],
		"RFC 9110 §15.5.6 requires Allow header enumerating supported verbs")
}

func TestHandler_Returns404WhenPathUnknownEvenOnMethodMismatch(t *testing.T) {
	// Path has no registered verbs and no Methods entry — must 404,
	// not 405 (405 would leak that some path exists under a verb the
	// client did not try, giving a probing signal for nothing).
	table := &fakeTable{
		routes:        map[string]routing.Route{},
		methodsByPath: map[string][]string{},
	}
	h := buildHandler(table, nil, nil)

	result := h.Handle(context.Background(),emptyServeInput("DELETE", "/does-not-exist"))

	assert.Equal(t, 404, result.Status)
	assert.Equal(t, gerrors.NotFound.Body, result.Body)
}

func TestHandler_Returns504OnTimeout(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, nil, natsgo.ErrTimeout)

	result := h.Handle(context.Background(),emptyServeInput("GET", "/users"))

	assert.Equal(t, 504, result.Status)
	assert.Equal(t, gerrors.GatewayTimeout.Body, result.Body)
}

func TestHandler_Returns503OnNatsError(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, nil, errors.New("connection refused"))

	result := h.Handle(context.Background(),emptyServeInput("GET", "/users"))

	assert.Equal(t, 503, result.Status)
	assert.Equal(t, gerrors.ServiceUnavailable.Body, result.Body)
}

func TestHandler_Returns502OnMalformedReply(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, []byte(`not json`), nil)

	result := h.Handle(context.Background(),emptyServeInput("GET", "/users"))

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

	result := h.Handle(context.Background(),in)

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

	result := sut.Handle(context.Background(),in)

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

	result := sut.Handle(context.Background(),authServeInput("GET", "/users/me"))

	assert.Equal(t, 401, result.Status)
	assert.Equal(t, []string{"Bearer"}, result.Headers["www-authenticate"])

	// Route was NOT called.
	require.Len(t, nats.requests, 1)
	assert.Equal(t, verifierSubject, nats.requests[0].subject)
}

func TestHandler_StampsDefaultWWWAuthenticateOn401WhenVerifierOmitsIt(t *testing.T) {
	// RFC 9110 §11.6.1: a 401 response MUST carry a WWW-Authenticate
	// challenge. Verifiers that return 401 without one would make the
	// gateway emit a spec-violating response; the handler stamps a
	// default `Bearer realm="gateway"` in that case.
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
		[]byte(`{"status":401,"headers":{},"body":{"error":"Unauthorized"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(context.Background(),authServeInput("GET", "/users/me"))

	assert.Equal(t, 401, result.Status)
	assert.Equal(t, []string{`Bearer realm="gateway"`}, result.Headers["www-authenticate"])
}

func TestHandler_PreservesVerifierWWWAuthenticateOn401(t *testing.T) {
	// When the verifier supplies its own challenge, the gateway must
	// forward it verbatim — custom realm, scope, or error parameters
	// are part of the verifier's contract with its clients.
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
		[]byte(`{"status":401,"headers":{"www-authenticate":["Bearer realm=\"users\", error=\"invalid_token\""]},"body":{"error":"Unauthorized"}}`),
		nil,
	)

	sut := newAuthHandler(stubTable(route), nats)

	result := sut.Handle(context.Background(),authServeInput("GET", "/users/me"))

	assert.Equal(t, 401, result.Status)
	assert.Equal(t,
		[]string{`Bearer realm="users", error="invalid_token"`},
		result.Headers["www-authenticate"],
	)
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
	result := sut.Handle(context.Background(),authServeInput("GET", "/articles/:id"))

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

	result := sut.Handle(context.Background(),authServeInput("GET", "/articles/:id"))

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

	result := sut.Handle(context.Background(),authServeInput("GET", "/users/me"))

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

	result := sut.Handle(context.Background(),authServeInput("GET", "/users/me"))

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

	result := sut.Handle(context.Background(),authServeInput("GET", "/users/me"))

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

	result := sut.Handle(context.Background(),authServeInput("GET", "/users/me"))

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

	result := sut.Handle(context.Background(),authServeInput("GET", "/users/me"))

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

	result := sut.Handle(context.Background(),authServeInput("GET", "/users/me"))

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

	result := sut.Handle(context.Background(),authServeInput("GET", "/articles/:id"))

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

	result := sut.Handle(context.Background(),in)

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

	result := h.Handle(context.Background(),emptyServeInput("GET", "/users"))

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

	result := h.Handle(context.Background(),in)

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

	result := h.Handle(context.Background(),in)

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

	result := h.Handle(context.Background(),in)

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

	result := h.Handle(context.Background(),in)

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

func (f *fakeRateLimiter) Allow(_ context.Context, key string, rps, burst int) (ratelimit.Decision, error) {
	f.calls = append(f.calls, rateLimitCall{key: key, rps: rps, burst: burst})

	return ratelimit.Decision{Allowed: f.allowed}, nil
}

func (f *fakeRateLimiter) FlushPrefix(_ context.Context, _ string) error { return nil }

func (f *fakeRateLimiter) Close() error { return nil }

// routerWithStore wraps an existing ratelimit.Store in a Router whose
// "memory" backend returns that Store on EnsureBackend. Tests use this
// to plug their bespoke fake into the handler without re-implementing
// the Router's dispatch semantics.
func routerWithStore(t *testing.T, s ratelimit.Store) *ratelimit.Router {
	t.Helper()
	router := ratelimit.NewRouter(ratelimit.FailPolicyOpen.Resolve(), zerolog.Nop())
	require.NoError(t, router.EnsureBackend("memory", func() (ratelimit.Store, error) {
		return s, nil
	}))

	return router
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
		RateLimiter: routerWithStore(t, rl),
	})

	in := emptyServeInput("GET", "/users")
	in.RemoteAddr = "1.2.3.4"

	result := h.Handle(context.Background(),in)

	assert.Equal(t, 429, result.Status)
	assert.Equal(t, gerrors.TooManyRequests.Body, result.Body)
	assert.Equal(t, []string{"1"}, result.Headers["Retry-After"])
	assert.Equal(t, []string{"10"}, result.Headers["X-RateLimit-Limit"])
	assert.Equal(t, []string{"0"}, result.Headers["X-RateLimit-Remaining"])

	require.Len(t, rl.calls, 1)
	assert.Equal(t, ratelimit.BuildBucketKey("GET", "/users", "1.2.3.4"), rl.calls[0].key)
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
		RateLimiter: routerWithStore(t, rl),
	})

	h.Handle(context.Background(),emptyServeInput("GET", "/users"))

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

	result := h.Handle(context.Background(),emptyServeInput("GET", "/users"))

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

	result := h.Handle(context.Background(),emptyServeInput("GET", "/slow"))

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

	result := h.Handle(context.Background(),emptyServeInput("GET", "/fast"))

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

	result := h.Handle(context.Background(),emptyServeInput("GET", "/users"))

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

	result := h.Handle(context.Background(),emptyServeInput("GET", "/users"))

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

	result := h.Handle(context.Background(),in)

	assert.Equal(t, 200, result.Status)
	assert.Equal(t, []string{"https://example.com"}, result.Headers["Access-Control-Allow-Origin"])
	assert.Equal(t, []string{"true"}, result.Headers["Access-Control-Allow-Credentials"])
	assert.Equal(t, []string{"Origin"}, result.Headers["Vary"])
	assert.Equal(t,
		[]string{"X-Request-Id, X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, Retry-After"},
		result.Headers["Access-Control-Expose-Headers"],
		"default expose list lands on every cross-origin response",
	)
}

func TestHandler_CORSCustomExposeHeadersReachClient(t *testing.T) {
	cors := &registry.CORSMeta{
		Origins:       []string{"https://example.com"},
		ExposeHeaders: []string{"X-Trace-Id", "X-Server-Version"},
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

	result := h.Handle(context.Background(),in)

	assert.Equal(t, 200, result.Status)
	assert.Equal(t,
		[]string{"X-Trace-Id, X-Server-Version"},
		result.Headers["Access-Control-Expose-Headers"],
		"per-route expose list replaces the gateway default",
	)
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

	result := h.Handle(context.Background(),in)

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

	result := h.Handle(context.Background(),in)

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

func (o *oncePerKeyLimiter) Allow(_ context.Context, key string, rps, burst int) (ratelimit.Decision, error) {
	o.calls = append(o.calls, rateLimitCall{key: key, rps: rps, burst: burst})
	if o.seen[key] {
		return ratelimit.Decision{Allowed: false}, nil
	}

	o.seen[key] = true

	return ratelimit.Decision{Allowed: true}, nil
}

func (o *oncePerKeyLimiter) FlushPrefix(_ context.Context, _ string) error { return nil }

func (o *oncePerKeyLimiter) Close() error { return nil }

// TestHandler_RateLimitKeyByRequestAttribute exercises the rate-limit
// key resolver across the wire-level attribute strategies (header,
// cookie). Both variants share the same behavioural contract:
//
//  1. Two requests from different IPs but the same attribute value
//     land on the same bucket key — the attribute value "wins" over
//     IP because it appears first in the keyBy chain.
//  2. The first request is allowed, the second is rate-limited (the
//     shared once-per-key fake limiter simulates a single-token
//     bucket).
//
// Parameterising lets a future third strategy (for example a
// forwarded-for slice or a query parameter) land as one more case
// instead of another 50-line copy.
func TestHandler_RateLimitKeyByRequestAttribute(t *testing.T) {
	type headerSetter func(in *ServeInput, value string)

	cases := []struct {
		name     string
		path     string
		subject  string
		keyBy    string
		value    string
		setValue headerSetter
	}{
		{
			name:    "header strategy wins over ip",
			path:    "/api",
			subject: "svc.cmd.api",
			keyBy:   "header:x-api-key",
			value:   "shared-key",
			setValue: func(in *ServeInput, value string) {
				in.Headers["x-api-key"] = value
			},
		},
		{
			name:    "cookie strategy wins over ip",
			path:    "/dashboard",
			subject: "svc.cmd.dashboard",
			keyBy:   "cookie:session",
			// Cookie header carries an extra pair so we exercise the
			// trim-and-key-match path in extractCookie.
			value: "abc",
			setValue: func(in *ServeInput, value string) {
				in.Headers["cookie"] = "session=" + value + "; theme=dark"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rl := newOncePerKeyLimiter()
			nats := newFakeNats()
			nats.reply = []byte(`{"status":200,"headers":{},"body":null}`)

			table := &fakeTable{routes: map[string]routing.Route{
				"GET " + tc.path: {
					Subject: tc.subject, PathTemplate: tc.path,
					Method: "GET",
					RateLimit: &registry.RateLimitMeta{
						RPS: 10, Burst: 20,
						KeyBy: []string{tc.keyBy, "ip"},
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
				RateLimiter: routerWithStore(t, rl),
			})

			in1 := emptyServeInput("GET", tc.path)
			in1.RemoteAddr = "1.1.1.1"
			tc.setValue(in1, tc.value)

			r1 := h.Handle(context.Background(),in1)

			assert.Equal(t, 200, r1.Status)

			// Different IP, same attribute value — must still share the
			// bucket because the attribute-based strategy resolves
			// before the ip fallback in the keyBy chain.
			in2 := emptyServeInput("GET", tc.path)
			in2.RemoteAddr = "2.2.2.2"
			tc.setValue(in2, tc.value)

			r2 := h.Handle(context.Background(),in2)

			assert.Equal(t, 429, r2.Status)

			require.Len(t, rl.calls, 2)
			expectedKey := ratelimit.BuildBucketKey("GET", tc.path, tc.value)
			assert.Equal(t, expectedKey, rl.calls[0].key)
			assert.Equal(t, expectedKey, rl.calls[1].key,
				"both requests keyed on %s value, not IP", tc.keyBy)
		})
	}
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
		RateLimiter: routerWithStore(t, rl),
	})

	// First request: IP=10.0.0.1, user:id=user-123 → allowed
	in1 := authServeInput("GET", "/users/me")
	in1.RemoteAddr = "10.0.0.1"
	in1.Headers["authorization"] = "Bearer tok1"

	r1 := h.Handle(context.Background(),in1)

	assert.Equal(t, 200, r1.Status)

	// Second request: different IP, same user:id → rate-limited
	in2 := authServeInput("GET", "/users/me")
	in2.RemoteAddr = "10.0.0.2"
	in2.Headers["authorization"] = "Bearer tok2"

	r2 := h.Handle(context.Background(),in2)

	assert.Equal(t, 429, r2.Status)
	require.Len(t, rl.calls, 2)
	expectedKey := ratelimit.BuildBucketKey("GET", "/users/me", "user-123")
	assert.Equal(t, expectedKey, rl.calls[0].key)
	assert.Equal(t, expectedKey, rl.calls[1].key,
		"both requests keyed on user:id, not IP")
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
		RateLimiter: routerWithStore(t, rl),
	})

	// Request WITHOUT x-api-key header → falls back to IP
	in := emptyServeInput("GET", "/api")
	in.RemoteAddr = "5.5.5.5"

	result := h.Handle(context.Background(),in)

	assert.Equal(t, 200, result.Status)
	require.Len(t, rl.calls, 1)
	assert.Equal(t, ratelimit.BuildBucketKey("GET", "/api", "5.5.5.5"), rl.calls[0].key,
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

	result := h.Handle(context.Background(),in)

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

	result := h.Handle(context.Background(),in)

	assert.Equal(t, 404, result.Status,
		"preflight on a route without CORS config returns 404")
}

// TestExtractCookie_TrimsWhitespaceAndQuotes pins RFC 6265 §5.4
// normalisation: leading whitespace after the "=" is dropped and a
// surrounding pair of double quotes is stripped. Without the
// normalisation, semantically equivalent cookies would land in
// different rate-limit buckets.
func TestExtractCookie_TrimsWhitespaceAndQuotes(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"plain value", "session=abc", "abc"},
		{"quoted value", `session="abc"`, "abc"},
		{"leading whitespace after equals", "session= abc", "abc"},
		{"quoted with surrounding spaces", `session= "abc"`, "abc"},
		{"value among siblings", "theme=dark; session=abc; lang=en", "abc"},
		{"quoted value among siblings", `theme=dark; session="abc"; lang=en`, "abc"},
		{"single trailing quote stays attached", `session=abc"`, `abc"`},
		{"single leading quote stays attached", `session="abc`, `"abc`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractCookie(map[string]string{"cookie": c.header}, "session")
			assert.Equal(t, c.want, got)
		})
	}
}

// TestExtractCookie_MissingCookieReturnsEmpty verifies the no-cookie
// fast paths: empty header, name absent from a populated header.
func TestExtractCookie_MissingCookieReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", extractCookie(map[string]string{}, "session"))
	assert.Equal(t, "", extractCookie(map[string]string{"cookie": "theme=dark"}, "session"))
}

// --- Context propagation tests ---

// ctxKey is a package-private type used by the ctx-propagation tests
// so they can stash a sentinel value on the parent ctx and recover it
// from the recordedCall captured inside the fake requester.
type ctxKey string

const ctxSentinelKey ctxKey = "test-sentinel"

// TestHandler_PropagatesContextToNATSRequest pins the no-orphan-IO
// invariant on the happy path: the inbound HTTP context flows down to
// the NATS layer so a client disconnect or a server-side cancellation
// can tear down the upstream call instead of letting it run to its
// per-route timeout.
func TestHandler_PropagatesContextToNATSRequest(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	nats := newFakeNats()
	nats.reply = []byte(`{"status":200,"headers":{},"body":null}`)

	sut := NewHandler(HandlerConfig{
		Table:   func() routing.Table { return table },
		Nats:    nats,
		Encoder: NewDefaultEncoder(),
		Decoder: NewDefaultDecoder(),
		Timeout: 30 * time.Second,
		Logger:  zerolog.Nop(),
	})

	parent := context.WithValue(context.Background(), ctxSentinelKey, "abc")

	result := sut.Handle(parent, emptyServeInput("GET", "/users"))

	require.Equal(t, 200, result.Status)
	require.Len(t, nats.requests, 1)
	require.NotNil(t, nats.requests[0].ctx)
	assert.Equal(t, "abc", nats.requests[0].ctx.Value(ctxSentinelKey),
		"handler must propagate caller ctx into NATS request")
}

// TestHandler_PropagatesContextToVerifierRequest mirrors the previous
// test for the auth flow: both verifier and main route requests must
// carry the inbound ctx so a single client disconnect cancels both
// upstream calls in lock-step.
func TestHandler_PropagatesContextToVerifierRequest(t *testing.T) {
	routeSubject := "users-svc__microservice.cmd.users.me"
	verifierSubject := "users-svc__microservice.cmd.auth.verifier.jwt"

	route := routing.Route{
		Subject:      routeSubject,
		Method:       "GET",
		PathTemplate: "/users/me",
		Auth:         &routing.RouteAuth{VerifierSubject: verifierSubject},
	}

	nats := newFakeNats()
	nats.program(verifierSubject,
		[]byte(`{"status":200,"headers":{},"body":{"id":"u1"}}`), nil)
	nats.program(routeSubject,
		[]byte(`{"status":200,"headers":{},"body":{"ok":true}}`), nil)

	sut := newAuthHandler(stubTable(route), nats)

	parent := context.WithValue(context.Background(), ctxSentinelKey, "v")

	result := sut.Handle(parent, authServeInput("GET", "/users/me"))

	require.Equal(t, 200, result.Status)
	require.Len(t, nats.requests, 2)
	for i, call := range nats.requests {
		require.NotNil(t, call.ctx, "request %d ctx must be propagated", i)
		assert.Equal(t, "v", call.ctx.Value(ctxSentinelKey),
			"both verifier and route NATS calls must carry the parent ctx")
	}
}

// TestHandler_CanceledContextReturns504 pins the timeout-class
// outcome for caller-side cancellation. The inbound HTTP client
// disconnects (or the server cancels for any reason); the upstream
// transport surfaces context.Canceled or context.DeadlineExceeded;
// the handler must turn that into 504 Gateway Timeout, not 503
// Service Unavailable, because no upstream reply will ever arrive
// for a cancelled request.
func TestHandler_CanceledContextReturns504(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, nil, context.Canceled)

	result := h.Handle(context.Background(), emptyServeInput("GET", "/users"))

	assert.Equal(t, 504, result.Status,
		"context.Canceled from the requester is a timeout-class outcome")
}

// TestHandler_DeadlineExceededReturns504 covers the second
// ctx-derived timeout sentinel — the dominant case once the
// requester swapped onto nats.Conn.RequestWithContext, where ctx
// deadline expiry surfaces as context.DeadlineExceeded instead of
// nats.ErrTimeout.
func TestHandler_DeadlineExceededReturns504(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, nil, context.DeadlineExceeded)

	result := h.Handle(context.Background(), emptyServeInput("GET", "/users"))

	assert.Equal(t, 504, result.Status,
		"context.DeadlineExceeded from the requester is a timeout-class outcome")
}
