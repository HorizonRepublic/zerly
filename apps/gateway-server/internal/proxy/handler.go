package proxy

import (
	"encoding/json"
	"time"

	"github.com/rs/zerolog"

	gerrors "github.com/HorizonRepublic/zerly/apps/gateway-server/internal/errors"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/routing"
)

// TableProvider returns the currently-active routing Table. The gateway
// rebuilds its Table on every registry change; the provider closure
// gives Handler an atomic view without requiring it to coordinate with
// the watcher directly.
//
// Implementations MUST return a non-nil Table. A nil return crashes
// the handler — an acceptable failure mode for a pure programming bug
// because it surfaces immediately instead of silently 404-ing.
type TableProvider func() routing.Table

// HandlerConfig bundles the dependencies of a Handler. Passed by value
// at construction; all fields are required and the zero value of a
// HandlerConfig is NOT safe to use.
type HandlerConfig struct {
	Table   TableProvider
	Nats    NatsRequester
	Encoder Encoder
	Decoder Decoder
	Timeout time.Duration
	Logger  zerolog.Logger
}

// Handler is the HTTP→NATS→HTTP orchestrator. It owns one request from
// lookup to response write. All I/O dependencies are injected via
// HandlerConfig, so Handle is trivially unit-testable with fakes.
type Handler struct {
	cfg HandlerConfig
}

// NewHandler constructs a Handler from the supplied configuration.
// The caller retains ownership of cfg.Logger — Handler clones it with
// no additional fields, so log entries carry whatever context the
// caller pre-configured.
func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{cfg: cfg}
}

// ServeInput is a framework-agnostic view of an incoming HTTP request.
// The Hertz (and any future) HTTP adapter populates this struct and
// calls Handle, which contains all lookup/encoding/decoding logic
// independent of any HTTP framework.
type ServeInput struct {
	Method      string
	Path        string
	Body        []byte
	Query       map[string]QueryValue
	Headers     map[string]string
	RequestID   string
	Traceparent string
	RemoteAddr  string
	ReceivedAt  int64
}

// ServeResult is the framework-agnostic outcome of handling a request.
// Adapters translate it into their HTTP response representation.
type ServeResult struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

// Handle performs the full request lifecycle: route lookup, envelope
// encode, NATS request, reply decode, response construction. Errors
// are translated to the appropriate HTTP status with a pre-encoded
// JSON error body from the internal/errors package.
//
// Payload ownership: the request envelope is marshalled into a
// pooled scratch []byte acquired from payloadPool. The defer
// releases the buffer back to the pool when Handle returns. This is
// safe because nats.Conn.Request synchronously copies the outgoing
// message into its write buffer before returning the reply — by the
// time Request returns, the payload slice is no longer referenced by
// NATS and is safe to reuse. Any future refactor that keeps the
// payload slice alive beyond this function MUST stop using the pool.
func (h *Handler) Handle(in *ServeInput) *ServeResult {
	table := h.cfg.Table()
	route, params, ok := table.Lookup(in.Method, in.Path)
	if !ok {
		return toServeResult(gerrors.NotFound)
	}

	var claims json.RawMessage

	if route.Auth != nil {
		authResult, proceed := h.runAuthFlow(in, route, params)
		if !proceed {
			return authResult
		}

		claims = authResult.Body
	}

	payload := acquirePayload()
	defer releasePayload(payload)

	err := h.cfg.Encoder.Encode(payload, &EncodeInput{
		Method:      in.Method,
		Path:        in.Path,
		Body:        in.Body,
		Query:       in.Query,
		Headers:     in.Headers,
		Route:       route,
		PathParams:  params,
		RequestID:   in.RequestID,
		Traceparent: in.Traceparent,
		RemoteAddr:  in.RemoteAddr,
		ReceivedAt:  in.ReceivedAt,
		TimeoutMs:   h.cfg.Timeout.Milliseconds(),
		Auth:        claims,
	})
	if err != nil {
		h.cfg.Logger.Error().Err(err).Msg("proxy encode failed")
		return toServeResult(gerrors.InternalError)
	}

	replyBytes, err := h.cfg.Nats.Request(route.Subject, *payload, h.cfg.Timeout)
	if err != nil {
		if isTimeoutErr(err) {
			return toServeResult(gerrors.GatewayTimeout)
		}
		h.cfg.Logger.Error().Err(err).Str("subject", route.Subject).Msg("nats request failed")
		return toServeResult(gerrors.ServiceUnavailable)
	}

	reply, err := h.cfg.Decoder.Decode(replyBytes)
	if err != nil {
		h.cfg.Logger.Error().Err(err).Msg("reply decode failed")
		return toServeResult(gerrors.BadGateway)
	}

	return &ServeResult{
		Status:  reply.Status,
		Headers: mergeHeaders(reply.Headers, in.RequestID),
		Body:    reply.Body,
	}
}

// runAuthFlow issues the verifier sub-request for a protected route
// and decides whether the main route request proceeds.
//
// Return semantics:
//
//   - proceed == false → the caller MUST return the ServeResult
//     pointer verbatim. Covers 401/403 short-circuits, verifier
//     transport errors (503/504/502), and decoder failures.
//   - proceed == true → the caller continues to the main route
//     request. The returned ServeResult.Body carries the verifier's
//     reply body (the claims) to inject into the main envelope's
//     Auth field. On an optional-auth 401 swallow, Body is nil.
//
// The ServeResult pointer returned on the "proceed" path is reused
// purely as a claims carrier — only Body is meaningful in that case.
func (h *Handler) runAuthFlow(
	in *ServeInput,
	route routing.Route,
	params map[string]string,
) (*ServeResult, bool) {
	verifyPayload := acquirePayload()
	defer releasePayload(verifyPayload)

	// The verify-request envelope is identical to the main envelope
	// except Body is always nil — verifiers never see the request
	// body by design (auth design spec §4.2).
	err := h.cfg.Encoder.Encode(verifyPayload, &EncodeInput{
		Method:      in.Method,
		Path:        in.Path,
		Body:        nil,
		Query:       in.Query,
		Headers:     in.Headers,
		Route:       route,
		PathParams:  params,
		RequestID:   in.RequestID,
		Traceparent: in.Traceparent,
		RemoteAddr:  in.RemoteAddr,
		ReceivedAt:  in.ReceivedAt,
		TimeoutMs:   h.cfg.Timeout.Milliseconds(),
	})
	if err != nil {
		h.cfg.Logger.Error().Err(err).Msg("auth: verify encode failed")
		return toServeResult(gerrors.InternalError), false
	}

	replyBytes, err := h.cfg.Nats.Request(route.Auth.VerifierSubject, *verifyPayload, h.cfg.Timeout)
	if err != nil {
		if isTimeoutErr(err) {
			return toServeResult(gerrors.GatewayTimeout), false
		}
		h.cfg.Logger.Error().
			Err(err).
			Str("subject", route.Auth.VerifierSubject).
			Msg("auth: verifier nats request failed")
		return toServeResult(gerrors.ServiceUnavailable), false
	}

	reply, err := h.cfg.Decoder.Decode(replyBytes)
	if err != nil {
		h.cfg.Logger.Error().Err(err).Msg("auth: verifier reply decode failed")
		return toServeResult(gerrors.BadGateway), false
	}

	if reply.Status == 200 {
		return &ServeResult{Body: reply.Body}, true
	}

	// Optional-auth: swallow 401 only. 403 and every other non-200
	// status still short-circuits. Transport errors above already
	// returned before this branch.
	if route.Auth.Optional && reply.Status == 401 {
		return &ServeResult{Body: nil}, true
	}

	// Forward the verifier's reply verbatim — this is how verifier-set
	// headers like WWW-Authenticate reach the client.
	return &ServeResult{
		Status:  reply.Status,
		Headers: mergeHeaders(reply.Headers, in.RequestID),
		Body:    reply.Body,
	}, false
}

// mergeHeaders combines the reply headers with gateway-owned headers
// (content-type, x-request-id). The gateway always stamps its own
// x-request-id — any value the upstream service set is intentionally
// overwritten to prevent spoofing.
func mergeHeaders(reply map[string]string, requestID string) map[string]string {
	out := make(map[string]string, len(reply)+2)
	out["content-type"] = "application/json"
	for k, v := range reply {
		out[k] = v
	}
	out["x-request-id"] = requestID
	return out
}

// jsonHeaders returns a fresh header map carrying only content-type.
// Allocates on every call because ServeResult.Headers is expected to
// be caller-owned — the shared pre-encoded error body is paired with
// this per-request header map so no caller ever sees aliased state.
func jsonHeaders() map[string]string {
	return map[string]string{"content-type": "application/json"}
}

// toServeResult materializes a ServeResult from a pre-encoded
// HTTPError. The ServeResult allocates its own headers map because
// the HTTPError is shared across goroutines and must never be
// aliased by a caller-owned mutable map.
func toServeResult(e gerrors.HTTPError) *ServeResult {
	return &ServeResult{
		Status:  e.Status,
		Headers: jsonHeaders(),
		Body:    e.Body,
	}
}
