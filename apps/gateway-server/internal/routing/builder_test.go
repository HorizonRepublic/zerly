package routing

import (
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/auth"
	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

func silentLogger() zerolog.Logger {
	return zerolog.New(io.Discard).Level(zerolog.Disabled)
}

func emptyVerifiers() *auth.VerifierRegistry {
	return auth.BuildVerifierRegistry(
		&registry.Snapshot{Entries: map[string]registry.HandlerEntry{}},
		silentLogger(),
	)
}

func TestBuildTable_SkipsEntriesWithoutHTTP(t *testing.T) {
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"users-svc.cmd.users.internal": {HTTP: nil},
		},
	}

	table := BuildTable(snapshot, emptyVerifiers(), silentLogger())
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

	table := BuildTable(snapshot, emptyVerifiers(), silentLogger())

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

	table := BuildTable(snapshot, emptyVerifiers(), silentLogger())

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

	table := BuildTable(snapshot, emptyVerifiers(), silentLogger())
	assert.NotNil(t, table)

	_, _, ok := table.Lookup("GET", "/anything")
	assert.False(t, ok)
	assert.Empty(t, table.Methods("/anything"))
}

func TestCollectRoutes_ResolvesExplicitVerifier(t *testing.T) {
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"users-svc.cmd.users.me": {
				HTTP: &registry.HTTPMeta{Method: "GET", Path: "/users/me"},
				Auth: &registry.RouteAuthMeta{Verifier: "jwt"},
			},
			"users-svc.cmd.auth.verifier.jwt": {
				Verifier: &registry.VerifierMeta{ID: "jwt", Default: true},
			},
		},
	}
	verifiers := auth.BuildVerifierRegistry(snapshot, silentLogger())

	routes := CollectRoutes(snapshot, verifiers, silentLogger())

	require.Len(t, routes, 1)
	require.NotNil(t, routes[0].Auth)
	assert.Equal(t, "users-svc__microservice.cmd.auth.verifier.jwt", routes[0].Auth.VerifierSubject)
	assert.False(t, routes[0].Auth.Optional)
}

func TestCollectRoutes_UsesDefaultVerifierWhenRouteOmitsId(t *testing.T) {
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"users-svc.cmd.users.me": {
				HTTP: &registry.HTTPMeta{Method: "GET", Path: "/users/me"},
				Auth: &registry.RouteAuthMeta{}, // empty Verifier → default
			},
			"users-svc.cmd.auth.verifier.jwt": {
				Verifier: &registry.VerifierMeta{ID: "jwt", Default: true},
			},
		},
	}
	verifiers := auth.BuildVerifierRegistry(snapshot, silentLogger())

	routes := CollectRoutes(snapshot, verifiers, silentLogger())

	require.Len(t, routes, 1)
	require.NotNil(t, routes[0].Auth)
	assert.Equal(t, "users-svc__microservice.cmd.auth.verifier.jwt", routes[0].Auth.VerifierSubject)
}

func TestCollectRoutes_OptionalAuthPreservesFlag(t *testing.T) {
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"users-svc.cmd.articles.get": {
				HTTP: &registry.HTTPMeta{Method: "GET", Path: "/articles/:id"},
				Auth: &registry.RouteAuthMeta{Verifier: "jwt", Optional: true},
			},
			"users-svc.cmd.auth.verifier.jwt": {
				Verifier: &registry.VerifierMeta{ID: "jwt"},
			},
		},
	}
	verifiers := auth.BuildVerifierRegistry(snapshot, silentLogger())

	routes := CollectRoutes(snapshot, verifiers, silentLogger())

	require.Len(t, routes, 1)
	require.NotNil(t, routes[0].Auth)
	assert.True(t, routes[0].Auth.Optional)
}

func TestCollectRoutes_DropsRouteWithUnknownVerifier(t *testing.T) {
	// Route references verifier 'jwt' but no such verifier is
	// registered. The route must be excluded from the routing table;
	// matching HTTP requests return 404 until the verifier registers.
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"users-svc.cmd.users.me": {
				HTTP: &registry.HTTPMeta{Method: "GET", Path: "/users/me"},
				Auth: &registry.RouteAuthMeta{Verifier: "jwt"},
			},
		},
	}
	verifiers := auth.BuildVerifierRegistry(snapshot, silentLogger())

	routes := CollectRoutes(snapshot, verifiers, silentLogger())

	assert.Empty(t, routes)
}

func TestCollectRoutes_DropsRouteWithImplicitDefaultWhenNoDefaultRegistered(t *testing.T) {
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"users-svc.cmd.users.me": {
				HTTP: &registry.HTTPMeta{Method: "GET", Path: "/users/me"},
				Auth: &registry.RouteAuthMeta{}, // implicit default
			},
			// Verifier exists but does NOT set Default:true.
			"users-svc.cmd.auth.verifier.jwt": {
				Verifier: &registry.VerifierMeta{ID: "jwt"},
			},
		},
	}
	verifiers := auth.BuildVerifierRegistry(snapshot, silentLogger())

	routes := CollectRoutes(snapshot, verifiers, silentLogger())

	assert.Empty(t, routes)
}

func TestCollectRoutes_PublicRouteUnaffectedByVerifierRegistry(t *testing.T) {
	// Regression: routes without an Auth block must still land in the
	// table even when the verifier registry is empty.
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"users-svc.cmd.healthcheck": {
				HTTP: &registry.HTTPMeta{Method: "GET", Path: "/health"},
			},
		},
	}
	verifiers := auth.BuildVerifierRegistry(snapshot, silentLogger())

	routes := CollectRoutes(snapshot, verifiers, silentLogger())

	require.Len(t, routes, 1)
	assert.Nil(t, routes[0].Auth)
}

func TestCollectRoutes_PropagatesExtendedFields(t *testing.T) {
	timeout := 5000
	snapshot := &registry.Snapshot{
		Entries: map[string]registry.HandlerEntry{
			"svc__microservice.cmd.users.create": {
				HTTP:      &registry.HTTPMeta{Method: "POST", Path: "/users"},
				CORS:      &registry.CORSMeta{Origins: []string{"https://app.com"}, Credentials: true},
				RateLimit: &registry.RateLimitMeta{RPS: 10, Burst: 20, KeyBy: []string{"ip"}},
				Headers:   map[string]string{"x-frame-options": "DENY"},
				Timeout:   &timeout,
			},
		},
	}

	routes := CollectRoutes(snapshot, emptyVerifiers(), silentLogger())

	require.Len(t, routes, 1)
	route := routes[0]

	require.NotNil(t, route.CORS)
	assert.Equal(t, []string{"https://app.com"}, route.CORS.Origins)
	assert.True(t, route.CORS.Credentials)

	require.NotNil(t, route.RateLimit)
	assert.Equal(t, 10, route.RateLimit.RPS)

	assert.Equal(t, "DENY", route.Headers["x-frame-options"])
	assert.Equal(t, 5*time.Second, route.Timeout)
}
