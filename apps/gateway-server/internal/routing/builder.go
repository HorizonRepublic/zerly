package routing

import (
	"github.com/rs/zerolog"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/auth"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

// CollectRoutes projects a registry.Snapshot into a flat slice of
// HTTP-facing Route values. Routes that declare an `auth` block are
// resolved against the supplied VerifierRegistry; routes whose
// referenced verifier is missing (explicit id not found, or implicit
// default requested when no default verifier exists) are dropped
// from the output with a WARN log so the gateway never forwards
// unauthenticated requests to them.
//
// Skip policy:
//
//  1. Pure-RPC handlers (entry.HTTP == nil) are silently skipped,
//     same as before.
//  2. Malformed KV keys produce a WARN log and are skipped.
//  3. Routes whose auth block references an unknown verifier produce
//     a WARN log and are skipped. Once the verifier registers, the
//     next rebuild reinstates the route — this is the cold-boot race
//     self-healing documented in the design spec §10.3.
//
// The returned slice is in Go-map iteration order, i.e. effectively
// random. Callers that need a deterministic order must sort it
// themselves — the routing table does not care, and the lifecycle
// logger sorts its own output explicitly.
func CollectRoutes(
	snapshot *registry.Snapshot,
	verifiers *auth.VerifierRegistry,
	logger zerolog.Logger,
) []Route {
	routes := make([]Route, 0, len(snapshot.Entries))

	for key, entry := range snapshot.Entries {
		if entry.HTTP == nil {
			continue
		}

		subject, err := registry.SubjectFromKey(key)
		if err != nil {
			logger.Warn().
				Err(err).
				Str("key", key).
				Msg("routing: skipping entry with malformed KV key")

			continue
		}

		route := Route{
			Subject:      subject,
			Method:       entry.HTTP.Method,
			PathTemplate: entry.HTTP.Path,
		}

		if entry.Auth != nil {
			resolved, ok := resolveVerifier(entry.Auth, verifiers)
			if !ok {
				logger.Warn().
					Str("key", key).
					Str("verifier", entry.Auth.Verifier).
					Msg("routing: dropping route with unresolved verifier")

				continue
			}

			route.Auth = &RouteAuth{
				VerifierSubject: resolved,
				Optional:        entry.Auth.Optional,
			}
		}

		routes = append(routes, route)
	}

	return routes
}

// resolveVerifier looks up the NATS subject for the verifier a route
// references. An empty Verifier id means the route wants the default
// verifier — success requires one to exist in the registry.
func resolveVerifier(meta *registry.RouteAuthMeta, verifiers *auth.VerifierRegistry) (string, bool) {
	if meta.Verifier == "" {
		return verifiers.LookupDefault()
	}

	return verifiers.Lookup(meta.Verifier)
}

// BuildTableFromRoutes constructs a Table from a pre-collected slice
// of routes. It performs no snapshot access and emits no log output:
// the caller (typically BuildTable or the lifecycle-aware rebuild
// closure in main.go) is responsible for having already logged and
// filtered the input. Keeping this step purely mechanical makes it
// trivial to reason about in tests and benchmarks.
func BuildTableFromRoutes(routes []Route) Table {
	table := newLinearTable()
	for _, route := range routes {
		table.add(route)
	}

	return table
}

// BuildTable constructs a routing Table from a snapshot plus the
// pre-built verifier registry. Both projections must come from the
// same snapshot for the auth resolution to see consistent data;
// callers publish both atomically in the rebuild closure.
//
// The function never returns an error: partial builds are preferable
// to an unavailable gateway.
func BuildTable(
	snapshot *registry.Snapshot,
	verifiers *auth.VerifierRegistry,
	logger zerolog.Logger,
) Table {
	return BuildTableFromRoutes(CollectRoutes(snapshot, verifiers, logger))
}
