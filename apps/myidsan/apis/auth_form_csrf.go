package apis

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"html"
	"net/http"
	"strings"

	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// CSRF for the SERVER-RENDERED auth pages.
//
// /api/auth/{login,mfa,forgot,reset} are public routes: they exist precisely for callers
// who have no session yet, so they never pass through the auth middleware and cannot use
// its session-bound double-submit token. Until now they carried no CSRF protection at all,
// which allowed:
//
//   - login CSRF / session fixation — an attacker submits THEIR credentials from a victim's
//     browser, silently signing the victim into the attacker's account, so anything the
//     victim then does (registering a device, saving data) lands in an account the attacker
//     controls;
//   - a forced password-reset submission on behalf of a victim holding a valid reset token.
//
// The mechanism is a double-submit token that needs no session: rendering a form mints a
// random value, sets it in a cookie, and embeds the same value as a hidden field. A
// cross-site page can cause the browser to send the cookie, but same-origin policy stops it
// reading the cookie to forge the matching field.

const authFormCSRFCookie = "kopiv2_auth_csrf"
const authFormCSRFField = "authCsrf"

// issueAuthFormCSRF mints a token, sets it as a cookie on the response, and returns the
// value to embed in the form.
//
// Called on every render rather than reused, so an abandoned form does not leave a
// long-lived token lying around. A form submitted after a re-render still matches, because
// the cookie is replaced at the same time as the field.
func issueAuthFormCSRF(w http.ResponseWriter, r *http.Request) string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		// Without a token the form cannot be protected. Return empty and let
		// validateAuthFormCSRF fail closed rather than render an unprotected form that
		// looks protected.
		return ""
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	secure := middlewares.IsSecureRequest(r)
	name := authFormCSRFCookie
	if secure {
		// __Host- pins the cookie to this exact origin with no Domain attribute, so a
		// sibling subdomain cannot plant one that would satisfy the comparison.
		name = "__Host-" + authFormCSRFCookie
	}
	http.SetCookie(w, &http.Cookie{
		Name:  name,
		Value: token,
		Path:  "/",
		// HttpOnly: nothing needs to read this from script — the matching value is
		// server-rendered into the form.
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		// Session cookie: it only has to outlive the form the user is looking at.
	})
	return token
}

// authFormCSRFInput renders the hidden field. The value is HTML-escaped even though it is
// base64 from our own CSPRNG — escaping at the point of output is the habit that survives
// someone later changing where the value comes from.
func authFormCSRFInput(token string) string {
	return `<input type="hidden" name="` + authFormCSRFField + `" value="` + html.EscapeString(token) + `">`
}

// validateAuthFormCSRF compares the submitted field against the cookie in constant time.
// Fails closed: a missing cookie, a missing field, or an empty token is a rejection.
//
// The caller must have parsed the form already.
func validateAuthFormCSRF(r *http.Request) bool {
	submitted := strings.TrimSpace(r.FormValue(authFormCSRFField))
	if submitted == "" {
		return false
	}
	cookie, err := r.Cookie("__Host-" + authFormCSRFCookie)
	if err != nil {
		cookie, err = r.Cookie(authFormCSRFCookie)
	}
	if err != nil || cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(submitted), []byte(cookie.Value)) == 1
}
