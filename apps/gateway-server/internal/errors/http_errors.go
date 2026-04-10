// Package errors exposes pre-encoded HTTP error response bodies for
// use on the gateway's hot path.
//
// Computing the JSON once at init() time and reusing both the status
// code and the byte slice for every error response eliminates
// allocations on the error path. At gateway scale — where a small
// percentage of every traffic spike returns 404/504/502 — this is
// measurably worth it in benchmarks, and it also guarantees that a
// given error class always ships the same wire shape regardless of
// which goroutine produced it.
//
// Each error is exposed as an HTTPError value rather than a loose
// pair of (int, []byte) variables so handler code passes a single
// symbolic argument and cannot accidentally pair a 404 body with a
// 500 status or vice versa.
package errors

import (
	"fmt"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/codec"
)

// HTTPError bundles an HTTP status code with a pre-encoded JSON body.
// Values of this type are constructed exactly once at package init()
// time and are safe to share across goroutines; the underlying byte
// slice is never mutated after construction.
//
// Consumers read Status and Body directly — there are no setters or
// mutators on purpose, so accidental mutation of a shared error body
// would require going out of the Go type system.
type HTTPError struct {
	// Status is the HTTP status code the gateway returns to the
	// client. Always a valid RFC 9110 status in the 400-599 range.
	Status int
	// Body is the pre-marshalled JSON response body. Always
	// non-empty.
	Body []byte
}

// HTTP status constants extracted so the init() table reads as intent
// instead of raw numbers. Exported so handler code can reference them
// directly without hard-coding magic numbers, and to make the full
// set of statuses the gateway may produce obvious at a glance.
const (
	StatusNotFound           = 404
	StatusMethodNotAllowed   = 405
	StatusPayloadTooLarge    = 413
	StatusUnsupportedMedia   = 415
	StatusInternalError      = 500
	StatusBadGateway         = 502
	StatusServiceUnavailable = 503
	StatusGatewayTimeout     = 504
)

// Pre-encoded HTTP errors produced once at init() time. Every error
// that the proxy handler may emit is declared here so the complete
// set of gateway-owned error responses is auditable in a single file.
//
// New entries MUST use the same build helper as the existing set so
// their JSON shape stays consistent with the application/problem+json
// style the gateway follows for error envelopes.
var (
	// NotFound is the 404 response returned when the routing table
	// has no match for the requested method+path combination.
	NotFound HTTPError
	// MethodNotAllowed is the 405 response returned when the path
	// matches but no registered route accepts the request method.
	// Currently unused by the proxy handler; reserved for a future
	// method-aware routing enhancement.
	MethodNotAllowed HTTPError
	// PayloadTooLarge is the 413 response returned by the Hertz
	// layer when a request exceeds HTTP_MAX_BODY_BYTES. Reserved for
	// a future transport-level hook.
	PayloadTooLarge HTTPError
	// UnsupportedMedia is the 415 response returned when a request
	// carries a Content-Type the gateway cannot forward. Reserved
	// for a future content-negotiation layer.
	UnsupportedMedia HTTPError
	// InternalError is the generic 500 response returned when the
	// proxy fails to encode the outbound envelope or hits an
	// unexpected internal condition.
	InternalError HTTPError
	// ServiceUnavailable is the 503 response returned when the
	// NATS round trip fails with any error other than a timeout
	// (connection drop, no-responders, transport closed, etc.).
	ServiceUnavailable HTTPError
	// GatewayTimeout is the 504 response returned when the NATS
	// round trip fails with nats.ErrTimeout — the upstream service
	// was reachable but did not reply within RequestTimeout.
	GatewayTimeout HTTPError
	// BadGateway is the 502 response returned when the upstream
	// service replied with a payload the decoder could not parse
	// or whose status field was out of the legal 100-599 range.
	BadGateway HTTPError
)

func init() {
	NotFound = build(StatusNotFound, "NOT_FOUND", "The requested route was not found on this gateway.")
	MethodNotAllowed = build(StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "The HTTP method is not allowed for this route.")
	PayloadTooLarge = build(StatusPayloadTooLarge, "PAYLOAD_TOO_LARGE", "The request body exceeds the maximum allowed size.")
	UnsupportedMedia = build(StatusUnsupportedMedia, "UNSUPPORTED_MEDIA_TYPE", "The request Content-Type is not supported.")
	InternalError = build(StatusInternalError, "INTERNAL_SERVER_ERROR", "An unexpected error occurred.")
	ServiceUnavailable = build(StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "The gateway cannot reach any upstream service.")
	GatewayTimeout = build(StatusGatewayTimeout, "GATEWAY_TIMEOUT", "The upstream service did not reply in time.")
	BadGateway = build(StatusBadGateway, "BAD_GATEWAY", "The upstream service returned a malformed response.")
}

// build marshals a (code, message) pair through the shared codec and
// wraps the result in an HTTPError paired with the supplied status.
// Any marshalling failure is fatal — we are encoding a fixed-shape
// map with string values only, and a failure there indicates a
// corrupt build of the sonic/codec layer that should prevent the
// process from starting rather than silently serving empty bodies.
func build(status int, code, message string) HTTPError {
	body, err := codec.Marshal(map[string]string{
		"error":   code,
		"message": message,
	})
	if err != nil {
		panic(fmt.Sprintf("errors: failed to pre-encode %q: %v", code, err))
	}
	return HTTPError{Status: status, Body: body}
}
