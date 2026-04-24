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

	assert.Equal(t, "open", cfg.RateLimitFailPolicy)
	assert.Equal(t, 10*time.Minute, cfg.RateLimitKeyTTL)

	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)
	assert.True(t, cfg.LogRequests)
	assert.False(t, cfg.LogRequestBody)
	assert.False(t, cfg.LogResponseBody)
	assert.Equal(t, 1000, cfg.LogSlowRequestMs)
	assert.Equal(t, 1, cfg.LogSamplingRate)

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

func TestLoad_TrustedProxies_DefaultsToPrivateSentinel(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "private", cfg.TrustedProxiesRaw,
		"raw env value preserved for diagnostics")
	require.Len(t, cfg.TrustedProxies, 7,
		"private sentinel expands to 7 CIDRs at Load() time")
}

func TestLoad_TrustedProxies_EmptyString_TrustsNothing(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")
	t.Setenv("TRUSTED_PROXIES", "")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Empty(t, cfg.TrustedProxies,
		"empty string means trust nothing — always use peer IP")
}

func TestLoad_TrustedProxies_LiteralCIDRList(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8,192.168.0.0/16")

	cfg, err := Load()
	require.NoError(t, err)

	require.Len(t, cfg.TrustedProxies, 2)
	assert.Equal(t, "10.0.0.0/8", cfg.TrustedProxies[0].String())
	assert.Equal(t, "192.168.0.0/16", cfg.TrustedProxies[1].String())
}

func TestLoad_TrustedProxies_InvalidCIDR_FailsStartupClosed(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")
	t.Setenv("TRUSTED_PROXIES", "garbage")

	_, err := Load()
	require.Error(t, err,
		"invalid CIDR must fail Load() so main.go aborts startup rather than running in an unsafe state")
	assert.Contains(t, err.Error(), "TRUSTED_PROXIES",
		"error must name the offending env var for operator diagnosis")
	assert.Contains(t, err.Error(), "garbage",
		"error must include the invalid value")
}

func TestLoad_RateLimitDefaults(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "open", cfg.RateLimitFailPolicy)
	assert.Equal(t, 10*time.Minute, cfg.RateLimitKeyTTL)
}

func TestLoad_RateLimitValidFailPolicyClosed(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")
	t.Setenv("RATELIMIT_FAIL_POLICY", "closed")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "closed", cfg.RateLimitFailPolicy)
}

func TestLoad_RateLimitInvalidFailPolicy(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")
	t.Setenv("RATELIMIT_FAIL_POLICY", "garbage")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RATELIMIT_FAIL_POLICY")
}

func TestLoad_RateLimitCustomKeyTTL(t *testing.T) {
	t.Setenv("NATS_URLS", "nats://localhost:4222")
	t.Setenv("RATELIMIT_KEY_TTL", "2m")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 2*time.Minute, cfg.RateLimitKeyTTL)
}
