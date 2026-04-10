package routing

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeDelta_EmptyToSome(t *testing.T) {
	next := []Route{
		{Subject: "svc.cmd.a", Method: "GET", PathTemplate: "/a"},
		{Subject: "svc.cmd.b", Method: "POST", PathTemplate: "/b"},
	}

	delta := ComputeDelta(nil, next)

	assert.Len(t, delta.Added, 2)
	assert.Empty(t, delta.Removed)
	assert.Equal(t, 0, delta.Unchanged)
	assert.False(t, delta.IsEmpty())
}

func TestComputeDelta_SomeToEmpty(t *testing.T) {
	previous := []Route{
		{Subject: "svc.cmd.a", Method: "GET", PathTemplate: "/a"},
		{Subject: "svc.cmd.b", Method: "POST", PathTemplate: "/b"},
	}

	delta := ComputeDelta(previous, nil)

	assert.Empty(t, delta.Added)
	assert.Len(t, delta.Removed, 2)
	assert.Equal(t, 0, delta.Unchanged)
	assert.False(t, delta.IsEmpty())
}

func TestComputeDelta_NoOp(t *testing.T) {
	routes := []Route{
		{Subject: "svc.cmd.a", Method: "GET", PathTemplate: "/a"},
		{Subject: "svc.cmd.b", Method: "POST", PathTemplate: "/b"},
	}

	delta := ComputeDelta(routes, routes)

	assert.Empty(t, delta.Added)
	assert.Empty(t, delta.Removed)
	assert.Equal(t, 2, delta.Unchanged)
	assert.True(t, delta.IsEmpty())
}

func TestComputeDelta_AddRemove(t *testing.T) {
	previous := []Route{
		{Subject: "svc.cmd.a", Method: "GET", PathTemplate: "/a"},
		{Subject: "svc.cmd.b", Method: "GET", PathTemplate: "/b"},
	}
	next := []Route{
		{Subject: "svc.cmd.b", Method: "GET", PathTemplate: "/b"},
		{Subject: "svc.cmd.c", Method: "GET", PathTemplate: "/c"},
	}

	delta := ComputeDelta(previous, next)

	require.Len(t, delta.Added, 1)
	assert.Equal(t, "/c", delta.Added[0].PathTemplate)

	require.Len(t, delta.Removed, 1)
	assert.Equal(t, "/a", delta.Removed[0].PathTemplate)

	assert.Equal(t, 1, delta.Unchanged)
	assert.False(t, delta.IsEmpty())
}

func TestComputeDelta_IgnoresSubjectRename(t *testing.T) {
	// Deliberate invariant: identity is (method, path) only. An
	// upstream subject rename must surface as zero churn so operators
	// are not paged over a purely internal refactor.
	previous := []Route{
		{Subject: "svc.cmd.a.old", Method: "GET", PathTemplate: "/a"},
	}
	next := []Route{
		{Subject: "svc.cmd.a.new", Method: "GET", PathTemplate: "/a"},
	}

	delta := ComputeDelta(previous, next)

	assert.Empty(t, delta.Added)
	assert.Empty(t, delta.Removed)
	assert.Equal(t, 1, delta.Unchanged)
	assert.True(t, delta.IsEmpty())
}

func TestComputeDelta_SortsDeterministically(t *testing.T) {
	previous := []Route{
		{Subject: "svc.cmd.z", Method: "GET", PathTemplate: "/z"},
		{Subject: "svc.cmd.y", Method: "GET", PathTemplate: "/y"},
		{Subject: "svc.cmd.x", Method: "GET", PathTemplate: "/x"},
	}
	next := []Route{
		{Subject: "svc.cmd.c", Method: "POST", PathTemplate: "/c"},
		{Subject: "svc.cmd.a", Method: "GET", PathTemplate: "/a"},
		{Subject: "svc.cmd.b", Method: "DELETE", PathTemplate: "/b"},
	}

	delta := ComputeDelta(previous, next)

	require.Len(t, delta.Added, 3)
	assert.Equal(t, "/b", delta.Added[0].PathTemplate)
	assert.Equal(t, "DELETE", delta.Added[0].Method)
	assert.Equal(t, "/a", delta.Added[1].PathTemplate)
	assert.Equal(t, "GET", delta.Added[1].Method)
	assert.Equal(t, "/c", delta.Added[2].PathTemplate)
	assert.Equal(t, "POST", delta.Added[2].Method)

	require.Len(t, delta.Removed, 3)
	assert.Equal(t, "/x", delta.Removed[0].PathTemplate)
	assert.Equal(t, "/y", delta.Removed[1].PathTemplate)
	assert.Equal(t, "/z", delta.Removed[2].PathTemplate)
}

func TestRouteDelta_IsEmpty_TrueWhenBothZero(t *testing.T) {
	delta := RouteDelta{Unchanged: 42}
	assert.True(t, delta.IsEmpty())
}

func TestRouteDelta_IsEmpty_FalseWhenAdded(t *testing.T) {
	delta := RouteDelta{
		Added: []Route{{Method: "GET", PathTemplate: "/new"}},
	}
	assert.False(t, delta.IsEmpty())
}

func TestRouteDelta_IsEmpty_FalseWhenRemoved(t *testing.T) {
	delta := RouteDelta{
		Removed: []Route{{Method: "GET", PathTemplate: "/gone"}},
	}
	assert.False(t, delta.IsEmpty())
}

func TestLogInitialLoad_DoesNotPanic(t *testing.T) {
	routes := []Route{
		{Subject: "svc.cmd.a", Method: "GET", PathTemplate: "/a"},
		{Subject: "svc.cmd.b", Method: "POST", PathTemplate: "/b"},
	}

	assert.NotPanics(t, func() {
		LogInitialLoad(routes, zerolog.Nop())
	})
}

func TestLogDelta_DoesNotPanic(t *testing.T) {
	delta := RouteDelta{
		Added: []Route{
			{Subject: "svc.cmd.new", Method: "GET", PathTemplate: "/new"},
		},
		Removed: []Route{
			{Subject: "svc.cmd.old", Method: "GET", PathTemplate: "/old"},
		},
		Unchanged: 3,
	}

	assert.NotPanics(t, func() {
		LogDelta(delta, zerolog.Nop())
	})

	assert.NotPanics(t, func() {
		LogDelta(RouteDelta{Unchanged: 5}, zerolog.Nop())
	})
}
