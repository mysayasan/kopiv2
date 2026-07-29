package middlewares

import (
	"log"
	"net"
	"net/http"
	"strings"
)

// Client-address resolution, shared so that everything which records or rate-limits by
// source address agrees on one answer.
//
// The rule that matters: X-Forwarded-For and X-Real-IP are attacker-controlled unless the
// immediate peer is a reverse proxy we put there. A directly-reachable instance that
// trusted those headers would let a caller choose the address recorded against their own
// actions — which quietly destroys the value of an audit trail and lets a per-IP lockout be
// evaded by rotating a header. So headers are honoured ONLY when the peer is in the
// configured trustedProxies list, and the peer address is used otherwise.
//
// This was previously private to the rate limiter while myseliasan's audit log had its own
// copy that trusted the headers unconditionally. One exported implementation avoids a
// security-relevant fork.

// ParseTrustedProxies turns configured IP/CIDR strings into networks. Bare IPs become
// /32 (v4) or /128 (v6). Unparseable entries are skipped with a log line rather than
// failing startup — a typo in one entry should not take the service down, but it must not
// silently widen trust either.
func ParseTrustedProxies(entries []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(entries))
	for _, raw := range entries {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, cidr, err := net.ParseCIDR(raw); err == nil {
			nets = append(nets, cidr)
			continue
		}
		if ip := net.ParseIP(raw); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		log.Printf("trustedProxies: ignoring unparseable entry %q", raw)
	}
	return nets
}

// PeerAddr returns the immediate peer's IP with any port stripped. This is the only
// address that cannot be forged by the caller.
func PeerAddr(r *http.Request) string {
	peer := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(peer); err == nil {
		return host
	}
	return peer
}

// PeerIsTrustedProxy reports whether the request's immediate peer is one of the configured
// reverse proxies. An empty list means nothing is trusted, which is the safe default for a
// directly-exposed instance.
func PeerIsTrustedProxy(r *http.Request, trusted []*net.IPNet) bool {
	if len(trusted) == 0 {
		return false
	}
	ip := net.ParseIP(PeerAddr(r))
	if ip == nil {
		return false
	}
	for _, cidr := range trusted {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP resolves the caller's source address: the left-most X-Forwarded-For entry (the
// original client the trusted proxy saw) or X-Real-IP when the peer is a trusted proxy,
// and the raw peer address otherwise.
func ClientIP(r *http.Request, trusted []*net.IPNet) string {
	peer := PeerAddr(r)
	if !PeerIsTrustedProxy(r, trusted) {
		return peer
	}
	if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); fwd != "" {
		if i := strings.Index(fwd, ","); i >= 0 {
			fwd = strings.TrimSpace(fwd[:i])
		}
		if fwd != "" {
			return fwd
		}
	}
	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
		return real
	}
	return peer
}

// UserAgent returns a length-capped User-Agent for storage. Clients control this string,
// so it is bounded before it reaches a database column or a log line.
func UserAgent(r *http.Request) string {
	ua := strings.TrimSpace(r.UserAgent())
	const max = 512
	if len(ua) > max {
		return ua[:max]
	}
	return ua
}
