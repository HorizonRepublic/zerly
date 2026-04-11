package proxy

import (
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/routing"
)

// EncodeInput bundles the data needed to build a GatewayRequest envelope.
// Kept as a struct rather than a long parameter list so adapters can
// populate fields in any order without confusing positional arguments.
//
// Path is the ACTUAL request path (e.g. "/users/42"), while
// Route.PathTemplate carries the matched template (e.g. "/users/:id").
// The encoder assembles them into the envelope's RouteContext so Nest
// handlers can read either via the @GatewayRoute param decorator.
type EncodeInput struct {
	Method      string
	Path        string
	Body        []byte
	Query       map[string]QueryValue
	Headers     map[string]string
	Route       routing.Route
	PathParams  map[string]string
	RequestID   string
	Traceparent string
	RemoteAddr  string
	ReceivedAt  int64
	TimeoutMs   int64
}

// Encoder builds GatewayRequest envelopes from pre-parsed HTTP request
// data into a caller-owned scratch buffer. The interface is
// deliberately narrow so fakes stub it in a few lines and so a future
// alternative encoder (e.g. protobuf) can swap in without touching
// Handler.
//
// Callers own the buffer and are responsible for its lifetime:
// acquire from a pool, pass a pointer to Encode, pass the dereferenced
// slice to the transport, then release. This eliminates the per-call
// output-slice allocation that a Marshal-style signature would
// otherwise require on the hot path.
type Encoder interface {
	Encode(out *[]byte, in *EncodeInput) error
}

// DefaultEncoder builds JSON envelopes using a hand-rolled,
// zero-allocation serializer defined in envelope_encode.go. It
// intentionally bypasses sonic for this specific type: sonic's
// map-iteration path allocates per-encode when reflecting over the
// three map fields of GatewayRequest, which is incompatible with the
// hot-path zero-alloc budget. Stateless and safe for concurrent use.
type DefaultEncoder struct{}

// NewDefaultEncoder returns an Encoder backed by the hand-rolled
// envelope serializer. The returned pointer is safe to share across
// goroutines.
func NewDefaultEncoder() *DefaultEncoder {
	return &DefaultEncoder{}
}

// Compile-time assertion that DefaultEncoder satisfies the Encoder
// contract. Adding a new method to Encoder fails the build here
// before any caller is affected.
var _ Encoder = (*DefaultEncoder)(nil)

// Encode assembles a GatewayRequest from in and appends its JSON
// representation onto *out via the hand-rolled envelope serializer.
// The pooled envelope is reset and released via defer so every code
// path returns it to the pool exactly once. The out slice is grown
// only by the standard append builtin as the encoder writes, so the
// caller's pooled backing array is reused in the common case and
// automatically extended when a larger envelope does not fit.
//
// The error return is preserved to keep the Encoder interface stable
// across alternative implementations that may need to fail, but the
// hand-rolled path itself cannot fail: every field has a deterministic
// JSON emission and no I/O is performed.
func (e *DefaultEncoder) Encode(out *[]byte, in *EncodeInput) error {
	envelope := acquireEnvelope()
	defer releaseEnvelope(envelope)

	envelope.Route = RouteContext{
		Method:      in.Method,
		Path:        in.Route.PathTemplate,
		MatchedPath: in.Path,
	}
	for k, v := range in.PathParams {
		envelope.Params[k] = v
	}
	for k, v := range in.Query {
		envelope.Query[k] = v
	}
	for k, v := range in.Headers {
		envelope.Headers[k] = v
	}
	envelope.Body = in.Body
	envelope.Meta = RequestMeta{
		RequestID:   in.RequestID,
		Traceparent: in.Traceparent,
		RemoteAddr:  in.RemoteAddr,
		ReceivedAt:  in.ReceivedAt,
		TimeoutMs:   in.TimeoutMs,
	}

	*out = appendEnvelopeJSON(*out, envelope)
	return nil
}
