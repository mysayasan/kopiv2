package apis

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// stepUpErrorCode is the sentinel the SPA looks for to decide "prompt for the password"
// instead of "show an error". A generic 403 would leave the operator stuck: the action is
// legitimately theirs to take, they simply have to prove it is still them.
const stepUpErrorCode = "step_up_required"

type stepUpApi struct {
	stepUp services.IStepUpService
	auditRecorder
}

// NewStepUpApi mounts re-authentication.
//
//	GET  /api/step-up   is this session currently elevated, and for how long
//	POST /api/step-up   prove the password (+ TOTP when enrolled) to elevate it
//
// Authenticated but NOT superadmin-gated: an ordinary user has nothing to elevate today,
// but the endpoint must work for whoever the gated actions are later extended to, and
// restricting it would only mean a role change could never be re-authenticated by the
// person about to make one.
func NewStepUpApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	stepUp services.IStepUpService,
	audit services.IAuditService,
	trustedProxies []string,
) {
	h := &stepUpApi{stepUp: stepUp, auditRecorder: newAuditRecorder(audit, trustedProxies)}

	group := router.PathPrefix("/step-up").Subrouter()
	group.Use(auth.Middleware)
	group.HandleFunc("", h.status).Methods("GET")
	group.HandleFunc("", h.verify).Methods("POST")
}

func (a *stepUpApi) status(w http.ResponseWriter, r *http.Request) {
	claims := callerClaims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "not signed in")
		return
	}
	controllers.SendResult(w, map[string]any{
		"elevated":      a.stepUp.IsRecent(r.Context(), claims.SessionId),
		"windowSeconds": int(a.stepUp.Window().Seconds()),
	})
}

func (a *stepUpApi) verify(w http.ResponseWriter, r *http.Request) {
	claims := callerClaims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "not signed in")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}

	// The identity comes from the session claims, never the body.
	err := a.stepUp.Verify(r.Context(), claims.Id, claims.Email, claims.SessionId, body.Password, body.Code)
	if err != nil {
		// A failed step-up is a security event in its own right: it is what an attacker
		// holding only a stolen cookie would produce while trying to escalate.
		a.record(r, services.AuditEntry{
			Action:     services.ActionStepUpFailure,
			TargetType: "self",
			TargetId:   strconv.FormatInt(claims.Id, 10),
			Outcome:    services.OutcomeDenied,
			Detail:     "re-authentication failed",
		})
		switch {
		case errors.Is(err, services.ErrInvalidCredential), errors.Is(err, services.ErrMfaBadCode):
			// One message for both, so this cannot be used to learn whether the account
			// has a second factor enrolled.
			controllers.SendError(w, controllers.ErrAuthFailed, "those credentials were not accepted")
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}

	a.record(r, services.AuditEntry{
		Action:     services.ActionStepUpSuccess,
		TargetType: "self",
		TargetId:   strconv.FormatInt(claims.Id, 10),
		Detail:     "re-authenticated for a sensitive action",
		Metadata:   map[string]any{"windowSeconds": int(a.stepUp.Window().Seconds())},
	})
	controllers.SendResult(w, map[string]any{
		"elevated":      true,
		"windowSeconds": int(a.stepUp.Window().Seconds()),
	})
}

// requireStepUp guards one sensitive handler. It is a plain wrapper rather than middleware
// because it is applied to individual routes, not whole subrouters — most of what a
// superadmin does does not warrant re-typing a password.
//
// A nil service means step-up is not configured, in which case the guard is inert. That is
// a deliberate choice for an install with no cache: the alternative would be to make the
// affected admin actions permanently unreachable.
func requireStepUp(stepUp services.IStepUpService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if stepUp == nil {
			next(w, r)
			return
		}
		claims := callerClaims(r)
		if claims == nil {
			controllers.SendError(w, controllers.ErrAuthFailed, "not signed in")
			return
		}
		if !stepUp.IsRecent(r.Context(), claims.SessionId) {
			// The code travels in the message because SendError has no structured-detail
			// channel; the SPA matches on it to open the re-authentication prompt.
			controllers.SendError(w, controllers.ErrLimitedAccess, stepUpErrorCode)
			return
		}
		next(w, r)
	}
}
