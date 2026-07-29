package apis

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type sessionApi struct {
	sessions services.ISessionService
	audit    services.IAuditService
	auth     middlewares.AuthMidware
	access   *middlewares.AccessSessionMidware
	// trustedProxies must match what the login API uses, or the same operator would be
	// recorded under two different addresses depending on which action they took.
	trustedProxies []*net.IPNet
}

// NewSessionApi mounts session listing and revocation.
//
// Self-service (authenticated, own account only):
//
//	GET    /api/session              your own sessions
//	DELETE /api/session/{sessionId}  end one of your own
//	POST   /api/session/revoke-all   end all of yours except the current one
//
// Administration (superadmin):
//
//	GET    /api/session-admin/user/{userId}         someone else's sessions
//	POST   /api/session-admin/user/{userId}/revoke  end all of theirs
//
// The split matters: the self-service routes are auth-only because reviewing and ending
// YOUR OWN sessions is not a privileged action — making it superadmin-gated would mean
// nobody but an administrator could respond to their own laptop being stolen. Acting on
// another account is superadmin-only, and every route enforces ownership server-side
// rather than trusting a user id from the client.
func NewSessionApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	access *middlewares.AccessSessionMidware,
	sessions services.ISessionService,
	audit services.IAuditService,
	trustedProxies []string,
) {
	h := &sessionApi{
		sessions:       sessions,
		audit:          audit,
		auth:           auth,
		access:         access,
		trustedProxies: middlewares.ParseTrustedProxies(trustedProxies),
	}

	// Self-service: any authenticated user, acting on their own account. Deliberately NO
	// access.Middleware — that enforces the RBAC permission matrix, and gating this behind
	// a grant would mean an ordinary user could not see or end their own sessions, which
	// is precisely the person who needs to when a laptop goes missing. Mirrors how
	// /api/mfa and /api/login/default/change-password are mounted.
	self := router.PathPrefix("/session").Subrouter()
	self.Use(auth.Middleware)
	self.HandleFunc("", h.listOwn).Methods("GET")
	self.HandleFunc("/revoke-all", h.revokeAllOwn).Methods("POST")
	// Registered last: the fixed paths above must match first.
	self.HandleFunc("/{sessionId}", h.revokeOwn).Methods("DELETE")

	// Acting on ANOTHER account is privilege-affecting, so it lives on its own prefix
	// behind the superadmin gate — the same split /api/mfa vs /api/mfa-admin uses.
	admin := router.PathPrefix("/session-admin").Subrouter()
	admin.Use(auth.Middleware)
	admin.Use(access.Middleware)
	admin.Use(access.RequireSuperadmin)
	admin.HandleFunc("/user/{userId:[0-9]+}", h.listForUser).Methods("GET")
	admin.HandleFunc("/user/{userId:[0-9]+}/revoke", h.revokeAllForUser).Methods("POST")
}

// callerClaims returns the requesting session's claims, which carry both the user id and
// the session id — the latter is how "this is the session you are using right now" is
// determined without trusting anything the client sent.
func callerClaims(r *http.Request) *models.JwtCustomClaims {
	claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	if !ok {
		return nil
	}
	return claims
}

func (a *sessionApi) listOwn(w http.ResponseWriter, r *http.Request) {
	claims := callerClaims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "not signed in")
		return
	}
	views, err := a.sessions.ListForUser(r.Context(), claims.Id, claims.SessionId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, views)
}

func (a *sessionApi) revokeOwn(w http.ResponseWriter, r *http.Request) {
	claims := callerClaims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "not signed in")
		return
	}
	sessionId := strings.TrimSpace(mux.Vars(r)["sessionId"])

	// Ownership is checked against the caller's own session list rather than by taking
	// the id on trust: without this, any signed-in user could end any session on the
	// server by guessing or replaying an id.
	views, err := a.sessions.ListForUser(r.Context(), claims.Id, claims.SessionId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	owned := false
	for _, v := range views {
		if v.SessionId == sessionId {
			owned = true
			break
		}
	}
	if !owned {
		// Deliberately the same answer as a session that does not exist, so this is not
		// a probe for which session ids are real.
		controllers.SendError(w, controllers.ErrNotFound, "session not found")
		return
	}

	if _, err := a.sessions.Revoke(r.Context(), sessionId); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.record(r, services.AuditEntry{
		Action:     services.ActionSessionRevoke,
		ActorId:    claims.Id,
		ActorEmail: claims.Email,
		ActorRole:  claims.RoleId,
		TargetType: "session",
		TargetId:   sessionId,
		Detail:     "ended own session",
		Metadata:   map[string]any{"self": true, "current": sessionId == claims.SessionId},
	})
	controllers.SendResult(w, map[string]bool{"ok": true})
}

func (a *sessionApi) revokeAllOwn(w http.ResponseWriter, r *http.Request) {
	claims := callerClaims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "not signed in")
		return
	}
	// The current session is spared so "sign out everywhere else" leaves the person who
	// asked for it still signed in — the point is to evict the other devices.
	count, err := a.sessions.RevokeAllForUser(r.Context(), claims.Id, claims.SessionId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.record(r, services.AuditEntry{
		Action:     services.ActionSessionRevokeAll,
		ActorId:    claims.Id,
		ActorEmail: claims.Email,
		ActorRole:  claims.RoleId,
		TargetType: "self",
		TargetId:   strconv.FormatInt(claims.Id, 10),
		Detail:     "signed out of other devices",
		Metadata:   map[string]any{"revoked": count, "self": true},
	})
	controllers.SendResult(w, map[string]any{"ok": true, "revoked": count})
}

func (a *sessionApi) listForUser(w http.ResponseWriter, r *http.Request) {
	userId, ok := a.pathUserId(w, r)
	if !ok {
		return
	}
	// currentSessionId is empty: none of the target's sessions is the caller's, so
	// nothing should be labelled "current" on someone else's list.
	views, err := a.sessions.ListForUser(r.Context(), userId, "")
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, views)
}

func (a *sessionApi) revokeAllForUser(w http.ResponseWriter, r *http.Request) {
	userId, ok := a.pathUserId(w, r)
	if !ok {
		return
	}
	claims := callerClaims(r)
	count, err := a.sessions.RevokeAllForUser(r.Context(), userId, "")
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	entry := services.AuditEntry{
		Action:     services.ActionSessionRevokeAll,
		TargetType: "user",
		TargetId:   strconv.FormatInt(userId, 10),
		Detail:     "administrator ended all sessions for this account",
		Metadata:   map[string]any{"revoked": count},
	}
	if claims != nil {
		entry.ActorId, entry.ActorEmail, entry.ActorRole = claims.Id, claims.Email, claims.RoleId
	}
	a.record(r, entry)
	controllers.SendResult(w, map[string]any{"ok": true, "revoked": count})
}

func (a *sessionApi) pathUserId(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := strings.TrimSpace(mux.Vars(r)["userId"])
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid user id")
		return 0, false
	}
	return id, true
}

func (a *sessionApi) record(r *http.Request, e services.AuditEntry) {
	if a.audit == nil {
		return
	}
	e.ClientIp, e.UserAgent = auditContext(r, a.trustedProxies)
	a.audit.Record(r.Context(), e)
}
