package apis

import (
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// NewLocalLoginApi registers the PUBLIC sign-in routes: an explicit login endpoint that
// exchanges a username and password for a session cookie, and a logout that clears it.
//
// It must be mounted on the router BEFORE the authenticated subrouter — it is the endpoint
// that authenticates, so it cannot itself sit behind the auth middleware.
//
// WHY A LOGIN ENDPOINT, when NewLocalBasicAuth already accepts an HTTP Basic header:
// because a Basic-only design makes the browser replay the credential on EVERY request,
// which means a full bcrypt verification on every request. mymatasan works that way and it
// cost real throughput — a load test put the ceiling at roughly 300 requests/sec, and the
// fix was a bcrypt result cache, which is a cache of password verifications and exactly as
// uncomfortable as it sounds. Exchanging the credential once for a session cookie pays
// bcrypt once per SIGN-IN instead of once per request, and keeps no verification cache.
// Basic still works for API clients and scripts.
//
// The failed-login lockout is applied here, because this endpoint is now where credential
// guessing actually happens.
func NewLocalLoginApi(router *mux.Router, cfg LocalAuthConfig, userServ services.ILocalUserService, guard *LoginGuard) {
	handler := &localLoginApi{cfg: cfg, userServ: userServ, guard: guard}
	group := router.PathPrefix("/auth").Subrouter()
	group.HandleFunc("/login", handler.login).Methods("POST")
	group.HandleFunc("/logout", handler.logout).Methods("POST")
}

type localLoginApi struct {
	cfg      LocalAuthConfig
	userServ services.ILocalUserService
	guard    *LoginGuard
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// login verifies a credential and issues the session cookie.
//
// A failure is deliberately indistinguishable from a wrong username: the response never
// says which half was wrong, so the endpoint cannot be used to enumerate accounts.
func (a *localLoginApi) login(w http.ResponseWriter, r *http.Request) {
	keys := loginGuardKeys(r)
	if locked, retry := a.guard.Locked(keys...); locked {
		writeLoginLockout(w, a.cfg, retry)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body loginRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	user, err := a.userServ.Authenticate(r.Context(), body.Username, body.Password)
	if err != nil {
		// Slow the attempt down, then count it. A newly tripped lockout is reported once.
		if delay := a.guard.FailedDelay(); delay > 0 {
			time.Sleep(delay)
		}
		if lockedNow, retry := a.guard.RecordFailure(keys...); lockedNow {
			if a.cfg.OnLockout != nil {
				a.cfg.OnLockout(r.Context(), LockoutInfo{
					Username:    body.Username,
					IP:          clientIP(r),
					LockSeconds: int(math.Ceil(retry.Seconds())),
				})
			}
			writeLoginLockout(w, a.cfg, retry)
			return
		}
		controllers.SendError(w, controllers.ErrLimitedAccess, "invalid username or password")
		return
	}

	a.guard.RecordSuccess(keys...)
	setLocalAuthCookie(w, r, a.cfg, user)
	controllers.SendResult(w, user, "succeed")
}

// logout clears the session cookie. It does not require the caller to be authenticated:
// signing out a session that is already gone is a success, not an error.
func (a *localLoginApi) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     a.cfg.cookieName(),
		Value:    "",
		Path:     "/api",
		HttpOnly: true,
		// Must mirror the attributes setLocalAuthCookie used, or the browser treats
		// this as a different cookie and the session cookie survives the logout.
		Secure:   middlewares.IsSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	controllers.SendResult(w, map[string]any{"signedOut": true}, "succeed")
}
