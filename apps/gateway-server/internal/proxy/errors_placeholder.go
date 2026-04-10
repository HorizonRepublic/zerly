package proxy

// HTTP status constants used by the proxy Handler when translating
// internal errors to response codes. Extracted so the handler code
// reads as intent rather than magic numbers.
const (
	statusNotFound           = 404
	statusInternalError      = 500
	statusBadGateway         = 502
	statusServiceUnavailable = 503
	statusGatewayTimeout     = 504
)

// Temporary placeholder bodies used by Handler until Milestone 19
// replaces them with pre-encoded application/problem+json payloads in
// internal/errors/http_errors.go. Kept here so the package compiles
// and handler_test.go can reference real byte slices.
var (
	notFoundBody           = []byte(`{"error":"NOT_FOUND"}`)
	internalErrorBody      = []byte(`{"error":"INTERNAL_SERVER_ERROR"}`)
	gatewayTimeoutBody     = []byte(`{"error":"GATEWAY_TIMEOUT"}`)
	serviceUnavailableBody = []byte(`{"error":"SERVICE_UNAVAILABLE"}`)
	badGatewayBody         = []byte(`{"error":"BAD_GATEWAY"}`)
)
