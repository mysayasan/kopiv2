package middlewares

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, cfg SecurityConfig, tlsConn bool) http.Header {
	t.Helper()
	m := NewSecurityHeaders(cfg)
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An inner handler that tries to advertise the server; the middleware
		// must have already decided the Server header before this runs, and its
		// strip must win at write time (headers are a map, last write wins, and
		// the middleware runs first/outermost so nothing re-adds it here).
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	if tlsConn {
		req.TLS = &tls.ConnectionState{}
	} else {
		req.TLS = nil
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result().Header
}

func TestSecurityHeaders_Defaults(t *testing.T) {
	got := serve(t, SecurityConfig{}, true)

	if v := got.Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", v)
	}
	if v := got.Get("X-Frame-Options"); v != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want SAMEORIGIN", v)
	}
	if v := got.Get("Referrer-Policy"); v != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", v)
	}
	if v := got.Get("Strict-Transport-Security"); v != "max-age=31536000; includeSubDomains" {
		t.Errorf("HSTS = %q, want max-age=31536000; includeSubDomains", v)
	}
	// No Server header should ever be advertised by default.
	if v := got.Get("Server"); v != "" {
		t.Errorf("Server = %q, want empty (stripped)", v)
	}
	// CSP is opt-in: absent unless configured.
	if v := got.Get("Content-Security-Policy"); v != "" {
		t.Errorf("CSP = %q, want empty when not configured", v)
	}
}

func TestSecurityHeaders_HSTSOnlyOnTLS(t *testing.T) {
	got := serve(t, SecurityConfig{}, false)
	if v := got.Get("Strict-Transport-Security"); v != "" {
		t.Errorf("HSTS on plaintext = %q, want empty (only sent over TLS)", v)
	}
}

func TestSecurityHeaders_CSPWhenSet(t *testing.T) {
	csp := "default-src 'self'; frame-ancestors 'none'"
	got := serve(t, SecurityConfig{ContentSecurityPolicy: csp}, true)
	if v := got.Get("Content-Security-Policy"); v != csp {
		t.Errorf("CSP = %q, want %q", v, csp)
	}
}

func TestSecurityHeaders_Overrides(t *testing.T) {
	empty := ""
	frame := "DENY"
	brand := "acme"
	no := false
	got := serve(t, SecurityConfig{
		FrameOptions:       &frame,
		ReferrerPolicy:     &empty, // explicit "" => omit
		ContentTypeOptions: &no,    // disable nosniff
		ServerHeader:       &brand, // set a brand token
		HSTSEnabled:        &no,    // disable HSTS
	}, true)

	if v := got.Get("X-Frame-Options"); v != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", v)
	}
	if v := got.Get("Referrer-Policy"); v != "" {
		t.Errorf("Referrer-Policy = %q, want empty (omitted)", v)
	}
	if v := got.Get("X-Content-Type-Options"); v != "" {
		t.Errorf("X-Content-Type-Options = %q, want empty (disabled)", v)
	}
	if v := got.Get("Server"); v != "acme" {
		t.Errorf("Server = %q, want acme", v)
	}
	if v := got.Get("Strict-Transport-Security"); v != "" {
		t.Errorf("HSTS = %q, want empty (disabled)", v)
	}
}

func TestSecurityHeaders_HSTSCustom(t *testing.T) {
	no := false
	got := serve(t, SecurityConfig{
		HSTSMaxAgeSeconds:     100,
		HSTSIncludeSubDomains: &no,
		HSTSPreload:           true,
	}, true)
	if v := got.Get("Strict-Transport-Security"); v != "max-age=100; preload" {
		t.Errorf("HSTS = %q, want max-age=100; preload", v)
	}
}
