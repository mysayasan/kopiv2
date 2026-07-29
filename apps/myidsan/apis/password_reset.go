package apis

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// passwordResetApi is the SUPERADMIN operator queue for account recovery: list the
// pending forgot-password requests, resolve one (issue a temporary password to hand
// over out-of-band), or dismiss a bogus one. The public request endpoint and the
// optional self-service email flow live elsewhere (login.go / federated_auth.go).
type passwordResetApi struct {
	auth    middlewares.AuthMidware
	service services.IPasswordResetService
	auditRecorder
}

// NewPasswordResetApi wires the superadmin reset-request queue. Clearing/issuing a
// credential is a privilege-affecting action, so the whole surface is superadmin-only
// (same gate as user-account management).
func NewPasswordResetApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	access *middlewares.AccessSessionMidware,
	service services.IPasswordResetService,
	audit services.IAuditService,
	stepUp services.IStepUpService,
	trustedProxies []string,
) {
	handler := &passwordResetApi{
		auth:          auth,
		service:       service,
		auditRecorder: newAuditRecorder(audit, trustedProxies),
	}

	group := router.PathPrefix("/password-reset").Subrouter()
	group.Use(auth.Middleware)
	group.Use(access.Middleware)
	group.Use(access.RequireSuperadmin)

	group.HandleFunc("", handler.list).Methods("GET")
	// Issuing a working credential for someone else's account requires proving the
	// operator is still present, not just that a session cookie exists.
	group.HandleFunc("/{id}/resolve", requireStepUp(stepUp, handler.resolve)).Methods("POST")
	group.HandleFunc("/{id}/dismiss", handler.dismiss).Methods("POST")
}

func (m *passwordResetApi) list(w http.ResponseWriter, r *http.Request) {
	rows, err := m.service.ListPending(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, rows)
}

func (m *passwordResetApi) resolve(w http.ResponseWriter, r *http.Request) {
	id, ok := m.requestId(w, r)
	if !ok {
		return
	}
	adminId := int64(0)
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		adminId = claims.Id
	}
	temp, err := m.service.Resolve(r.Context(), id, adminId)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrResetRequestNotFound):
			controllers.SendError(w, controllers.ErrNotFound, err.Error())
		case errors.Is(err, services.ErrThirdPartyOnlyAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, "this account signs in through an external identity provider — reset it there")
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}
	// Issuing a temporary password hands an operator a working credential for someone
	// else's account. The password itself is NEVER recorded — only that it happened, to
	// whom, and by whom.
	m.record(r, services.AuditEntry{
		Action:     services.ActionPasswordReset,
		TargetType: "user",
		TargetId:   strconv.FormatInt(id, 10),
		Detail:     "issued a temporary password from the reset queue",
		Metadata:   map[string]any{"channel": "operator_queue", "requestId": id},
	})

	// The temporary password is shown to the operator ONCE (must-change on first use).
	controllers.SendResult(w, map[string]any{"temporaryPassword": temp})
}

func (m *passwordResetApi) dismiss(w http.ResponseWriter, r *http.Request) {
	id, ok := m.requestId(w, r)
	if !ok {
		return
	}
	adminId := int64(0)
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		adminId = claims.Id
	}
	if err := m.service.Dismiss(r.Context(), id, adminId); err != nil {
		if errors.Is(err, services.ErrResetRequestNotFound) {
			controllers.SendError(w, controllers.ErrNotFound, err.Error())
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]bool{"ok": true})
}

func (m *passwordResetApi) requestId(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid request id")
		return 0, false
	}
	return id, true
}
