// Package trustedproxy resolves the real client IP from an incoming
// request given a list of trusted proxy CIDR ranges. It is a
// framework-agnostic pure-Go package: callers hand in the peer IP
// and the raw X-Forwarded-For header string; the package walks the
// chain per RFC 7239 §7.1 (rightmost untrusted) and returns the IP
// that should be used for rate limiting, access logging, and
// auth-adjacent identity decisions.
//
// The package deliberately accepts primitive types only (net.IP,
// string, []*net.IPNet) so reuse across non-HTTP transports is
// trivial. HTTP-specific wiring (Hertz middleware, adapter glue)
// lives in internal/transport/http.
package trustedproxy

import (
	"fmt"
	"net"
	"strings"
)

// MaxHops bounds the number of X-Forwarded-For hops the resolver
// will walk before giving up and falling back to the peer IP.
//
// 32 L7 hops is well above any realistic production topology
// (typical chains are 1-3 hops: client → CDN → ingress → gateway).
// The cap exists as a defence-in-depth measure against XFF
// inflation attacks — an attacker cannot make the resolver walk an
// arbitrarily large chain by stuffing the header with fake entries.
const MaxHops = 32

// privateSentinel is the operator-facing magic string that expands
// to the conventional "private network" CIDR set covering k8s,
// EC2/GCE VPCs, loopback, and IPv6 ULA.
const privateSentinel = "private"

// privateCIDRs is the expansion of the `"private"` sentinel. The
// list intentionally mixes IPv4 and IPv6 because a dual-stack
// deployment may have private peers on either family.
//
// Contents (kept in sync with docs/.env.example):
//   - 10.0.0.0/8      RFC 1918
//   - 172.16.0.0/12   RFC 1918
//   - 192.168.0.0/16  RFC 1918
//   - 100.64.0.0/10   RFC 6598 (carrier-grade NAT)
//   - 127.0.0.0/8     loopback
//   - ::1/128         loopback (IPv6)
//   - fd00::/8        RFC 4193 unique-local
var privateCIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"::1/128",
	"fd00::/8",
}

// ParseCIDRList parses operator-facing configuration input into a
// slice of trusted CIDR networks. Supported forms:
//
//   - ""           → empty slice (trust nothing; always use peer IP).
//   - "private"    → expand to the privateCIDRs list.
//   - "a/b,c/d"    → literal comma-separated CIDR list, whitespace
//                    around entries is tolerated.
//
// A single malformed entry fails the whole parse — callers
// (config.Load()) MUST treat any error as fatal to preserve
// fail-closed startup behaviour.
func ParseCIDRList(input string) ([]*net.IPNet, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, nil
	}

	if trimmed == privateSentinel {
		return parseCIDRs(privateCIDRs)
	}

	parts := strings.Split(trimmed, ",")

	return parseCIDRs(parts)
}

// parseCIDRs converts a slice of textual CIDRs into parsed
// *net.IPNet values. Whitespace around each entry is trimmed. The
// first invalid entry aborts the whole parse with an error naming
// the offending input so operators can locate the typo.
func parseCIDRs(entries []string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(entries))
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("trustedproxy: invalid CIDR %q: %w", entry, err)
		}

		out = append(out, network)
	}

	return out, nil
}
