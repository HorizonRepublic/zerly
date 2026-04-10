package proxy

import (
	"errors"

	natsgo "github.com/nats-io/nats.go"
)

// isTimeoutErr reports whether err represents a NATS request timeout.
//
// The check uses errors.Is against nats.ErrTimeout so wrapped errors
// (e.g., `fmt.Errorf("nats request %q: %w", subject, err)` produced
// by the transport Requester) still match. Handler translates a
// positive result into a 504 Gateway Timeout response; anything else
// is treated as an upstream failure and returned as 503 Service
// Unavailable.
func isTimeoutErr(err error) bool {
	return errors.Is(err, natsgo.ErrTimeout)
}
