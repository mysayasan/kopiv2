package apis

import (
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	inputdtos "github.com/mysayasan/kopiv2/apps/myidsan/dtos/input"
	outputdtos "github.com/mysayasan/kopiv2/apps/myidsan/dtos/output"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// UserLoginApi struct
type userLoginApi struct {
	auth     middlewares.AuthMidware
	serv     services.IUserLoginDtoService[outputdtos.UserLoginDto]
	sessions services.ISessionService
	audit    services.IAuditService
	trusted  []*net.IPNet
}

// endSessionsFor terminates every live session belonging to a user. Called when an account
// is disabled or deleted.
//
// Without this, disabling an account did not sign anybody out: the auth middleware
// validates the cached session entry, which carries no account-status flag, so an already
// signed-in user kept working until their session expired — up to 72 hours after an
// administrator believed they had cut off access. RBAC-gated routes did start refusing
// them (the role resolver reports the account disabled), but auth-only routes such as
// /api/profile/* and /api/mfa stayed reachable.
func (m *userLoginApi) endSessionsFor(r *http.Request, userId int64, reason string) {
	if m.sessions == nil || userId <= 0 {
		return
	}
	count, err := m.sessions.RevokeAllForUser(r.Context(), userId, "")
	if err != nil {
		log.Printf("failed to end sessions for user %d after %s: %v", userId, reason, err)
		return
	}
	if count == 0 || m.audit == nil {
		return
	}
	entry := services.AuditEntry{
		Action:     services.ActionSessionRevokeAll,
		TargetType: "user",
		TargetId:   strconv.FormatInt(userId, 10),
		Detail:     "sessions ended because the account was " + reason,
		Metadata:   map[string]any{"revoked": count, "reason": reason},
	}
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		entry.ActorId, entry.ActorEmail, entry.ActorRole = claims.Id, claims.Email, claims.RoleId
	}
	entry.ClientIp, entry.UserAgent = auditContext(r, m.trusted)
	m.audit.Record(r.Context(), entry)
}

// Create UserLoginApi
//
// User-account management (listing, role assignment, enable/disable, deletion) is
// SUPERADMIN-ONLY, mirroring myseliasan's /api/rbac surface. Role assignment is a
// privilege-escalation vector, so it must never be governed by the generic permission
// matrix alone (a non-superadmin role with a broad GET/PUT grant could otherwise change
// any account's role — including its own — to superadmin).
func NewUserLoginApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	access *middlewares.AccessSessionMidware,
	serv services.IUserLoginDtoService[outputdtos.UserLoginDto],
	sessions services.ISessionService,
	audit services.IAuditService,
	trustedProxies []string) {
	handler := &userLoginApi{
		auth:     auth,
		serv:     serv,
		sessions: sessions,
		audit:    audit,
		trusted:  middlewares.ParseTrustedProxies(trustedProxies),
	}

	// Create api sub-router — the whole user-account surface is superadmin-only.
	group := router.PathPrefix("/user-credential").Subrouter()
	group.Use(auth.Middleware)
	group.Use(access.Middleware)
	group.Use(access.RequireSuperadmin)

	// Group Handlers
	group.HandleFunc("", handler.get).Methods("GET")
	group.HandleFunc("/email", handler.getByEmail).Methods("GET")
	group.HandleFunc("", handler.post).Methods("POST")
	group.HandleFunc("", handler.put).Methods("PUT")
	group.HandleFunc("/{id}", handler.delete).Methods("DELETE")
}

// post creates an account with a role assigned up front — the admin-provisioning
// path (setup wizard "create your own superadmin", Users page). Distinct from
// self-registration (/api/login/default/register), which always lands pending.
func (m *userLoginApi) post(w http.ResponseWriter, r *http.Request) {
	body, err := sharedapis.DecodeRequestDto[inputdtos.UserLoginDto, entities.UserLogin](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if strings.TrimSpace(body.Email) == "" || strings.TrimSpace(body.Userpwd) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "email and password are required")
		return
	}
	body.Id = 0
	body.IsActive = true
	if body.CreatedAt == 0 {
		body.CreatedAt = time.Now().Unix()
	}
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		body.CreatedBy = claims.Id
	}
	id, err := m.serv.Create(r.Context(), *body)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"id": id}, "succeed")
}

func (m *userLoginApi) get(w http.ResponseWriter, r *http.Request) {

	opts, err := sharedapis.ParseListQueryOptions[entities.UserLogin](r)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}

	res, totalCnt, err := m.serv.Get(r.Context(), opts.Limit, opts.Offset, opts.Filters, opts.Sorters)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendPagingResult(w, res, opts.Limit, opts.Offset, totalCnt)
}

func (m *userLoginApi) getByEmail(w http.ResponseWriter, r *http.Request) {
	usermail := r.URL.Query().Get("email")

	res, err := m.serv.GetByEmail(r.Context(), usermail)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendResult(w, res)
}

func (m *userLoginApi) put(w http.ResponseWriter, r *http.Request) {
	body, err := sharedapis.DecodeRequestDto[inputdtos.UserLoginDto, entities.UserLogin](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	// Defence in depth: a superadmin may not change their OWN role (no self-elevation
	// games, and no accidental self-lockout). claims.RoleId is the live role — the
	// access middleware re-stamps it from the user store on each request.
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		if body.Id == claims.Id && body.UserRoleId != claims.RoleId {
			controllers.SendError(w, controllers.ErrLimitedAccess, "you cannot change your own role")
			return
		}
	}

	res, err := m.serv.Update(r.Context(), *body)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	// An account that has just been disabled must lose its live sessions. Done after the
	// update succeeds so a failed save never signs anyone out.
	if !body.IsActive {
		m.endSessionsFor(r, body.Id, "disabled")
	}
	m.recordUserAudit(r, services.ActionUserUpdate, body.Id, body.Email, map[string]any{"isActive": body.IsActive, "roleId": body.UserRoleId})

	controllers.SendResult(w, res, "succeed")
}

// recordUserAudit records an account-management action against the target user.
func (m *userLoginApi) recordUserAudit(r *http.Request, action string, targetId int64, targetEmail string, meta map[string]any) {
	if m.audit == nil {
		return
	}
	entry := services.AuditEntry{
		Action:     action,
		TargetType: "user",
		TargetId:   strconv.FormatInt(targetId, 10),
		Detail:     targetEmail,
		Metadata:   meta,
	}
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		entry.ActorId, entry.ActorEmail, entry.ActorRole = claims.Id, claims.Email, claims.RoleId
	}
	entry.ClientIp, entry.UserAgent = auditContext(r, m.trusted)
	m.audit.Record(r.Context(), entry)
}

func (m *userLoginApi) delete(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	res, err := m.serv.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	// A deleted account's sessions would otherwise keep working until they expired,
	// authenticating a user row that no longer exists.
	m.endSessionsFor(r, int64(id), "deleted")
	m.recordUserAudit(r, services.ActionUserDelete, int64(id), "", nil)

	controllers.SendResult(w, res, "succeed")
}
