package routing

import (
	"sort"

	"github.com/rs/zerolog"
)

// RouteDelta summarises the difference between two routing-table
// snapshots. Added holds routes present in the new slice but not in
// the old one; Removed holds the reverse. Routes present in both
// slices are counted by Unchanged only — they are not materialised
// because an operator reviewing logs after a production change
// cares about churn, not stability.
//
// Every slice is sorted lexicographically by "METHOD PATH" so log
// output is deterministic across rebuilds, which matters when
// diffing two log snapshots during an incident review.
type RouteDelta struct {
	Added     []Route
	Removed   []Route
	Unchanged int
}

// IsEmpty reports whether the delta represents a no-op rebuild —
// the new and old slices were identical. The caller uses this to
// demote the log level from INFO to DEBUG for no-op rebuilds so
// production log volume stays bounded when the KV bucket churns
// without actually changing the set of registered routes.
func (d RouteDelta) IsEmpty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0
}

// ComputeDelta returns the sorted delta between two route slices.
// Identity is determined by the (method, path-template) pair:
// changing a route's Subject alone is treated as an unchanged
// route because clients address routes by method+path, not by
// upstream subject.
//
// Pure function: both input slices are treated as immutable and
// the returned slices are freshly allocated. Safe to call
// concurrently.
func ComputeDelta(previous, next []Route) RouteDelta {
	prevByKey := make(map[string]Route, len(previous))
	for _, r := range previous {
		prevByKey[routeKey(r)] = r
	}

	nextByKey := make(map[string]Route, len(next))
	for _, r := range next {
		nextByKey[routeKey(r)] = r
	}

	var added []Route
	for key, route := range nextByKey {
		if _, existed := prevByKey[key]; !existed {
			added = append(added, route)
		}
	}

	var removed []Route
	for key, route := range prevByKey {
		if _, stillExists := nextByKey[key]; !stillExists {
			removed = append(removed, route)
		}
	}

	sortRoutes(added)
	sortRoutes(removed)

	unchanged := 0
	for key := range nextByKey {
		if _, existed := prevByKey[key]; existed {
			unchanged++
		}
	}

	return RouteDelta{
		Added:     added,
		Removed:   removed,
		Unchanged: unchanged,
	}
}

// LogInitialLoad emits a single INFO entry carrying the full set of
// routes that the gateway just hydrated for the first time. Called
// exactly once per process, from the first rebuild callback before
// the watcher starts streaming deltas. The full route list is
// intentionally dumped in one entry so a startup-time operator sees
// the complete topology in one log line rather than piecing it
// together from N per-route entries.
func LogInitialLoad(routes []Route, logger zerolog.Logger) {
	sorted := make([]Route, len(routes))
	copy(sorted, routes)
	sortRoutes(sorted)

	logger.Info().
		Int("count", len(sorted)).
		Array("routes", routesArray(sorted)).
		Msg("routing: initial route set published")
}

// LogDelta emits a log entry describing the transition from the
// previous route set to the new one. No-op rebuilds (IsEmpty) log
// at DEBUG; real changes log at INFO so production operators see
// every churn event without needing to raise the log level.
//
// The unchanged count is reported in both cases so the total active
// route count is always observable from a single entry — operators
// should not need to mentally add added+unchanged to answer "how
// many routes are live now".
func LogDelta(delta RouteDelta, logger zerolog.Logger) {
	total := len(delta.Added) + delta.Unchanged
	event := logger.Info()
	if delta.IsEmpty() {
		event = logger.Debug()
	}

	event.
		Int("added", len(delta.Added)).
		Int("removed", len(delta.Removed)).
		Int("unchanged", delta.Unchanged).
		Int("total", total).
		Array("added_routes", routesArray(delta.Added)).
		Array("removed_routes", routesArray(delta.Removed)).
		Msg("routing: table rebuilt")
}

// routeKey produces the identity string used to match routes
// across rebuilds. Subject is deliberately excluded so upstream
// subject renames do not show up as a remove-then-add pair.
func routeKey(r Route) string {
	return r.Method + " " + r.PathTemplate
}

// sortRoutes sorts a slice of routes in place by method+path so
// log output is deterministic between rebuilds that happen to see
// Go map iteration produce a different order.
func sortRoutes(routes []Route) {
	sort.Slice(routes, func(i, j int) bool {
		return routeKey(routes[i]) < routeKey(routes[j])
	})
}

// routesArray adapts a []Route into a zerolog.LogArrayMarshaler so
// every route is emitted as a JSON object with method/path/subject
// fields. Uses a zerolog Array so the resulting log entry is one
// structured field, not a string representation.
func routesArray(routes []Route) zerolog.LogArrayMarshaler {
	return &routeLogArray{routes: routes}
}

// routeLogArray is the zerolog.LogArrayMarshaler adapter for a
// slice of routes. Kept file-private and only constructed via
// routesArray — external consumers should never touch it.
type routeLogArray struct {
	routes []Route
}

// MarshalZerologArray implements zerolog.LogArrayMarshaler. Each
// route is written as a nested object so zerolog consumers can
// filter by "added_routes.path" or "removed_routes.subject"
// without parsing string fields.
func (a *routeLogArray) MarshalZerologArray(arr *zerolog.Array) {
	for _, route := range a.routes {
		arr.Dict(zerolog.Dict().
			Str("method", route.Method).
			Str("path", route.PathTemplate).
			Str("subject", route.Subject),
		)
	}
}
