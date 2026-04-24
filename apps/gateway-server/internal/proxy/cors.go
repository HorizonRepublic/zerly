package proxy

import (
	"strconv"
	"strings"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

// defaultExposedHeaders is the standard `Access-Control-Expose-Headers`
// list the gateway emits when a route's CORS config does not carry
// its own `ExposeHeaders` slice. Covers the headers the gateway itself
// stamps on every response, so cross-origin JavaScript can read
// correlators and rate-limit budgets without every operator having to
// remember to opt in.
//
// Comma-joined once at package init so the per-request write path is
// a single map assignment with no allocation.
const defaultExposedHeaders = "X-Request-Id, X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, Retry-After"

// MatchOrigin checks whether the request Origin is in the CORS
// allowed origins list. Returns the matched origin to echo back,
// or "" if no match. Wildcard "*" matches everything.
func MatchOrigin(cors *registry.CORSMeta, origin string) string {
	for _, allowed := range cors.Origins {
		if allowed == "*" {
			return "*"
		}

		if allowed == origin {
			return origin
		}
	}

	return ""
}

// BuildPreflightHeaders returns the full set of CORS headers for
// an OPTIONS preflight response (204 No Content).
func BuildPreflightHeaders(cors *registry.CORSMeta, matchedOrigin string) map[string]string {
	h := make(map[string]string, 6)

	h["Access-Control-Allow-Origin"] = matchedOrigin

	if len(cors.Methods) > 0 {
		h["Access-Control-Allow-Methods"] = strings.Join(cors.Methods, ", ")
	}

	if len(cors.Headers) > 0 {
		h["Access-Control-Allow-Headers"] = strings.Join(cors.Headers, ", ")
	}

	if cors.Credentials {
		h["Access-Control-Allow-Credentials"] = "true"
	}

	if cors.MaxAge > 0 {
		h["Access-Control-Max-Age"] = strconv.Itoa(cors.MaxAge)
	}

	if matchedOrigin != "*" {
		h["Vary"] = "Origin"
	}

	return h
}

// BuildResponseCORSHeaders returns CORS headers for a regular
// (non-preflight) response. Only origin, credentials, expose-headers,
// and vary — methods/headers/max-age are preflight-only per the CORS
// spec.
//
// Access-Control-Expose-Headers is emitted on every response so
// cross-origin JavaScript can read gateway-stamped correlators
// (X-Request-Id) and rate-limit budget headers (X-RateLimit-*,
// Retry-After). When the route's CORSMeta.ExposeHeaders is set the
// gateway emits exactly that list; otherwise the standard gateway
// default list applies.
func BuildResponseCORSHeaders(cors *registry.CORSMeta, matchedOrigin string) map[string]string {
	h := make(map[string]string, 4)

	h["Access-Control-Allow-Origin"] = matchedOrigin

	if cors.Credentials {
		h["Access-Control-Allow-Credentials"] = "true"
	}

	h["Access-Control-Expose-Headers"] = resolveExposeHeaders(cors.ExposeHeaders)

	if matchedOrigin != "*" {
		h["Vary"] = "Origin"
	}

	return h
}

// resolveExposeHeaders picks the Access-Control-Expose-Headers value
// the gateway should emit for a response. A non-empty per-route slice
// replaces the default list entirely (shallow replace, matching the
// other CORS fields' contract). Nil or empty falls back to the
// gateway's standard list.
func resolveExposeHeaders(configured []string) string {
	if len(configured) == 0 {
		return defaultExposedHeaders
	}

	return strings.Join(configured, ", ")
}
