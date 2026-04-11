package proxy

import (
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
