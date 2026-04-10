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
// data. The interface is deliberately narrow so fakes stub it in a few
// lines and so a future alternative encoder (e.g. protobuf) can swap in
// without touching Handler.
type Encoder interface {
	Encode(in *EncodeInput) ([]byte, error)
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

// Encode assembles a GatewayRequest from in and marshals it to JSON.
//
// The envelope is acquired from the pool, populated, marshalled, then
// returned — pool churn is hidden from callers. The returned byte
// slice is owned by the caller and lives beyond the pool release.
func (e *DefaultEncoder) Encode(in *EncodeInput) ([]byte, error) {
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

	out, err := codec.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("proxy encoder marshal: %w", err)
	}
	return out, nil
}
