package apis

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type auditApi struct {
	audit   services.IAuditService
	session *middlewares.AccessSessionMidware
}

// NewAuditApi mounts the read-only, superadmin-gated audit trail.
//
//	GET /api/audit?limit=&offset=&action=&targetType=&targetId=
//
// The trail is append-only: there is intentionally no create/update/delete route —
// entries are written server-side by the sensitive-action handlers themselves.
func NewAuditApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, audit services.IAuditService) {
	h := &auditApi{audit: audit, session: session}
	g := router.PathPrefix("/audit").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("", h.requireSuper(h.list)).Methods("GET")
}

// requireSuper restricts a handler to a superadmin session (the audit trail can
// expose sensitive operator activity, so only superadmins may read it).
func (a *auditApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.session == nil || !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmin access required")
			return
		}
		next(w, r)
	}
}

func (a *auditApi) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.ParseUint(q.Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(q.Get("offset"), 10, 64)
	rows, total, err := a.audit.List(r.Context(), limit, offset, q.Get("action"), q.Get("targetType"), q.Get("targetId"))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, limit, offset, total, "succeed")
}

// --- shared actor/context helpers used by the sensitive-action handlers ---------

// auditActor pulls the requesting operator's identity from the JWT claims for audit
// attribution: user id, a human-readable label (email, else display name), and the
// role id baked in the token.
func auditActor(r *http.Request) (id int64, label string, role int64) {
	claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	if !ok || claims == nil {
		return 0, "", 0
	}
	label = strings.TrimSpace(claims.Email)
	if label == "" {
		label = strings.TrimSpace(claims.Name)
	}
	if label == "" {
		label = strconv.FormatInt(claims.Id, 10)
	}
	return claims.Id, label, claims.RoleId
}

// clientIP returns the best-effort source address of the request (X-Forwarded-For
// first hop when present, else RemoteAddr host).
func clientIP(r *http.Request) string {
	if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); fwd != "" {
		if i := strings.IndexByte(fwd, ','); i >= 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return fwd
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
