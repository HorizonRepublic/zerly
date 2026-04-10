package nats

import (
	"fmt"
	"strings"

	natsgo "github.com/nats-io/nats.go"
	"github.com/rs/zerolog"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/config"
)

// Connect establishes a NATS connection using cfg and returns a live
// *nats.Conn. The comma-separated URL list supports clustered, single-
// node, and DNS-based discovery transparently — nats.go resolves all
// three formats from a comma-joined URL string.
//
// On failure the returned error wraps the underlying nats.go error so
// callers see the original cause when logging with zerolog's Err.
func Connect(cfg *config.Config, logger zerolog.Logger) (*natsgo.Conn, error) {
	urls := strings.Join(cfg.NATSUrls, ",")
	opts := BuildOptions(cfg, logger)

	nc, err := natsgo.Connect(urls, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats connect %q: %w", urls, err)
	}
	return nc, nil
}
