package routing

import (
	"github.com/rs/zerolog"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

// BuildTable is a pure function that projects a registry.Snapshot into
// a routing Table. It runs off the hot path, once per KV change event
// delivered by the watcher, and its output is published atomically by
// the caller via atomic.Pointer[Table] — there is no in-place mutation.
//
// Skip policy:
//
//  1. Entries whose HTTP field is nil represent pure-RPC handlers that
//     the gateway does not expose; they are silently skipped (not logged
//     because a registry with hundreds of pure-RPC handlers would
//     otherwise flood the logs on every rebuild).
//  2. Entries whose KV key cannot be parsed into a NATS subject (see
//     registry.SubjectFromKey) are logged at warn level and skipped.
//     A single malformed entry MUST NOT take the whole gateway offline.
//
// The function never returns an error: partial builds are preferable to
// an unavailable gateway.
func BuildTable(snapshot *registry.Snapshot, logger zerolog.Logger) Table {
	table := newLinearTable()

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

		table.add(Route{
			Subject:      subject,
			Method:       entry.HTTP.Method,
			PathTemplate: entry.HTTP.Path,
		})
	}

	return table
}
