package apis

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
)

const localAuthCookieName = "mymatasan_local_auth"

// passwordChangeRequiredCode is the error code returned (and read by the SPA)
// when a must-change user touches any route other than the change endpoint.
const passwordChangeRequiredCode = "password_change_required"

type localAuthContextKey struct{}

// LocalUserFromContext returns the authenticated local mymatasan user.
func LocalUserFromContext(ctx context.Context) (*services.AuthenticatedUser, bool) {
	user, ok := ctx.Value(localAuthContextKey{}).(*services.AuthenticatedUser)
	return user, ok && user != nil
}

// NewLocalBasicAuth protects standalone mymatasan routes with DB-backed users.
// guard (optional) throttles failed sign-ins to blunt brute-force attacks; when
// it trips a lockout and notifier is set, a security event is published.
func NewLocalBasicAuth(userService services.ILocalUserService, guard *LoginGuard, notifier services.INotificationPublisher) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// deny rejects with 401 but deliberately WITHOUT a WWW-Authenticate: Basic
			// header. This is an SPA that owns its own login UI; sending that header makes
			// the browser pop its native Basic-auth dialog whenever any request hits 401 —
			// especially <img>/<video> media tiles that auth via cookie — e.g. after a
			// session expires or mid factory-reset when the database is dropped. The SPA
			// handles 401 itself, so the native prompt is never wanted.
			deny := func(msg string) { http.Error(w, msg, http.StatusUnauthorized) }

			// A control-channel command arrives pre-authenticated: the dispatcher
			// (apis.NewControlDispatcher) injects the principal in-process after the
			// fleet-mTLS parent connection is verified. Honor it and skip credential
			// and lockout checks — the principal can only be set in-process, never by a
			// network client. The admin-for-writes gate downstream still applies.
			if _, ok := LocalUserFromContext(r.Context()); ok {
				next.ServeHTTP(w, r)
				return
			}

			if userService == nil {
				deny("local auth is not configured")
				return
			}

			gotUser, gotPass, ok := r.BasicAuth()
			keys := loginGuardKeys(r)

			// The failed-login lockout applies ONLY to the interactive login probe
			// (GET /auth/session), never to the rest of the protected surface. The
			// SPA replays its stored Basic credential on every request — background
			// polls, the notification SSE reconnect, media tiles, page-load data
			// fetches — so counting a wrong credential on any of those lets a single
			// page load from a client whose credential just went stale (e.g. after a
			// reinstall/factory-reset reseeds the admin password, or a password
			// change in another tab) fire a burst of parallel requests that drains
			// the whole attempt budget and self-locks a legitimate user before they
			// ever type anything. Volumetric abuse of other endpoints is bounded
			// separately by the global rate limiter; credential guessing happens at
			// the login probe, which is where the lockout belongs.
			loginAttempt := isLoginAttemptPath(r.URL.Path)

			// Locked-out keys are rejected before any bcrypt work so the lockout
			// is both enforced and cheap to serve under a flood.
			if loginAttempt {
				if locked, retry := guard.Locked(keys...); locked {
					writeLoginLockout(w, retry)
					return
				}
			}

			// serve grants access, but first enforces the forced-password-change
			// gate so a flagged user can reach nothing but the change/session
			// endpoints until they pick a new password.
			serve := func(user *services.AuthenticatedUser) {
				if user.MustChangePassword && !isPasswordChangeAllowedPath(r.URL.Path) {
					writePasswordChangeRequired(w)
					return
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), localAuthContextKey{}, user)))
			}

			// Explicit Basic credentials are authoritative: if present, they decide
			// the request. We never fall back to the session cookie on a wrong Basic
			// credential — otherwise a stale cookie would silently authenticate a bad
			// login, letting the SPA in as the old user while every request re-fails
			// the wrong credential and piles up lockout failures.
			if ok {
				user, err := userService.Authenticate(r.Context(), gotUser, gotPass)
				if err == nil {
					if loginAttempt {
						guard.RecordSuccess(keys...)
					}
					setLocalAuthCookie(w, user)
					serve(user)
					return
				}
				// Wrong credentials on a non-login request (a background poll, SSE
				// reconnect, or media tile whose stored credential went stale): deny
				// without touching the lockout so it can't be drained by page churn.
				if !loginAttempt {
					deny("access denied")
					return
				}
				// A real login attempt with wrong credentials: slow it, then count
				// it. A newly tripped lockout is reported (once) and surfaced as an
				// event.
				if delay := guard.FailedDelay(); delay > 0 {
					time.Sleep(delay)
				}
				if lockedNow, retry := guard.RecordFailure(keys...); lockedNow {
					if notifier != nil {
						services.NotifyAuthLockout(r.Context(), notifier, services.AuthLockoutInfo{
							Username:    gotUser,
							IP:          clientIP(r),
							LockSeconds: int(math.Ceil(retry.Seconds())),
						})
					}
					writeLoginLockout(w, retry)
					return
				}
				deny("access denied")
				return
			}

			// No Basic header (e.g. <img>/<video> media requests that cannot send
			// one) — fall back to the session cookie.
			if cookie, err := r.Cookie(localAuthCookieName); err == nil {
				username, sessionHash, ok := parseLocalAuthCookie(cookie.Value)
				if ok {
					user, err := userService.AuthenticateSession(r.Context(), username, sessionHash)
					if err != nil {
						deny("access denied")
						return
					}
					serve(user)
					return
				}
			}

			deny("access denied")
		})
	}
}

// loginGuardKeys derives the lockout keys for a request. Keying is per source IP
// only: it throttles a host hammering credentials without letting an attacker
// lock a real user out of their account by spamming that username (account-axis
// lockout DoS).
func loginGuardKeys(r *http.Request) []string {
	return []string{"ip:" + clientIP(r)}
}

// isLoginAttemptPath reports whether a request is the interactive login probe —
// the only surface the failed-login lockout counts and enforces against. The SPA
// verifies a credential by calling GET /auth/session (apis.NewLocalAuthApi), so a
// wrong password there is a deliberate sign-in attempt; a wrong credential on any
// other protected route is stale-client churn, not credential guessing.
func isLoginAttemptPath(path string) bool {
	return strings.HasSuffix(path, "/auth/session")
}

// isPasswordChangeAllowedPath reports whether a path stays reachable while a user
// is flagged must-change: only the self-service change endpoint and the session
// probe the frontend reads to discover the flag.
func isPasswordChangeAllowedPath(path string) bool {
	return strings.HasSuffix(path, "/auth/change-password") || strings.HasSuffix(path, "/auth/session")
}

// writePasswordChangeRequired rejects a request with a machine-readable code the
// SPA keys on to render the forced password-change screen.
func writePasswordChangeRequired(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "failed",
		"message": "password change required",
		"code":    passwordChangeRequiredCode,
	})
}

// clientIP returns the connecting peer's IP. It intentionally uses RemoteAddr
// rather than X-Forwarded-For so a client cannot spoof a header to dodge its own
// lockout; deployments behind a trusted proxy should terminate at this process.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

// writeLoginLockout sends a 429 with the remaining wait both as a Retry-After
// header and in the JSON body (retryAfterSeconds), so the SPA can drive a
// countdown without depending on cross-origin header exposure.
func writeLoginLockout(w http.ResponseWriter, retry time.Duration) {
	secs := int(math.Ceil(retry.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("WWW-Authenticate", `Basic realm="mymatasan"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":            "failed",
		"message":           "Too many failed login attempts.",
		"code":              "login_locked",
		"retryAfterSeconds": secs,
	})
}

func setLocalAuthCookie(w http.ResponseWriter, user *services.AuthenticatedUser) {
	http.SetCookie(w, &http.Cookie{
		Name:     localAuthCookieName,
		Value:    localAuthCookieValue(user),
		Path:     "/api",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(12 * time.Hour),
		MaxAge:   int((12 * time.Hour).Seconds()),
	})
}

func localAuthCookieValue(user *services.AuthenticatedUser) string {
	if user == nil {
		return ""
	}
	return user.Username + ":" + user.SessionHash
}

func parseLocalAuthCookie(value string) (string, string, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
