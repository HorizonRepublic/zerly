package proxy

import (
	"strconv"
	"strings"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

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
// (non-preflight) response. Only origin, credentials, and vary —
// methods/headers/max-age are preflight-only per the CORS spec.
func BuildResponseCORSHeaders(cors *registry.CORSMeta, matchedOrigin string) map[string]string {
	h := make(map[string]string, 3)

	h["Access-Control-Allow-Origin"] = matchedOrigin

	if cors.Credentials {
		h["Access-Control-Allow-Credentials"] = "true"
	}

	if matchedOrigin != "*" {
		h["Vary"] = "Origin"
	}

	return h
}
