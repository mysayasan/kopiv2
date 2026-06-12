package apis

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
)

const localAuthCookieName = "mymatasan_local_auth"

type localAuthContextKey struct{}

// LocalUserFromContext returns the authenticated local mymatasan user.
func LocalUserFromContext(ctx context.Context) (*services.AuthenticatedUser, bool) {
	user, ok := ctx.Value(localAuthContextKey{}).(*services.AuthenticatedUser)
	return user, ok && user != nil
}

// NewLocalBasicAuth protects standalone mymatasan routes with DB-backed users.
func NewLocalBasicAuth(userService services.ILocalUserService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userService == nil {
				w.Header().Set("WWW-Authenticate", `Basic realm="mymatasan"`)
				http.Error(w, "local auth is not configured", http.StatusUnauthorized)
				return
			}

			gotUser, gotPass, ok := r.BasicAuth()
			if ok {
				user, err := userService.Authenticate(r.Context(), gotUser, gotPass)
				if err == nil {
					setLocalAuthCookie(w, user)
					next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), localAuthContextKey{}, user)))
					return
				}
			}

			if cookie, err := r.Cookie(localAuthCookieName); err == nil {
				username, sessionHash, ok := parseLocalAuthCookie(cookie.Value)
				if ok {
					user, err := userService.AuthenticateSession(r.Context(), username, sessionHash)
					if err != nil {
						w.Header().Set("WWW-Authenticate", `Basic realm="mymatasan"`)
						http.Error(w, "access denied", http.StatusUnauthorized)
						return
					}
					next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), localAuthContextKey{}, user)))
					return
				}
			}

			{
				w.Header().Set("WWW-Authenticate", `Basic realm="mymatasan"`)
				http.Error(w, "access denied", http.StatusUnauthorized)
				return
			}
		})
	}
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
