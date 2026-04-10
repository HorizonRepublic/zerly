package routing

import (
	"github.com/rs/zerolog"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

// CollectRoutes projects a registry.Snapshot into a flat slice of
// HTTP-facing Route values. It is the first half of the two-step
// routing build pipeline; the second half, BuildTableFromRoutes,
// consumes this slice to construct the lookup table. Splitting the
// pipeline lets the lifecycle logger diff the flat slice across
// rebuilds before it gets hidden behind the opaque Table interface.
//
// Skip policy (identical to the old monolithic BuildTable):
//
//  1. Entries whose HTTP field is nil represent pure-RPC handlers that
//     the gateway does not expose; they are silently skipped (not logged
//     because a registry with hundreds of pure-RPC handlers would
//     otherwise flood the logs on every rebuild).
//  2. Entries whose KV key cannot be parsed into a NATS subject (see
//     registry.SubjectFromKey) are logged at warn level and skipped.
//     A single malformed entry MUST NOT take the whole gateway offline.
//
// The returned slice is in Go-map iteration order, i.e. effectively
// random. Callers that need a deterministic order must sort it
// themselves — the routing table does not care, and the lifecycle
// logger sorts its own output explicitly.
func CollectRoutes(snapshot *registry.Snapshot, logger zerolog.Logger) []Route {
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

		routes = append(routes, Route{
			Subject:      subject,
			Method:       entry.HTTP.Method,
			PathTemplate: entry.HTTP.Path,
		})
	}

	return routes
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

// BuildTable is a convenience wrapper around CollectRoutes and
// BuildTableFromRoutes preserved for callers and tests that do not
// need access to the intermediate []Route slice. It runs off the hot
// path, once per KV change event delivered by the watcher, and its
// output is published atomically by the caller via atomic.Value —
// there is no in-place mutation.
//
// The function never returns an error: partial builds are preferable
// to an unavailable gateway.
func BuildTable(snapshot *registry.Snapshot, logger zerolog.Logger) Table {
	return BuildTableFromRoutes(CollectRoutes(snapshot, logger))
}
