package middlewares

import (
	"net/http"
	"strconv"
	"strings"
)

// SecurityConfig configures the SecurityHeaders middleware. The zero value is
// safe: every nil pointer falls back to a hardened default in
// NewSecurityHeaders, so an app that supplies an empty config still gets
// baseline protection (nosniff, X-Frame-Options, Referrer-Policy, HSTS on TLS,
// and no Server header). ContentSecurityPolicy is the sole opt-in: it is only
// emitted when non-empty, because a wrong CSP silently breaks a SPA, so each
// app enables it only after its own front-end is verified against it.
type SecurityConfig struct {
	// ContentSecurityPolicy, when non-empty, is sent verbatim as the
	// Content-Security-Policy header. Empty => header omitted.
	ContentSecurityPolicy string
	// FrameOptions is the X-Frame-Options value. nil => "SAMEORIGIN"; a pointer
	// to "" omits the header (e.g. when CSP frame-ancestors is authoritative).
	FrameOptions *string
	// ReferrerPolicy is the Referrer-Policy value. nil =>
	// "strict-origin-when-cross-origin"; pointer to "" omits it.
	ReferrerPolicy *string
	// ContentTypeOptions toggles "X-Content-Type-Options: nosniff". nil => on.
	ContentTypeOptions *bool
	// ServerHeader sets the outgoing Server header. nil or "" => the header is
	// stripped, so the server software/version is never advertised.
	ServerHeader *string
	// HSTSEnabled toggles Strict-Transport-Security (only ever sent over TLS).
	// nil => on.
	HSTSEnabled           *bool
	HSTSMaxAgeSeconds     int   // <=0 => 31536000 (one year)
	HSTSIncludeSubDomains *bool // nil => true
	HSTSPreload           bool  // default false
}

// SecurityHeadersMidware sets a fixed set of hardening response headers on every
// request. Values are resolved once in NewSecurityHeaders; the per-request
// handler only copies precomputed strings, so it adds negligible overhead.
type SecurityHeadersMidware struct {
	csp            string // "" => omit
	frameOptions   string // "" => omit
	referrerPolicy string // "" => omit
	nosniff        bool
	serverHeader   string // "" => strip Server
	hsts           string // "" => omit; only applied on TLS connections
}

func strOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// NewSecurityHeaders builds the middleware, filling hardened defaults for any
// unset field.
func NewSecurityHeaders(cfg SecurityConfig) *SecurityHeadersMidware {
	m := &SecurityHeadersMidware{
		csp:            cfg.ContentSecurityPolicy,
		frameOptions:   strOr(cfg.FrameOptions, "SAMEORIGIN"),
		referrerPolicy: strOr(cfg.ReferrerPolicy, "strict-origin-when-cross-origin"),
		nosniff:        boolOr(cfg.ContentTypeOptions, true),
		serverHeader:   strOr(cfg.ServerHeader, ""),
	}

	if boolOr(cfg.HSTSEnabled, true) {
		maxAge := cfg.HSTSMaxAgeSeconds
		if maxAge <= 0 {
			maxAge = 31536000
		}
		parts := []string{"max-age=" + strconv.Itoa(maxAge)}
		if boolOr(cfg.HSTSIncludeSubDomains, true) {
			parts = append(parts, "includeSubDomains")
		}
		if cfg.HSTSPreload {
			parts = append(parts, "preload")
		}
		m.hsts = strings.Join(parts, "; ")
	}

	return m
}

// Middleware applies the configured headers. It runs early in the chain so the
// headers are present on every response, including errors served by inner
// middleware (auth 401s, rate-limit 429s) and static assets.
func (m *SecurityHeadersMidware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Never advertise the server software/version. When a brand string is
		// configured we set it; otherwise we strip any Server header a downstream
		// handler might add.
		if m.serverHeader != "" {
			h.Set("Server", m.serverHeader)
		} else {
			h.Del("Server")
		}

		if m.nosniff {
			h.Set("X-Content-Type-Options", "nosniff")
		}
		if m.frameOptions != "" {
			h.Set("X-Frame-Options", m.frameOptions)
		}
		if m.referrerPolicy != "" {
			h.Set("Referrer-Policy", m.referrerPolicy)
		}
		if m.csp != "" {
			h.Set("Content-Security-Policy", m.csp)
		}
		// HSTS is only meaningful — and only permitted by the spec — over HTTPS.
		// Sending it on plaintext would have the browser ignore it (or worse,
		// cache it against a hostname reachable only via HTTP in dev).
		if m.hsts != "" && r.TLS != nil {
			h.Set("Strict-Transport-Security", m.hsts)
		}

		next.ServeHTTP(w, r)
	})
}
