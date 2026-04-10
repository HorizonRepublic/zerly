package proxy

import "time"

// NatsRequester is the narrow contract the Handler needs from the
// NATS transport layer. Defining it here (in the proxy package) means
// the Handler depends on the abstraction, not a concrete nats.Conn —
// classic Go dependency inversion, and the reason the Handler can be
// unit-tested without any NATS server running.
//
// Implementations MUST be safe for concurrent use; Handler.Handle may
// be called from many goroutines in parallel.
type NatsRequester interface {
	Request(subject string, payload []byte, timeout time.Duration) ([]byte, error)
}
