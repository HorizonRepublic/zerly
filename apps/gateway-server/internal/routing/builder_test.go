package routing

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

func TestBuildTable_SkipsEntriesWithoutHTTP(t *testing.T) {
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"users-svc.cmd.users.internal": {HTTP: nil},
		},
	}

	table := BuildTable(snapshot, zerolog.Nop())
	_, _, ok := table.Lookup("GET", "/users")
	assert.False(t, ok)
}

func TestBuildTable_IncludesHTTPEntries(t *testing.T) {
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"users-svc.cmd.users.list": {
				HTTP: &registry.HTTPMeta{Method: "GET", Path: "/users"},
			},
			"users-svc.cmd.users.create": {
				HTTP: &registry.HTTPMeta{Method: "POST", Path: "/users"},
			},
		},
	}

	table := BuildTable(snapshot, zerolog.Nop())

	listRoute, _, ok := table.Lookup("GET", "/users")
	assert.True(t, ok)
	assert.Equal(t, "users-svc__microservice.cmd.users.list", listRoute.Subject)

	createRoute, _, ok := table.Lookup("POST", "/users")
	assert.True(t, ok)
	assert.Equal(t, "users-svc__microservice.cmd.users.create", createRoute.Subject)
}

func TestBuildTable_SkipsMalformedKeys(t *testing.T) {
	// The "broken-key" entry has no ".cmd." infix, so SubjectFromKey
	// returns an error. The builder must log-and-skip it rather than
	// failing the whole rebuild — a single bad KV entry should never
	// take the gateway offline.
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"broken-key": {
				HTTP: &registry.HTTPMeta{Method: "GET", Path: "/broken"},
			},
			"users-svc.cmd.users.list": {
				HTTP: &registry.HTTPMeta{Method: "GET", Path: "/users"},
			},
		},
	}

	table := BuildTable(snapshot, zerolog.Nop())

	// The malformed entry is absent.
	_, _, ok := table.Lookup("GET", "/broken")
	assert.False(t, ok)

	// The well-formed entry survived the skip.
	route, _, ok := table.Lookup("GET", "/users")
	assert.True(t, ok)
	assert.Equal(t, "users-svc__microservice.cmd.users.list", route.Subject)
}

func TestBuildTable_EmptySnapshot(t *testing.T) {
	// Empty snapshot is a real production state during warmup: the
	// gateway is up, the watcher has not yet received any KV events.
	// BuildTable must return a non-nil Table whose Lookup uniformly
	// returns ok=false rather than panicking on a nil map.
	snapshot := &registry.Snapshot{Entries: map[string]registry.HandlerEntry{}}

	table := BuildTable(snapshot, zerolog.Nop())
	assert.NotNil(t, table)

	_, _, ok := table.Lookup("GET", "/anything")
	assert.False(t, ok)
	assert.Empty(t, table.Methods("/anything"))
}
