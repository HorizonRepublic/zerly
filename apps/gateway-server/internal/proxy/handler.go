package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/rs/zerolog"

	gerrors "github.com/HorizonRepublic/zerly/apps/gateway-server/internal/errors"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/ratelimit"
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
// at construction; all fields are required (except RateLimiter) and the
// zero value of a HandlerConfig is NOT safe to use.
type HandlerConfig struct {
	Table   TableProvider
	Nats    NatsRequester
	Encoder Encoder
	Decoder Decoder
	Timeout time.Duration
	Logger  zerolog.Logger
	// RateLimiter is the per-route store router. nil = rate limiting
	// disabled globally for this handler. Backends are registered via
	// Router.EnsureBackend by the gateway bootstrap.
	RateLimiter *ratelimit.Router
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
//
// Headers is a multi-value map so the HTTP adapter can emit each
// slice entry as a separate header on the client response. This is
// the critical shape for Set-Cookie, where two cookies set by the
// same handler MUST land on the wire as two distinct Set-Cookie
// lines instead of a single joined value.
type ServeResult struct {
	Status  int
	Headers map[string][]string
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

	if in.Method == "OPTIONS" {
		return h.handlePreflight(table, in)
	}

	route, params, ok := table.Lookup(in.Method, in.Path)
	if !ok {
		return toServeResult(gerrors.NotFound)
	}

	var claims json.RawMessage
	var authHeaders map[string][]string

	timeout := h.cfg.Timeout
	if route.Timeout > 0 {
		timeout = route.Timeout
	}

	if route.Auth != nil {
		authOutcome := h.runAuthFlow(in, route, params, timeout)
		if !authOutcome.Proceed {
			return authOutcome.ShortCircuit
		}

		claims = authOutcome.Claims
		authHeaders = authOutcome.AuthHeaders
	}

	var rlHeaders map[string]string

	// GCRA contract: rps >= 1. Treat rps <= 0 as "no RL" — fail safe.
	if route.RateLimit != nil && route.RateLimit.RPS > 0 && h.cfg.RateLimiter != nil {
		rlKey := h.resolveRateLimitKey(in, route, claims)

		burst := route.RateLimit.Burst
		if burst == 0 {
			burst = route.RateLimit.RPS * 2
		}

		fullKey := ratelimit.BuildBucketKey(route.Method, route.PathTemplate, rlKey)
		store := h.cfg.RateLimiter.StoreFor(route)

		rlCtx, cancel := context.WithTimeout(context.Background(), timeout)
		decision, rlErr := store.Allow(rlCtx, fullKey, route.RateLimit.RPS, burst)
		cancel()

		allowed := decision.Allowed
		if rlErr != nil {
			allowed = h.cfg.RateLimiter.FailPolicy().Apply(rlErr, route, fullKey, h.cfg.Logger)
		}

		rlHeaders = ratelimit.BuildHeaders(route.RateLimit, decision)

		if !allowed {
			result := toServeResult(gerrors.TooManyRequests)
			for k, v := range rlHeaders {
				result.Headers[k] = []string{v}
			}

			return result
		}
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
		TimeoutMs:   timeout.Milliseconds(),
		Auth:        claims,
	})
	if err != nil {
		h.cfg.Logger.Error().Err(err).Msg("proxy encode failed")
		return toServeResult(gerrors.InternalError)
	}

	replyBytes, err := h.cfg.Nats.Request(route.Subject, *payload, timeout)
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

	mergedHeaders := mergeHeaders(reply.Headers, in.RequestID)
	mergeAuthHeaders(mergedHeaders, authHeaders)

	for k, v := range rlHeaders {
		if _, exists := mergedHeaders[k]; !exists {
			mergedHeaders[k] = []string{v}
		}
	}

	for k, v := range route.Headers {
		if _, exists := mergedHeaders[k]; !exists {
			mergedHeaders[k] = []string{v}
		}
	}

	if route.CORS != nil {
		origin := in.Headers["origin"]
		if matched := MatchOrigin(route.CORS, origin); matched != "" {
			for k, v := range BuildResponseCORSHeaders(route.CORS, matched) {
				mergedHeaders[k] = []string{v}
			}
		}
	}

	return &ServeResult{
		Status:  reply.Status,
		Headers: mergedHeaders,
		Body:    reply.Body,
	}
}

// handlePreflight handles CORS OPTIONS preflight requests. It uses the
// Access-Control-Request-Method header to find the actual route, then
// returns 204 with the appropriate CORS headers if the origin matches.
func (h *Handler) handlePreflight(table routing.Table, in *ServeInput) *ServeResult {
	acrm := in.Headers["access-control-request-method"]
	if acrm == "" {
		return toServeResult(gerrors.NotFound)
	}

	route, _, ok := table.Lookup(strings.ToUpper(acrm), in.Path)
	if !ok || route.CORS == nil {
		return toServeResult(gerrors.NotFound)
	}

	origin := in.Headers["origin"]
	matched := MatchOrigin(route.CORS, origin)
	if matched == "" {
		return toServeResult(gerrors.NotFound)
	}

	preflight := BuildPreflightHeaders(route.CORS, matched)

	headers := make(map[string][]string, len(preflight))
	for k, v := range preflight {
		headers[k] = []string{v}
	}

	return &ServeResult{Status: 204, Headers: headers}
}

// resolveRateLimitKey computes the rate-limit bucket key from the
// route's keyBy chain, falling back to clientIP if nothing resolves.
func (h *Handler) resolveRateLimitKey(
	in *ServeInput,
	route routing.Route,
	claims json.RawMessage,
) string {
	keyBy := route.RateLimit.KeyBy
	if len(keyBy) == 0 {
		keyBy = []string{"ip"}
	}

	var claimsMap map[string]any
	if len(claims) > 0 {
		_ = json.Unmarshal(claims, &claimsMap)
	}

	return ratelimit.ResolveKey(
		keyBy,
		in.RemoteAddr,
		func(name string) string { return in.Headers[name] },
		func(name string) string { return extractCookie(in.Headers, name) },
		claimsMap,
	)
}

// extractCookie parses a single named cookie from the Cookie header.
// Avoids allocating a full cookie map per request — most rate-limit
// keyBy chains resolve before reaching the cookie strategy.
func extractCookie(headers map[string]string, name string) string {
	cookieHeader := headers["cookie"]
	if cookieHeader == "" {
		return ""
	}

	for _, part := range strings.Split(cookieHeader, ";") {
		part = strings.TrimSpace(part)
		if eqIdx := strings.IndexByte(part, '='); eqIdx > 0 && part[:eqIdx] == name {
			return part[eqIdx+1:]
		}
	}

	return ""
}

// authFlowResult captures the outcome of a pre-flight verifier
// sub-request. Exactly one of ShortCircuit and the (Claims,
// AuthHeaders) pair is populated:
//
//   - When Proceed is false, ShortCircuit holds the response that
//     must be returned to the HTTP client verbatim; Claims and
//     AuthHeaders are zero.
//   - When Proceed is true, the caller injects Claims as the
//     `auth` field of the main request envelope and merges
//     AuthHeaders into the main route reply headers via
//     mergeAuthHeaders before the HTTP write.
type authFlowResult struct {
	Proceed      bool
	ShortCircuit *ServeResult
	Claims       json.RawMessage
	AuthHeaders  map[string][]string
}

// runAuthFlow issues the verifier sub-request for a protected route
// and decides whether the main route request proceeds.
//
// The returned authFlowResult is discriminated on Proceed:
//
//   - Proceed == false → the caller MUST return ShortCircuit
//     verbatim. Covers 401/403 short-circuits, verifier transport
//     errors (503/504/502), and decoder failures.
//   - Proceed == true → the caller continues to the main route
//     request. Claims carries the verifier's reply body for
//     injection into the main envelope's auth field (nil on the
//     optional-auth 401 swallow). AuthHeaders carries the verifier
//     reply's response headers for merge into the main reply — only
//     set on a 200 verifier reply so failed verifier replies never
//     leak headers onto the main response.
func (h *Handler) runAuthFlow(
	in *ServeInput,
	route routing.Route,
	params map[string]string,
	timeout time.Duration,
) *authFlowResult {
	verifyPayload := acquirePayload()
	defer releasePayload(verifyPayload)

	// The verify-request envelope is identical to the main envelope
	// except Body is always nil — verifiers never see the request
	// body by design. Auth decisions must stay independent of body
	// content so the verifier path can be cached without cache-key
	// explosion on body variance.
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
		TimeoutMs:   timeout.Milliseconds(),
	})
	if err != nil {
		h.cfg.Logger.Error().Err(err).Msg("auth: verify encode failed")
		return &authFlowResult{Proceed: false, ShortCircuit: toServeResult(gerrors.InternalError)}
	}

	replyBytes, err := h.cfg.Nats.Request(route.Auth.VerifierSubject, *verifyPayload, timeout)
	if err != nil {
		if isTimeoutErr(err) {
			return &authFlowResult{Proceed: false, ShortCircuit: toServeResult(gerrors.GatewayTimeout)}
		}
		h.cfg.Logger.Error().
			Err(err).
			Str("subject", route.Auth.VerifierSubject).
			Msg("auth: verifier nats request failed")

		return &authFlowResult{Proceed: false, ShortCircuit: toServeResult(gerrors.ServiceUnavailable)}
	}

	reply, err := h.cfg.Decoder.Decode(replyBytes)
	if err != nil {
		h.cfg.Logger.Error().Err(err).Msg("auth: verifier reply decode failed")
		return &authFlowResult{Proceed: false, ShortCircuit: toServeResult(gerrors.BadGateway)}
	}

	if reply.Status == 200 {
		return &authFlowResult{
			Proceed:     true,
			Claims:      reply.Body,
			AuthHeaders: reply.Headers,
		}
	}

	// Optional-auth: swallow 401 only. 403 and every other non-200
	// status still short-circuits. Transport errors above already
	// returned before this branch. Verifier headers are intentionally
	// dropped on this path — only 200-path verifier replies
	// contribute headers to the main response.
	if route.Auth.Optional && reply.Status == 401 {
		return &authFlowResult{Proceed: true}
	}

	// Forward the verifier's reply verbatim — this is how verifier-set
	// headers like WWW-Authenticate reach the client.
	return &authFlowResult{
		Proceed: false,
		ShortCircuit: &ServeResult{
			Status:  reply.Status,
			Headers: mergeHeaders(reply.Headers, in.RequestID),
			Body:    reply.Body,
		},
	}
}

// mergeHeaders combines the reply headers with gateway-owned defaults.
// Multi-value entries from the reply (e.g. multiple Set-Cookie lines)
// are forwarded verbatim so RFC-mandated multi-value headers survive
// the wire. The gateway always stamps its own x-request-id on top of
// whatever the upstream service emitted, so a compromised upstream
// cannot forge correlator ids.
func mergeHeaders(reply map[string][]string, requestID string) map[string][]string {
	out := make(map[string][]string, len(reply)+2)
	out["content-type"] = []string{"application/json"}
	for k, v := range reply {
		out[k] = v
	}
	out["x-request-id"] = []string{requestID}
	return out
}

// mergeAuthHeaders layers a verifier reply's response headers onto
// an already-merged main reply headers map using these rules:
//
//   - set-cookie is appended with verifier values first, then the
//     route's existing values — so the client sees the verifier's
//     rotated cookies alongside any cookies the main handler set,
//     in a stable order that matches the canonical auth contract.
//   - Other headers from the verifier are added only when the
//     merged map does not already contain the key. The main route
//     reply (and the gateway's x-request-id / content-type stamps
//     from mergeHeaders) own conflicting single-value slots
//     unconditionally, so verifier headers never overwrite gateway
//     state or silently shadow a route-chosen value.
//
// The merged map is mutated in place. Callers are expected to have
// already run mergeHeaders so gateway defaults are baked in.
func mergeAuthHeaders(merged map[string][]string, authHeaders map[string][]string) {
	if len(authHeaders) == 0 {
		return
	}

	for verifierKey, verifierValues := range authHeaders {
		if len(verifierValues) == 0 {
			continue
		}

		if verifierKey == "set-cookie" {
			existing := merged["set-cookie"]
			combined := make([]string, 0, len(verifierValues)+len(existing))
			combined = append(combined, verifierValues...)
			combined = append(combined, existing...)
			merged["set-cookie"] = combined

			continue
		}

		if _, exists := merged[verifierKey]; exists {
			continue
		}

		merged[verifierKey] = verifierValues
	}
}

// jsonHeaders returns a fresh header map carrying only content-type.
// Allocates on every call because ServeResult.Headers is expected to
// be caller-owned — the shared pre-encoded error body is paired with
// this per-request header map so no caller ever sees aliased state.
func jsonHeaders() map[string][]string {
	return map[string][]string{"content-type": {"application/json"}}
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
