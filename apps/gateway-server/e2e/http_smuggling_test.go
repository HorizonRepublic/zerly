//go:build e2e

// Package e2e — HTTP request-smuggling rejection pinned end-to-end
// against Hertz. Sibling of e2e_test.go / contract_test.go; reuses
// their `gatewayURL` constant and `waitForGateway` helper. Spin the
// stack up per the README protocol in this directory before running.
//
// These tests issue raw HTTP/1.1 requests over a TCP socket so the
// suite can craft byte sequences a high-level http.Client refuses to
// send. They pin the contract that the underlying Hertz HTTP/1.1
// parser rejects RFC-7230-§3.3.3-violating frames — a request
// carrying both `Transfer-Encoding: chunked` AND `Content-Length`
// is the canonical smuggling primitive that lets an attacker
// desynchronise the gateway's view of message boundaries from any
// upstream proxy that disagrees on which length to honour.
//
// The test passes if Hertz either:
//  1. Returns a 400 Bad Request on the malformed frame, or
//  2. Closes the connection without responding.
//
// Both outcomes are RFC-compliant. Pinning the union of the two
// guards against a future Hertz upgrade that silently relaxes the
// rejection — without this test, a regression to "accept
// TE+CL with TE winning" would land unobserved and reintroduce the
// smuggling vector.
package e2e

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// gatewayHostPort extracts host:port from gatewayURL so the raw TCP
// dialer can target the listener without depending on the URL parser
// at every callsite.
func gatewayHostPort(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(gatewayURL)
	require.NoError(t, err)
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}

	return host
}

// TestE2E_HTTPSmuggling_TransferEncodingPlusContentLengthRejected
// pins the canonical request-smuggling primitive: an HTTP/1.1
// request carrying BOTH `Transfer-Encoding: chunked` and
// `Content-Length: N`. RFC 7230 §3.3.3 rule 3 requires the receiver
// to reject the message or remove `Content-Length` — Hertz rejects.
//
// The crafted frame is what an attacker would use against a frontend
// that honours TE while a downstream honours CL (or vice versa) to
// inject a second request hidden inside the body of the first. The
// rejection at the gateway boundary terminates the attack before
// either side reads inconsistent message boundaries.
func TestE2E_HTTPSmuggling_TransferEncodingPlusContentLengthRejected(t *testing.T) {
	waitForGateway(t)

	conn, err := net.DialTimeout("tcp", gatewayHostPort(t), 5*time.Second)
	require.NoError(t, err, "raw TCP dial to gateway must succeed")
	defer func() { _ = conn.Close() }()

	// The smuggled frame: TE: chunked declares a chunked body that
	// terminates immediately ("0\r\n\r\n"), while CL: 5 declares a
	// 5-byte body. The "SMUGG" trailer is what a TE-honouring parser
	// would treat as the start of a second request, while a
	// CL-honouring parser would treat as the body of the first. Hertz
	// (TE-priority per RFC) MUST reject before any of this reaches
	// the proxy layer.
	req := "POST /users HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Content-Type: application/json\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"Content-Length: 5\r\n" +
		"\r\n" +
		"0\r\n" +
		"\r\n" +
		"SMUGG"

	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = conn.Write([]byte(req))
	require.NoError(t, err, "writing the smuggling request must not fail")

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')

	// Two RFC-compliant outcomes are accepted:
	//   1. Hertz returns 400 Bad Request (or another 4xx) on the
	//      smuggling frame.
	//   2. Hertz drops the connection without responding (EOF or
	//      a TCP-level read error before any status line arrives).
	// Either outcome means the smuggled trailer never reached the
	// proxy layer, which is the security contract we are pinning.
	if err != nil {
		require.True(t,
			errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || isNetReadError(err),
			"connection-drop is RFC-compliant, but unexpected error: %v", err)

		return
	}

	statusLine = strings.TrimRight(statusLine, "\r\n")
	require.True(t,
		strings.HasPrefix(statusLine, "HTTP/1.1 4") || strings.HasPrefix(statusLine, "HTTP/1.0 4"),
		"smuggling frame must be rejected with a 4xx status line; got %q", statusLine)
}

// TestE2E_HTTPSmuggling_DuplicateContentLengthRejected pins the
// duplicate-Content-Length rejection. RFC 7230 §3.3.3 rule 4 allows
// two equal CL headers to be folded but two distinct values MUST be
// rejected. Hertz follows the strict reading: any duplicate CL
// fails the parse.
//
// The attack vector is the same as TE+CL: get one parser to honour
// the first value and another to honour the second so the frame
// boundaries desynchronise. Pinning rejection here guards against a
// regression to "merge duplicate CLs" that a permissive parser
// upgrade might silently introduce.
func TestE2E_HTTPSmuggling_DuplicateContentLengthRejected(t *testing.T) {
	waitForGateway(t)

	conn, err := net.DialTimeout("tcp", gatewayHostPort(t), 5*time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	req := "POST /users HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: 5\r\n" +
		"Content-Length: 6\r\n" +
		"\r\n" +
		"hello"

	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = conn.Write([]byte(req))
	require.NoError(t, err)

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')

	if err != nil {
		require.True(t,
			errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || isNetReadError(err),
			"connection-drop is RFC-compliant, but unexpected error: %v", err)

		return
	}

	statusLine = strings.TrimRight(statusLine, "\r\n")
	require.True(t,
		strings.HasPrefix(statusLine, "HTTP/1.1 4") || strings.HasPrefix(statusLine, "HTTP/1.0 4"),
		"duplicate Content-Length frame must be rejected with a 4xx status line; got %q", statusLine)
}

// isNetReadError reports whether err is a network-level read failure
// (e.g. connection reset by peer, EOF, use-of-closed-network-conn)
// consistent with the gateway dropping the conn rather than emitting
// a status line. The test treats those as equivalent to a clean EOF
// for the purpose of the rejection contract — what we are pinning is
// "smuggled bytes did not reach the proxy layer", not the specific
// RFC-permitted termination shape.
//
// We deliberately classify only network-level failures here; an
// arbitrary parser-side error (malformed HTTP, decode failure on a
// status line that DID arrive) would indicate the gateway responded
// and so MUST NOT be mistaken for "conn dropped". The previous
// "treat any non-nil err as drop" shape would mask such regressions.
func isNetReadError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// Some platforms surface read-after-close via a string match only;
	// the canonical Go message is "use of closed network connection".
	if strings.Contains(err.Error(), "use of closed network connection") {
		return true
	}

	return false
}
