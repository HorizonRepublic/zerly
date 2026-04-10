package routing

import "strings"

// linearTable is a simple linear-scan matcher over path templates. It is
// the MVP implementation, suitable for registries of tens to low
// hundreds of routes. Benchmarks in Milestone 22 decide whether this
// needs replacement with a proper trie; the Table interface is narrow
// enough that the swap is a one-file change.
//
// Concurrency: safe for concurrent readers after construction. Callers
// MUST publish a fully-built table atomically (e.g. via atomic.Pointer)
// rather than mutating an existing one. The add method is package-
// private and only used by BuildTable, which constructs the table once
// and never exposes a mid-build view.
type linearTable struct {
	routes []Route
}

// Compile-time assertion that linearTable satisfies the Table contract.
// Adding a new method to Table will fail the build here before any
// downstream caller even references the routing package, making the
// interface an enforced contract rather than a runtime assumption.
var _ Table = (*linearTable)(nil)

// newLinearTable returns an empty linearTable ready for BuildTable to
// populate via add. The returned value is not safe to share across
// goroutines until construction completes.
func newLinearTable() *linearTable {
	return &linearTable{routes: make([]Route, 0, 16)}
}

// add appends a Route to the internal bucket. It is package-private
// because callers must go through BuildTable — manual mutation would
// violate the "publish atomically, never mutate in place" invariant
// documented on linearTable.
func (t *linearTable) add(route Route) {
	t.routes = append(t.routes, route)
}

// Lookup walks the bucket in insertion order and returns the first
// Route whose method equals the request method AND whose path template
// matches the request path. Insertion order is preserved from the
// BuildTable iteration, which itself iterates the snapshot map — so
// callers MUST NOT rely on any specific ordering between routes that
// share a method and a prefix. In practice registries define at most
// one route per (method, template) pair, so order does not matter.
func (t *linearTable) Lookup(method, path string) (Route, map[string]string, bool) {
	for i := range t.routes {
		route := t.routes[i]
		if route.Method != method {
			continue
		}
		params, ok := matchTemplate(route.PathTemplate, path)
		if !ok {
			continue
		}
		return route, params, true
	}
	return Route{}, nil, false
}

// Methods returns the verbs registered for an EXACT template match.
// See the Table interface godoc for the rationale behind the exact-
// match simplification and its acceptable scope for the MVP.
func (t *linearTable) Methods(path string) []string {
	var methods []string
	for i := range t.routes {
		if t.routes[i].PathTemplate == path {
			methods = append(methods, t.routes[i].Method)
		}
	}
	return methods
}

// matchTemplate compares a path template (e.g. "/users/:id") against a
// concrete request path (e.g. "/users/42") and returns the extracted
// parameters on success. It returns a nil map and false on a mismatch.
//
// The function is intentionally naive: it splits on "/" and walks the
// segments once. No regex, no backtracking, no wildcard support. Exact-
// match routes (zero parameters) allocate nothing and return a nil
// params map; parameterized routes allocate the map lazily on the first
// ":param" segment encountered, sized to fit the remaining segments.
// This matches the zero-alloc hot-path goal in the design spec for the
// common case of static routes and keeps parameter extraction proportional
// to the number of parameters actually present.
func matchTemplate(template, path string) (map[string]string, bool) {
	templateSegments := splitPath(template)
	pathSegments := splitPath(path)

	if len(templateSegments) != len(pathSegments) {
		return nil, false
	}

	var params map[string]string
	for i, seg := range templateSegments {
		if strings.HasPrefix(seg, ":") {
			if params == nil {
				params = make(map[string]string, len(templateSegments)-i)
			}
			params[seg[1:]] = pathSegments[i]
			continue
		}
		if seg != pathSegments[i] {
			return nil, false
		}
	}
	return params, true
}

// splitPath slices a URL path into its non-empty segments. Leading and
// trailing slashes are ignored so that "/users" and "/users/" yield the
// same segment list — this matches the convention used by Hertz and
// net/http routers.
func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}
