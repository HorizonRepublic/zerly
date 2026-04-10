package nats

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

// Requester implements proxy.NatsRequester by round-robin-ing requests
// across a pool of nats.Conn instances.
//
// A single nats.Conn is goroutine-safe but funnels all sends through
// one socket, which becomes a contention point at very high RPS.
// Holding N parallel connections and distributing requests across them
// scales linearly up to NIC saturation. The pool size is configured
// via NATS_CONNECTION_POOL (default 1); only increase it when
// profiling shows socket-level contention, never as a speculative
// optimisation.
//
// The Requester is safe for concurrent use from any number of
// goroutines — the only shared state is the atomic round-robin
// counter, and each underlying nats.Conn is independently
// goroutine-safe.
type Requester struct {
	conns   []*natsgo.Conn
	counter atomic.Uint64
}

// errNoConns is returned by NewRequester when the caller supplied an
// empty connection slice. Construction-time validation avoids an
// unreachable divide-by-zero in the request path.
var errNoConns = errors.New("nats requester: at least one connection required")

// NewRequester constructs a Requester wrapping the supplied connections.
// At least one connection is required; an empty slice returns
// errNoConns so the caller can fail startup loudly.
func NewRequester(conns []*natsgo.Conn) (*Requester, error) {
	if len(conns) == 0 {
		return nil, errNoConns
	}
	return &Requester{conns: conns}, nil
}

// Request sends an RPC request to subject and waits for a reply.
//
// Errors are wrapped with the subject name and propagated verbatim
// from nats.go so callers can use errors.Is against nats.ErrTimeout
// to discriminate timeouts from connection failures upstream.
func (r *Requester) Request(subject string, payload []byte, timeout time.Duration) ([]byte, error) {
	idx := r.counter.Add(1) % uint64(len(r.conns))
	msg, err := r.conns[idx].Request(subject, payload, timeout)
	if err != nil {
		return nil, fmt.Errorf("nats request %q: %w", subject, err)
	}
	return msg.Data, nil
}

// Close drains every underlying connection. Drain waits for in-flight
// subscriptions to finish before tearing down the socket, giving
// handlers a chance to complete cleanly on shutdown.
func (r *Requester) Close() {
	for _, c := range r.conns {
		_ = c.Drain()
	}
}
