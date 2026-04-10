// Package registry mirrors the nestjs-jetstream handler_registry NATS KV
// bucket into an in-memory snapshot. It is the single source of truth for
// the gateway's HTTP routing table — the routing layer reads entries, the
// proxy layer uses the stored metadata to build the NATS RPC subject, and
// the watcher keeps the snapshot in sync with KV changes as they happen.
//
// The KV contract is: keys formatted as "{service}.cmd.{pattern}", values
// JSON-encoded with an "http" sub-object when the handler is exposed to
// the gateway. Entries without an "http" field are pure-RPC handlers that
// remain invisible to the HTTP routing table but are still watched and
// stored for completeness.
package registry

// HTTPMeta is the HTTP-routing descriptor stored under "meta.http" (wire
// shape) or "http" (at-rest KV value) in a handler_registry entry.
//
// This struct mirrors the IGatewayHttpMeta interface published by
// @zerly/gateway-sdk. Any field addition, rename, or removal is a breaking
// change for BOTH packages and requires a synchronized release. The design
// spec §4.2 documents the extensibility policy: new optional fields may be
// added without a major version bump, but both sides must tolerate unknown
// fields gracefully (Go's encoding/json does this by default).
type HTTPMeta struct {
	// Method is the HTTP verb (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS)
	// the gateway accepts for this handler.
	Method string `json:"method"`

	// Path is the URL path template with `:param` placeholders.
	// Example: "/users/:id".
	Path string `json:"path"`

	// StatusCode is the HTTP status returned on a successful reply. When
	// nil, the gateway applies the default rules: 200 for non-null body,
	// 204 for void. Stored as a pointer so the zero value is distinguishable
	// from an explicit 0 (which no HTTP implementation accepts anyway, but
	// the pointer keeps the JSON omitempty semantics clean).
	StatusCode *int `json:"statusCode,omitempty"`
}

// HandlerEntry is a single deserialized record from the handler_registry
// KV bucket.
//
// Entries without an HTTP field represent pure-RPC handlers that the
// gateway does not expose. The watcher still stores them (so future
// features — health checks, debug dashboards, service discovery — can
// read arbitrary metadata) but the routing table build step in
// `routing.BuildTable` skips them.
//
// Unknown JSON fields in the KV value are silently ignored by Go's
// default json unmarshal behavior. This is intentional: it keeps the
// gateway forward-compatible with future nestjs-jetstream metadata
// extensions (auth descriptors, rate-limit rules, schema references, etc.)
// without requiring a gateway upgrade in lockstep.
type HandlerEntry struct {
	HTTP *HTTPMeta `json:"http,omitempty"`
}
