package proxy

import (
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// fakeRequester implements NatsRequester with canned reply/error.
type fakeRequester struct {
	reply []byte
	err   error
}

func (f *fakeRequester) Request(string, []byte, time.Duration) ([]byte, error) {
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
	assert.Equal(t, "r1", result.Headers["x-request-id"])
	assert.Equal(t, "application/json", result.Headers["content-type"])
	assert.JSONEq(t, `{"ok":true}`, string(result.Body))
}

func TestHandler_Returns404WhenRouteNotFound(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{}}
	h := buildHandler(table, nil, nil)

	result := h.Handle(emptyServeInput("GET", "/unknown"))

	assert.Equal(t, 404, result.Status)
	assert.Equal(t, notFoundBody, result.Body)
}

func TestHandler_Returns504OnTimeout(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, nil, errTimeoutPlaceholder)

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, 504, result.Status)
	assert.Equal(t, gatewayTimeoutBody, result.Body)
}

func TestHandler_Returns503OnNatsError(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, nil, errors.New("connection refused"))

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, 503, result.Status)
	assert.Equal(t, serviceUnavailableBody, result.Body)
}

func TestHandler_Returns502OnMalformedReply(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	h := buildHandler(table, []byte(`not json`), nil)

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, 502, result.Status)
	assert.Equal(t, badGatewayBody, result.Body)
}

func TestHandler_SuccessReplyPreservesStatusAndHeaders(t *testing.T) {
	table := &fakeTable{routes: map[string]routing.Route{
		"POST /users": {Subject: "svc.cmd.users.create", PathTemplate: "/users", Method: "POST"},
	}}
	reply := []byte(`{"status":201,"headers":{"x-custom":"yes"},"body":{"id":"1"}}`)
	h := buildHandler(table, reply, nil)

	in := emptyServeInput("POST", "/users")
	in.Body = []byte(`{"name":"Alice"}`)

	result := h.Handle(in)

	assert.Equal(t, 201, result.Status)
	assert.Equal(t, "yes", result.Headers["x-custom"])
	assert.Equal(t, "r1", result.Headers["x-request-id"], "x-request-id is always gateway-owned")
	require.NotNil(t, result.Body)
}

func TestHandler_OverwritesUpstreamRequestID(t *testing.T) {
	// Upstream services MUST NOT be able to set x-request-id — the
	// gateway always stamps its own value so request-id tracking
	// cannot be spoofed by a compromised handler.
	table := &fakeTable{routes: map[string]routing.Route{
		"GET /users": {Subject: "svc.cmd.users.list", PathTemplate: "/users", Method: "GET"},
	}}
	reply := []byte(`{"status":200,"headers":{"x-request-id":"spoofed"},"body":null}`)
	h := buildHandler(table, reply, nil)

	result := h.Handle(emptyServeInput("GET", "/users"))

	assert.Equal(t, "r1", result.Headers["x-request-id"])
}
