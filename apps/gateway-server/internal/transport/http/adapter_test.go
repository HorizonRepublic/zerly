package http

import (
	"bytes"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/proxy"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/routing"
)

// ---------- buildServeInput tests ----------

func TestBuildServeInput_ExtractsMethodAndPath(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/users/42", nil)

	input := buildServeInput(ctx)

	assert.Equal(t, "GET", input.Method)
	assert.Equal(t, "/users/42", input.Path)
}

func TestBuildServeInput_LowercasesHeaderKeys(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil,
		ut.Header{Key: "Authorization", Value: "Bearer token"},
		ut.Header{Key: "X-Custom", Value: "value"},
	)

	input := buildServeInput(ctx)

	assert.Equal(t, "Bearer token", input.Headers["authorization"])
	assert.Equal(t, "value", input.Headers["x-custom"])
	assert.NotContains(t, input.Headers, "Authorization")
}

func TestBuildServeInput_StripsHopByHopHeaders(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil,
		ut.Header{Key: "Connection", Value: "keep-alive"},
		ut.Header{Key: "Transfer-Encoding", Value: "chunked"},
		ut.Header{Key: "Upgrade", Value: "h2c"},
	)

	input := buildServeInput(ctx)

	assert.NotContains(t, input.Headers, "connection")
	assert.NotContains(t, input.Headers, "transfer-encoding")
	assert.NotContains(t, input.Headers, "upgrade")
}

func TestBuildServeInput_CollectsSingleValueQuery(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x?include=profile", nil)

	input := buildServeInput(ctx)

	require.Contains(t, input.Query, "include")
	assert.Equal(t, proxy.NewQueryValueString("profile"), input.Query["include"])
}

func TestBuildServeInput_CollectsRepeatedQueryKey(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x?tag=a&tag=b", nil)

	input := buildServeInput(ctx)

	require.Contains(t, input.Query, "tag")
	assert.Equal(t, proxy.NewQueryValueStrings([]string{"a", "b"}), input.Query["tag"])
}

func TestBuildServeInput_CapturesTraceparentHeader(t *testing.T) {
	traceValue := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil,
		ut.Header{Key: "Traceparent", Value: traceValue},
	)

	input := buildServeInput(ctx)

	assert.Equal(t, traceValue, input.Traceparent)
	assert.Equal(t, traceValue, input.Headers["traceparent"])
}

func TestBuildServeInput_GeneratesRequestIDWhenAbsent(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil)

	input := buildServeInput(ctx)

	assert.Len(t, input.RequestID, 26, "monotonic ULID is exactly 26 base32 chars")
}

func TestBuildServeInput_StampsXRequestIdResponseHeader(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil)

	input := buildServeInput(ctx)

	assert.Equal(t, input.RequestID, string(ctx.Response.Header.Peek("X-Request-Id")))
}

// ---------- writeServeResult tests ----------

func TestWriteServeResult_SetsStatusHeadersAndBody(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil)
	result := &proxy.ServeResult{
		Status: 201,
		Headers: map[string][]string{
			"x-custom":     {"yes"},
			"x-request-id": {"r-1"},
		},
		Body: []byte(`{"ok":true}`),
	}

	writeServeResult(ctx, result)

	assert.Equal(t, 201, ctx.Response.StatusCode())
	assert.Equal(t, "yes", string(ctx.Response.Header.Peek("x-custom")))
	assert.Equal(t, "r-1", string(ctx.Response.Header.Peek("x-request-id")))
	assert.Equal(t, `{"ok":true}`, string(ctx.Response.Body()))
}

func TestWriteServeResult_ForcesApplicationJSONContentType(t *testing.T) {
	// Upstream services MUST NOT be able to change the wire content-
	// type. The gateway overrides whatever they sent to application/
	// json as the last write before status, which is the invariant
	// downstream clients rely on to parse the body without sniffing.
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil)
	result := &proxy.ServeResult{
		Status:  200,
		Headers: map[string][]string{"content-type": {"text/plain"}},
		Body:    []byte(`"ok"`),
	}

	writeServeResult(ctx, result)

	assert.Contains(t, string(ctx.Response.Header.Peek("Content-Type")), "application/json")
}

// TestWriteServeResult_EmitsMultipleSetCookieLines is the load-bearing
// test for Phase E.1: a handler that returns two Set-Cookie values in
// the envelope MUST land on the wire as two distinct header lines so
// RFC 6265 §3 parsers (every browser, curl -v, Node's http module)
// recognize both cookies. If this assertion ever breaks it means the
// adapter is joining multi-value headers, which would silently drop
// the second cookie from the client-visible jar.
func TestWriteServeResult_EmitsMultipleSetCookieLines(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil)
	result := &proxy.ServeResult{
		Status: 200,
		Headers: map[string][]string{
			"set-cookie": {
				"sid=abc; Path=/; HttpOnly",
				"theme=dark; Path=/",
			},
		},
		Body: []byte(`{}`),
	}

	writeServeResult(ctx, result)

	var lines []string
	ctx.Response.Header.VisitAllCookie(func(_, value []byte) {
		lines = append(lines, string(value))
	})

	assert.ElementsMatch(t, []string{
		"sid=abc; Path=/; HttpOnly",
		"theme=dark; Path=/",
	}, lines)
}

// TestWriteServeResult_DeduplicatesXRequestIdAcrossBuildAndWrite
// pins the regression fix for a double X-Request-Id header on
// every response. `buildServeInput` stamps the correlator on
// the Hertz response up front so panic-recovery middleware
// still sees it; `writeServeResult` later re-emits it from the
// proxy's ServeResult headers. Without the Del in
// writeServeResult, both writes landed and every HTTP response
// carried two identical `X-Request-Id:` header lines. The
// assertion walks every header line (not just Peek, which
// returns the first value) so a future regression producing
// duplicates is caught loudly instead of sneaking past
// single-value introspection.
func TestWriteServeResult_DeduplicatesXRequestIdAcrossBuildAndWrite(t *testing.T) {
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/x", nil)

	// Simulate the real request flow: buildServeInput stamps
	// X-Request-Id on Response.Header early, then writeServeResult
	// runs with a ServeResult whose Headers include x-request-id
	// (as mergeHeaders in the proxy layer produces on every
	// request).
	input := buildServeInput(ctx)
	result := &proxy.ServeResult{
		Status: 200,
		Headers: map[string][]string{
			"x-request-id": {input.RequestID},
		},
		Body: []byte(`{"ok":true}`),
	}

	writeServeResult(ctx, result)

	var xRequestIDLines []string
	ctx.Response.Header.VisitAll(func(key, value []byte) {
		if bytes.EqualFold(key, []byte("X-Request-Id")) {
			xRequestIDLines = append(xRequestIDLines, string(value))
		}
	})

	// Exactly one X-Request-Id header, with the generated ID.
	assert.Len(t, xRequestIDLines, 1, "expected exactly one X-Request-Id header, got %v", xRequestIDLines)
	assert.Equal(t, input.RequestID, xRequestIDLines[0])
}

// ---------- full adapter integration tests ----------

// fakeTable is a routing.Table double whose behaviour is determined
// by the hit flag and the canned route. Lives in the test file so it
// is not exported beyond the http package.
type fakeTable struct {
	route routing.Route
	hit   bool
}

func (f *fakeTable) Lookup(_, _ string) (routing.Route, map[string]string, bool) {
	if !f.hit {
		return routing.Route{}, nil, false
	}
	return f.route, map[string]string{}, true
}

func (f *fakeTable) Methods(string) []string { return nil }

// fakeRequester is a proxy.NatsRequester double that returns a fixed
// reply payload regardless of subject or timeout.
type fakeRequester struct {
	reply []byte
}

func (f *fakeRequester) Request(string, []byte, time.Duration) ([]byte, error) {
	return f.reply, nil
}

func TestAdapter_ForwardsResponseThroughProxyHandler(t *testing.T) {
	table := &fakeTable{
		route: routing.Route{
			Subject:      "svc.cmd.users.list",
			Method:       "GET",
			PathTemplate: "/users",
		},
		hit: true,
	}
	requester := &fakeRequester{
		reply: []byte(`{"status":200,"headers":{"x-custom":["yes"]},"body":{"ok":true}}`),
	}
	handler := proxy.NewHandler(proxy.HandlerConfig{
		Table:   func() routing.Table { return table },
		Nats:    requester,
		Encoder: proxy.NewDefaultEncoder(),
		Decoder: proxy.NewDefaultDecoder(),
		Timeout: 5 * time.Second,
		Logger:  zerolog.Nop(),
	})

	adapter := NewHertzAdapter(handler)
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/users", nil)

	adapter(nil, ctx)

	assert.Equal(t, 200, ctx.Response.StatusCode())
	assert.Equal(t, "yes", string(ctx.Response.Header.Peek("x-custom")))
	assert.Contains(t, string(ctx.Response.Header.Peek("Content-Type")), "application/json")
	assert.Equal(t, `{"ok":true}`, string(ctx.Response.Body()))
}

func TestAdapter_Returns404WhenRouteNotFound(t *testing.T) {
	table := &fakeTable{hit: false}
	handler := proxy.NewHandler(proxy.HandlerConfig{
		Table:   func() routing.Table { return table },
		Nats:    &fakeRequester{},
		Encoder: proxy.NewDefaultEncoder(),
		Decoder: proxy.NewDefaultDecoder(),
		Timeout: 5 * time.Second,
		Logger:  zerolog.Nop(),
	})

	adapter := NewHertzAdapter(handler)
	ctx := ut.CreateUtRequestContext("GET", "https://gateway.test/unknown", nil)

	adapter(nil, ctx)

	assert.Equal(t, 404, ctx.Response.StatusCode())
}
