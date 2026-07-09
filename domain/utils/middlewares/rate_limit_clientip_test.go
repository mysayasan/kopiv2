package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestReq(remoteAddr, xff, xreal string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://example.com/api/home/latest", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	if xreal != "" {
		r.Header.Set("X-Real-IP", xreal)
	}
	return r
}

// TestClientIPIgnoresSpoofedForwardHeadersWhenUntrusted is the security case:
// with no trusted proxies configured (the default), a directly-connected client
// cannot change its rate-limit bucket by sending X-Forwarded-For / X-Real-IP —
// the resolver uses the real peer address, so header rotation can't mint fresh
// buckets to bypass the limiter or the login lockout.
func TestClientIPIgnoresSpoofedForwardHeadersWhenUntrusted(t *testing.T) {
	mid := &RateLimitMidware{trustedProxies: parseTrustedProxies(nil)}

	got := mid.clientIP(newTestReq("203.0.113.7:5555", "1.2.3.4", "5.6.7.8"))
	if got != "203.0.113.7" {
		t.Fatalf("untrusted peer: clientIP = %q, want the real peer 203.0.113.7 (headers must be ignored)", got)
	}

	// A different spoofed header from the same peer must resolve to the same bucket.
	again := mid.clientIP(newTestReq("203.0.113.7:6666", "9.9.9.9", ""))
	if again != "203.0.113.7" {
		t.Fatalf("untrusted peer with different header: clientIP = %q, want 203.0.113.7", again)
	}
}

// TestClientIPHonorsForwardHeaderFromTrustedProxy: behind a configured proxy,
// the real client (left-most XFF entry) becomes the bucket key so clients get
// per-client limits instead of sharing the proxy's single address.
func TestClientIPHonorsForwardHeaderFromTrustedProxy(t *testing.T) {
	mid := &RateLimitMidware{trustedProxies: parseTrustedProxies([]string{"10.0.0.0/8"})}

	got := mid.clientIP(newTestReq("10.0.0.5:5555", "198.51.100.23, 10.0.0.5", ""))
	if got != "198.51.100.23" {
		t.Fatalf("trusted proxy XFF: clientIP = %q, want original client 198.51.100.23", got)
	}

	// X-Real-IP is used when XFF is absent.
	real := mid.clientIP(newTestReq("10.0.0.5:5555", "", "198.51.100.99"))
	if real != "198.51.100.99" {
		t.Fatalf("trusted proxy X-Real-IP: clientIP = %q, want 198.51.100.99", real)
	}
}

// TestClientIPTrustedProxyWithoutHeaderFallsBackToPeer: a trusted proxy that
// sends no forwarding header still resolves to the proxy's own address (rather
// than empty), and a bare-IP trusted entry matches as a /32.
func TestClientIPTrustedProxyWithoutHeaderFallsBackToPeer(t *testing.T) {
	mid := &RateLimitMidware{trustedProxies: parseTrustedProxies([]string{"10.0.0.5"})}

	got := mid.clientIP(newTestReq("10.0.0.5:5555", "", ""))
	if got != "10.0.0.5" {
		t.Fatalf("trusted proxy no header: clientIP = %q, want 10.0.0.5", got)
	}
}
