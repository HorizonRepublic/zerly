// Package http wires the cloudwego/hertz HTTP server to the
// framework-agnostic proxy.Handler. It owns two responsibilities only:
//
//  1. Translating a Hertz *app.RequestContext into a proxy.ServeInput
//     (method, path, headers, query, body, request-id, remote addr).
//  2. Writing the resulting proxy.ServeResult back onto the Hertz
//     response (status, headers, body), stamping content-type and
//     x-request-id on every response regardless of what the upstream
//     handler returned.
//
// Deliberately thin — no middleware, no routing, no business logic.
// Recovery, access logging, metrics, and tracing are layered on
// separately so this translation layer stays easy to audit against
// the framework-agnostic proxy layer above it.
package http

import (
	"bytes"
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/observability"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/proxy"
)

// hopByHopHeaders lists the connection-control headers that MUST NOT
// be forwarded to upstream Nest handlers. Defined in RFC 7230 §6.1;
// the gateway strips them on the way in so upstream services see
// only end-to-end headers, which is the expected contract for a
// well-behaved HTTP proxy. Host is included alongside the standard
// nine because forwarding the gateway's own Host header to a
// downstream RPC is meaningless and would only confuse handlers
// that key on it for multi-tenancy.
var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailers":            {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"host":                {},
}

// initialHeadersCap and initialQueryCap pre-size the adapter's
// working maps based on observed-typical request shapes. The numbers
// match the envelope pool constants in internal/proxy/pool.go so the
// adapter and pool allocate at the same scale and avoid a resize on
// the common case.
const (
	initialHeadersCap = 16
	initialQueryCap   = 4
)

// NewHertzAdapter returns a Hertz HandlerFunc that drives a single
// request through the proxy pipeline. The returned closure captures
// handler by reference, so callers may share one adapter across the
// entire Hertz route tree — there is no per-request state stored on
// the adapter itself.
func NewHertzAdapter(handler *proxy.Handler) app.HandlerFunc {
	return func(_ context.Context, ctx *app.RequestContext) {
		input := buildServeInput(ctx)
		result := handler.Handle(input)
		writeServeResult(ctx, result)
	}
}

// buildServeInput translates a Hertz request context into the
// framework-agnostic proxy.ServeInput shape. All header keys are
// lowercased so downstream code can key on canonical form without
// knowing which framework produced them. Hop-by-hop headers are
// dropped. Query values are collected into the typed QueryValue
// union so single-occurrence keys marshal as strings and repeated
// keys marshal as arrays — preserving the TypeScript
// `string | readonly string[]` contract on the wire.
//
// The X-Request-Id response header is stamped here, BEFORE the
// proxy handler runs, so even error responses written further down
// the pipeline carry the correlator. The request id itself is also
// returned in the ServeInput so the proxy can echo it inside the
// envelope meta block.
func buildServeInput(ctx *app.RequestContext) *proxy.ServeInput {
	method := string(ctx.Method())
	path := string(ctx.Path())

	headers := make(map[string]string, initialHeadersCap)
	ctx.Request.Header.VisitAll(func(key, value []byte) {
		lowerKey := string(bytes.ToLower(key))
		if _, skip := hopByHopHeaders[lowerKey]; skip {
			return
		}
		headers[lowerKey] = string(value)
	})

	query := collectQueryValues(ctx)

	requestID := observability.NewRequestID()
	ctx.Response.Header.Set("X-Request-Id", requestID)

	return &proxy.ServeInput{
		Method:      method,
		Path:        path,
		Body:        ctx.Request.Body(),
		Query:       query,
		Headers:     headers,
		RequestID:   requestID,
		Traceparent: headers["traceparent"],
		RemoteAddr:  resolveRemoteAddr(ctx),
		ReceivedAt:  time.Now().UnixMilli(),
	}
}

// collectQueryValues walks the Hertz query arguments and returns a
// map keyed on the raw parameter name with values wrapped in the
// typed QueryValue union. Keys observed exactly once become the
// scalar Single variant; keys observed two or more times become the
// slice Multi variant, preserving "repeated" semantics so the
// upstream handler's Array.isArray() discriminator still works.
//
// Two-pass collection — accumulate into an intermediate
// map[string][]string, then convert — is deliberate: Hertz's
// VisitAll callback fires once per (key, value) pair, and attempting
// to make the union decision in the callback requires mutating the
// target map mid-iteration, which is error-prone and harder to read.
func collectQueryValues(ctx *app.RequestContext) map[string]proxy.QueryValue {
	accumulator := make(map[string][]string, initialQueryCap)
	ctx.QueryArgs().VisitAll(func(key, value []byte) {
		k := string(key)
		accumulator[k] = append(accumulator[k], string(value))
	})

	result := make(map[string]proxy.QueryValue, len(accumulator))
	for k, values := range accumulator {
		if len(values) == 1 {
			result[k] = proxy.NewQueryValueString(values[0])
			continue
		}
		result[k] = proxy.NewQueryValueStrings(values)
	}
	return result
}

// writeServeResult copies a proxy.ServeResult onto the Hertz response
// buffer. The content-type header is forced to application/json AFTER
// the caller-supplied headers are applied so the gateway always owns
// the wire format — an upstream handler cannot change it to anything
// else, which is a deliberate anti-spoofing measure that lets HTTP
// clients parse the body without sniffing.
//
// Header.Add is used instead of Header.Set so each slice entry lands
// as a separate header line on the client response. The critical
// case is Set-Cookie: Hertz's setSpecialHeader routes every Add on
// "Set-Cookie" through its per-cookie slot (internally an append),
// so calling Add twice yields two cookie lines — exactly the RFC
// 6265 shape browsers expect. Single-value headers with a one-element
// slice land as one Add call, equivalent to Set on an empty slot.
//
// The `x-request-id` slot is explicitly cleared before the Add loop
// runs. `buildServeInput` stamps the correlator on `ctx.Response`
// up front so a panic between input-build and writeServeResult
// still gives the error response a correlator, but the proxy layer
// then re-emits it via `result.Headers` — without the Del, Add
// would produce two identical `X-Request-Id` response headers on
// every request. Clearing only this specific slot keeps the Del
// targeted to the header the adapter itself stamped; other
// header names reach the Add loop untouched and preserve
// whatever semantics Hertz middleware may have configured.
func writeServeResult(ctx *app.RequestContext, result *proxy.ServeResult) {
	ctx.Response.Header.Del("X-Request-Id")

	for key, values := range result.Headers {
		for _, value := range values {
			ctx.Response.Header.Add(key, value)
		}
	}
	ctx.Response.Header.SetContentType(consts.MIMEApplicationJSON)

	// Gateway-owned error bodies (404/502/503/504/429/...) are
	// transient infrastructure failures — stamp `Cache-Control:
	// no-store` so intermediate caches never memoize them. Handler-
	// thrown errors are untouched because their cache policy is part
	// of the application contract, not the gateway's to override.
	if result.GatewayOwnedBody {
		ctx.Response.Header.Set("Cache-Control", "no-store")
	}

	ctx.SetStatusCode(result.Status)
	ctx.Response.SetBody(result.Body)
}

// resolveRemoteAddr returns the client IP the handler should see.
//
// The trusted-proxy middleware stamps the resolved IP on the
// request context via ctx.Set(clientIPUserKey, ...). This helper
// reads that value; if the middleware did not run (unit tests that
// drive the adapter directly, or a future startup path that forgets
// to register it) we fall back to Hertz's built-in ClientIP() so
// the request still serves with a best-effort IP.
//
// The empty-string guard exists because a buggy middleware writing
// "" would otherwise propagate an empty RemoteAddr into the
// envelope meta — the fallback preserves the "always return
// something" contract.
func resolveRemoteAddr(ctx *app.RequestContext) string {
	if raw, ok := ctx.Get(clientIPUserKey); ok {
		if v, ok := raw.(string); ok && v != "" {
			return v
		}
	}

	return ctx.ClientIP()
}
