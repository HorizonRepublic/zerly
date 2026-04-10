package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_AppliesDefaultsWhenOnlyRequiredSet(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.Equal(t, 10*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 30*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 120*time.Second, cfg.IdleTimeout)
	assert.Equal(t, int64(1048576), cfg.MaxBodyBytes)
	assert.Equal(t, 16384, cfg.MaxHeaderBytes)
	assert.True(t, cfg.EnableHTTP2)

	assert.True(t, cfg.NATSRandomizeUrls)
	assert.True(t, cfg.NATSDiscoverServers)
	assert.Equal(t, 1, cfg.NATSConnectionPool)
	assert.Equal(t, 2*time.Second, cfg.NATSReconnectWait)
	assert.Equal(t, -1, cfg.NATSMaxReconnects)

	assert.Equal(t, "handler_registry", cfg.KVBucket)
	assert.Equal(t, 5*time.Second, cfg.KVWatchTimeout)

	assert.Equal(t, 30*time.Second, cfg.RequestTimeout)
	assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout)

	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)
	assert.True(t, cfg.LogRequests)
	assert.False(t, cfg.LogRequestBody)
	assert.False(t, cfg.LogResponseBody)
	assert.Equal(t, 1000, cfg.LogSlowRequestMs)
	assert.Equal(t, 1, cfg.LogSamplingRate)

	assert.True(t, cfg.MetricsEnabled)
	assert.Equal(t, ":9090", cfg.MetricsAddr)
	assert.Equal(t, "/metrics", cfg.MetricsPath)
	assert.False(t, cfg.TracingEnabled)
	assert.InDelta(t, 0.01, cfg.TracingSampleRate, 1e-9)

	assert.True(t, cfg.HealthEnabled)
	assert.Equal(t, "/_gateway/live", cfg.HealthLivePath)
	assert.Equal(t, "/_gateway/ready", cfg.HealthReadyPath)

	assert.Equal(t, "production", cfg.Environment)
	assert.True(t, cfg.IsProduction())
}

func TestLoad_ParsesMultipleNATSUrls(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://n1:4222,nats://n2:4222,nats://n3:4222")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, []string{
		"nats://n1:4222",
		"nats://n2:4222",
		"nats://n3:4222",
	}, cfg.NATSUrls)
}

func TestLoad_FailsWithoutRequiredNATSUrls(t *testing.T) {
	original, wasSet := os.LookupEnv("NATS_URLS")
	require.NoError(t, os.Unsetenv("NATS_URLS"))
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv("NATS_URLS", original)
		}
	})

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NATS_URLS")
}

func TestIsProduction_FalseForNonProductionEnv(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")
	t.Setenv("ENVIRONMENT", "staging")

	cfg, err := Load()
	require.NoError(t, err)
	assert.False(t, cfg.IsProduction())
}

func TestLoad_HonorsCustomHTTPAddr(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")
	t.Setenv("HTTP_ADDR", ":9000")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ":9000", cfg.HTTPAddr)
}
