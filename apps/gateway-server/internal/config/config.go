// Package config loads and validates the zerly-gateway-server's
// operator-facing configuration from environment variables.
//
// All settings are read at process startup via
// github.com/caarlos0/env/v11. Missing required fields cause Load to return
// an error, which the caller MUST treat as fatal — starting the gateway
// with partial config would be far more dangerous than refusing to start.
// Hot-reload is not supported in the MVP; config changes require a pod
// restart, which is the expected operational model in Kubernetes rolling
// deployments.
//
// Per-endpoint configuration (HTTP method, path, statusCode) is NOT defined
// here. It lives in the handler_registry NATS KV bucket and is controlled
// by Nest services via the @ApiGateway decorator from @zerly/gateway-sdk.
// The split keeps infrastructure concerns (how the gateway talks to NATS,
// where it listens, how it logs) separate from application concerns
// (which HTTP routes map to which RPC subjects), letting platform teams
// and feature teams own each side independently.
package config

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/trustedproxy"
)

// Config is the complete set of operator-controlled gateway parameters.
//
// Fields are grouped by concern (HTTP, NATS, registry, request lifecycle,
// logging, observability, health, runtime). The groupings match the
// environment-variable prefixes documented on each field and are the
// contract operators use to configure a running gateway pod.
//
// The struct is loaded once at startup by Load. After that it is treated
// as effectively immutable — no code should mutate fields on a live
// Config instance, because doing so would create data races with the
// many components that hold references to it.
type Config struct {
	// HTTPAddr is the TCP listen address for the public HTTP server,
	// in Go's standard host:port form (empty host binds all interfaces).
	HTTPAddr string `env:"HTTP_ADDR"       envDefault:":8080"`
	// ReadTimeout bounds how long the server will wait for a full
	// request (headers + body) to arrive from the client.
	ReadTimeout time.Duration `env:"HTTP_READ_TIMEOUT"  envDefault:"10s"`
	// WriteTimeout bounds how long the server will take to write the
	// full response back to the client before forcibly closing.
	WriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	// IdleTimeout bounds how long a keep-alive connection may sit
	// between requests before the server closes it.
	IdleTimeout time.Duration `env:"HTTP_IDLE_TIMEOUT"  envDefault:"120s"`
	// MaxBodyBytes is the maximum accepted request body size in bytes.
	// Requests exceeding this are rejected with 413 Payload Too Large.
	MaxBodyBytes int64 `env:"HTTP_MAX_BODY_BYTES"   envDefault:"1048576"`
	// MaxHeaderBytes is the maximum accepted request header size in
	// bytes, summed across all headers.
	MaxHeaderBytes int `env:"HTTP_MAX_HEADER_BYTES" envDefault:"16384"`
	// EnableHTTP2 toggles h2c (HTTP/2 cleartext) support on the public
	// HTTP listener. Disable if sitting behind a proxy that terminates
	// HTTP/2 upstream.
	EnableHTTP2 bool `env:"HTTP_ENABLE_H2"        envDefault:"true"`

	// TrustedProxiesRaw is the operator-facing `TRUSTED_PROXIES` env
	// value kept verbatim for diagnostics (log dumps, future
	// /_gateway/config endpoint). Parsed into TrustedProxies by
	// Load(). Supported forms: "" (trust nothing), "private" (the
	// 7-range private-network sentinel), or a literal comma-separated
	// CIDR list (`"10.0.0.0/8,172.16.0.0/12"`).
	TrustedProxiesRaw string `env:"TRUSTED_PROXIES"`

	// TrustedProxies is the parsed CIDR list consumed by the HTTP
	// trusted-proxy middleware. Populated by Load() at startup; not
	// an env field (derived from TrustedProxiesRaw).
	TrustedProxies []*net.IPNet `env:"-"`

	// NATSUrls is the comma-separated list of NATS server URLs to
	// connect to. Supports a single URL, a static cluster list, or a
	// DNS-resolvable hostname (the nats.go client resolves A/SRV
	// records transparently). This is the only required field.
	NATSUrls []string `env:"NATS_URLS,required" envSeparator:","`
	// NATSRandomizeUrls shuffles NATSUrls before dialing to spread
	// initial connections across cluster nodes. Disable for
	// deterministic testing.
	NATSRandomizeUrls bool `env:"NATS_RANDOMIZE_URLS"    envDefault:"true"`
	// NATSDiscoverServers enables the client-side server-discovery
	// protocol so new cluster nodes are picked up without restart.
	NATSDiscoverServers bool `env:"NATS_DISCOVER_SERVERS"  envDefault:"true"`
	// NATSUser is the NATS username for password auth. Leave empty if
	// using creds-file or no auth.
	NATSUser string `env:"NATS_USER"`
	// NATSPassword is the NATS password for password auth. Leave empty
	// if using creds-file or no auth.
	NATSPassword string `env:"NATS_PASSWORD"`
	// NATSCredsFile is the filesystem path to an NKey credentials file,
	// used for NGS / decentralised JWT auth.
	NATSCredsFile string `env:"NATS_CREDS_FILE"`
	// NATSConnectionPool is the number of parallel NATS connections to
	// maintain. A value of 1 matches the nats.go default and is
	// correct for most workloads; raise only after benchmarking.
	NATSConnectionPool int `env:"NATS_CONNECTION_POOL"   envDefault:"1"`
	// NATSReconnectWait is the delay between reconnection attempts
	// after the NATS connection drops.
	NATSReconnectWait time.Duration `env:"NATS_RECONNECT_WAIT"    envDefault:"2s"`
	// NATSMaxReconnects is the cap on reconnection attempts before the
	// client gives up. A value of -1 means retry forever, which is the
	// right default for a gateway that must survive cluster restarts.
	NATSMaxReconnects int `env:"NATS_MAX_RECONNECTS"    envDefault:"-1"`
	// NATSReconnectBufSize is the in-memory buffer size (bytes) for
	// messages published while the connection is temporarily down.
	NATSReconnectBufSize int `env:"NATS_RECONNECT_BUFSIZE" envDefault:"8388608"`

	// KVBucket is the NATS KV bucket name the gateway watches for
	// handler registry entries.
	KVBucket string `env:"KV_BUCKET"       envDefault:"handler_registry"`
	// KVWatchTimeout bounds how long the initial KV watch hydration
	// can take before the gateway aborts startup.
	KVWatchTimeout time.Duration `env:"KV_WATCH_TIMEOUT" envDefault:"5s"`

	// RequestTimeout is the per-request hard deadline applied to the
	// full handler pipeline (RPC round-trip included).
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT"  envDefault:"30s"`
	// ShutdownTimeout bounds how long the graceful-shutdown sequence
	// waits for in-flight requests to finish before force-closing.
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"30s"`

	// LogLevel is the minimum zerolog level to emit. Valid values:
	// trace, debug, info, warn, error, fatal, panic, disabled.
	LogLevel string `env:"LOG_LEVEL"          envDefault:"info"`
	// LogFormat is the log output encoding: "json" for production or
	// "console" for human-friendly colored output in local dev.
	LogFormat string `env:"LOG_FORMAT"         envDefault:"json"`
	// LogRequests toggles access-log-style entries for each request.
	LogRequests bool `env:"LOG_REQUESTS"       envDefault:"true"`
	// LogRequestBody toggles inclusion of the full request body in
	// access log entries. Expensive and leaks PII; off by default.
	LogRequestBody bool `env:"LOG_REQUEST_BODY"   envDefault:"false"`
	// LogResponseBody toggles inclusion of the full response body in
	// access log entries. Expensive and leaks PII; off by default.
	LogResponseBody bool `env:"LOG_RESPONSE_BODY"  envDefault:"false"`
	// LogSlowRequestMs is the latency (in milliseconds) above which a
	// request is additionally logged at warn level regardless of
	// LogRequests.
	LogSlowRequestMs int `env:"LOG_SLOW_REQUEST_MS" envDefault:"1000"`
	// LogSamplingRate is the 1-in-N sampling rate for access logs when
	// LogRequests is enabled. 1 means log every request.
	LogSamplingRate int `env:"LOG_SAMPLING_RATE"  envDefault:"1"`

	// MetricsEnabled toggles the Prometheus metrics endpoint.
	MetricsEnabled bool `env:"METRICS_ENABLED"   envDefault:"true"`
	// MetricsAddr is the host:port the metrics endpoint listens on,
	// separate from the public HTTP listener so operators can firewall
	// it to scrape-only subnets.
	MetricsAddr string `env:"METRICS_ADDR"      envDefault:":9090"`
	// MetricsPath is the HTTP path served by the metrics endpoint.
	MetricsPath string `env:"METRICS_PATH"      envDefault:"/metrics"`
	// TracingEnabled toggles OpenTelemetry trace export.
	TracingEnabled bool `env:"TRACING_ENABLED"   envDefault:"false"`
	// TracingSampleRate is the head-based trace sampling ratio in the
	// [0.0, 1.0] range.
	TracingSampleRate float64 `env:"TRACING_SAMPLE_RATE" envDefault:"0.01"`

	// HealthEnabled toggles the built-in liveness and readiness
	// endpoints on the public listener.
	HealthEnabled bool `env:"HEALTH_ENABLED"    envDefault:"true"`
	// HealthLivePath is the HTTP path for the liveness probe.
	HealthLivePath string `env:"HEALTH_LIVE_PATH"  envDefault:"/_gateway/live"`
	// HealthReadyPath is the HTTP path for the readiness probe.
	HealthReadyPath string `env:"HEALTH_READY_PATH" envDefault:"/_gateway/ready"`

	// Environment is a free-form deployment-tier label ("production",
	// "staging", "development", ...). The gateway treats "production"
	// specially to redact sensitive error details from HTTP responses.
	Environment string `env:"ENVIRONMENT" envDefault:"production"`
}

// Load reads the configuration from environment variables, applying
// envDefault tags for optional fields and returning an error if any
// required field is missing or malformed.
//
// The only currently required field is NATS_URLS; every other knob has a
// sensible default suitable for local development. Callers are expected
// to treat any returned error as fatal and exit the process immediately
// rather than attempt partial startup.
//
// TrustedProxies are parsed from TRUSTED_PROXIES at startup; invalid
// CIDR input fails Load() with an error naming the offending value so
// startup aborts fail-closed. If TRUSTED_PROXIES is unset, it defaults
// to "private".
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse gateway config: %w", err)
	}

	// caarlos0/env v11 treats an explicitly empty env value the same
	// as unset and applies envDefault, which would silently turn
	// TRUSTED_PROXIES="" into "private" and break the contract that
	// "" means "trust nothing". LookupEnv distinguishes the two cases
	// so only a truly absent variable gets the default.
	if _, ok := os.LookupEnv("TRUSTED_PROXIES"); !ok {
		cfg.TrustedProxiesRaw = "private"
	}

	trusted, err := trustedproxy.ParseCIDRList(cfg.TrustedProxiesRaw)
	if err != nil {
		return nil, fmt.Errorf("parse TRUSTED_PROXIES=%q: %w",
			cfg.TrustedProxiesRaw, err)
	}
	cfg.TrustedProxies = trusted

	return cfg, nil
}

// IsProduction reports whether the gateway is running with Environment
// set to "production".
//
// Components that need to redact or hide sensitive data (stack traces,
// internal error messages, registry lookup failures) from HTTP responses
// consult this method rather than reading Environment directly, so the
// policy stays centralised and is easy to audit.
func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}
