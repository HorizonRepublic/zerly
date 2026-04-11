package proxy

import (
	"fmt"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/codec"
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

// DefaultEncoder builds JSON envelopes using sonic through the codec
// package. It is stateless and safe for concurrent use.
type DefaultEncoder struct{}

// NewDefaultEncoder returns an Encoder backed by the codec package.
// The returned pointer is safe to share across goroutines.
func NewDefaultEncoder() *DefaultEncoder {
	return &DefaultEncoder{}
}

// Compile-time assertion that DefaultEncoder satisfies the Encoder
// contract. Adding a new method to Encoder fails the build here
// before any caller is affected.
var _ Encoder = (*DefaultEncoder)(nil)

// Encode assembles a GatewayRequest from in and appends its JSON
// representation into out. The pooled envelope is reset and released
// via defer so every code path returns the envelope to the pool
// exactly once. The out slice is never reallocated by this method
// beyond what sonic.EncodeInto does internally to grow the
// caller-supplied backing array when the envelope does not fit.
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

	if err := codec.MarshalInto(out, envelope); err != nil {
		return fmt.Errorf("proxy encoder marshal: %w", err)
	}
	return nil
}
