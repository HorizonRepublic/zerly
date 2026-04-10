// Package routing turns a registry snapshot into an HTTP method+path
// matching table. It is the only layer that understands path templates;
// everything upstream (proxy, transport) consumes Route values by reference.
package routing

// Route is the immutable descriptor of a single HTTP endpoint exposed by
// the gateway. It is produced by BuildTable from a registry.HandlerEntry
// and consumed by the proxy layer, which uses the Subject field to
// address the downstream NATS RPC and the PathTemplate as a cache key
// for logs and metrics.
//
// The struct is deliberately small and value-typed: routes are stored
// by value inside the linearTable bucket and copied out on each
// successful Lookup, which keeps the hot path allocation-free.
type Route struct {
	// Subject is the pre-computed NATS RPC subject (e.g.
	// "users-svc__microservice.cmd.users.list"). Pre-computing at build
	// time keeps the per-request hot path free of string concatenation
	// and lookups in the registry.
	Subject string

	// Method is the HTTP verb this route accepts, uppercased to match
	// the incoming request convention used by both Hertz and net/http.
	Method string

	// PathTemplate is the original template string with `:param`
	// placeholders (e.g. "/users/:id"). Retained on the Route so
	// downstream layers can log the matched template rather than the
	// raw request path — the template is a bounded-cardinality label,
	// the raw path is not.
	PathTemplate string
}
