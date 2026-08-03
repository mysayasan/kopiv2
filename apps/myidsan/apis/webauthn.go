package apis

import (
	"bytes"
	"encoding/json"
	"errors"
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

// maxWebAuthnBody caps a ceremony response. An attestation object with a long certificate
// chain is a few KiB; 128 KiB is far beyond any legitimate response.
const maxWebAuthnBody = 128 << 10

// webAuthnApi is the security-key surface. It has three parts, gated differently:
//
//	/mfa/webauthn        self-service on the caller's OWN account (auth-only, no matrix)
//	/mfa-admin/{id}/…    clearing SOMEONE ELSE's keys (superadmin + step-up)
//
// The pre-session login legs are NOT here — they live on the login API, because they run
// before any session exists and must be reachable without one. See NewLoginApi.
type webAuthnApi struct {
	auditRecorder
	service services.IWebAuthnService
	users   services.IUserLoginService
	mfa     services.IMfaService
}

// NewWebAuthnApi wires the self-service and admin security-key routes.
func NewWebAuthnApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	access *middlewares.AccessSessionMidware,
	service services.IWebAuthnService,
	users services.IUserLoginService,
	mfa services.IMfaService,
	audit services.IAuditService,
	stepUp services.IStepUpService,
	trustedProxies []string,
) {
	h := &webAuthnApi{
		service:       service,
		users:         users,
		mfa:           mfa,
		auditRecorder: newAuditRecorder(audit, trustedProxies),
	}

	// Self-service. Mirrors /mfa: any authenticated user, acting only on themselves.
	self := router.PathPrefix("/mfa/webauthn").Subrouter()
	self.Use(auth.Middleware)
	self.HandleFunc("", h.list).Methods("GET")
	self.HandleFunc("/register/begin", h.beginRegister).Methods("POST")
	self.HandleFunc("/register/finish", h.finishRegister).Methods("POST")
	self.HandleFunc("/{id}", h.rename).Methods("PUT")
	self.HandleFunc("/{id}", h.remove).Methods("DELETE")

	// Admin: clear another account's keys (lost-device case). Superadmin + step-up, the
	// same gate as clearing a TOTP factor — it removes a control the owner relies on.
	admin := router.PathPrefix("/mfa-admin/{id}/webauthn").Subrouter()
	admin.Use(auth.Middleware)
	admin.Use(access.Middleware)
	admin.Use(access.RequireSuperadmin)
	admin.HandleFunc("", requireStepUp(stepUp, h.adminResetAll)).Methods("DELETE")
}

func (h *webAuthnApi) claims(r *http.Request) *models.JwtCustomClaims {
	claims, _ := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	return claims
}

// enrollStateKey namespaces the in-flight enrolment ceremony to the account. The user
// already holds a session here, so their own id is a safe and self-cleaning handle — a
// second concurrent enrolment simply replaces the first rather than accumulating state.
func enrollStateKey(userId int64) string {
	return "enroll:" + strconv.FormatInt(userId, 10)
}

func (h *webAuthnApi) list(w http.ResponseWriter, r *http.Request) {
	claims := h.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	keys, err := h.service.List(r.Context(), claims.Id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if keys == nil {
		keys = []services.WebAuthnCredentialView{}
	}
	// enabled travels with the list so the UI can explain an empty state ("switched off on
	// this server") instead of silently offering an Add button that will fail.
	controllers.SendResult(w, map[string]any{
		"enabled": h.service.Enabled(),
		"keys":    keys,
	})
}

func (h *webAuthnApi) beginRegister(w http.ResponseWriter, r *http.Request) {
	claims := h.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	display := strings.TrimSpace(claims.Email)
	creation, err := h.service.BeginEnroll(r.Context(), r, enrollStateKey(claims.Id), claims.Id, claims.Email, display)
	if err != nil {
		h.writeErr(w, err)
		return
	}
	// The browser needs exactly the `publicKey` options object; hand back the library's
	// own shape so no field is lost in translation.
	controllers.SendResult(w, creation)
}

func (h *webAuthnApi) finishRegister(w http.ResponseWriter, r *http.Request) {
	claims := h.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	// The body carries both the label the user typed and the authenticator's response, so
	// it is read once and the credential half handed to the verifier as its own reader.
	var body struct {
		Label      string          `json:"label"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxWebAuthnBody)).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if len(body.Credential) == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "credential is required")
		return
	}

	view, err := h.service.FinishEnroll(r.Context(), r, enrollStateKey(claims.Id), claims.Id,
		body.Label, bytes.NewReader(body.Credential))
	if err != nil {
		h.writeErr(w, err)
		return
	}
	// A new second factor on an account is exactly what an attacker holding a stolen
	// session would add to keep access. The owner must be able to see it happened.
	h.record(r, services.AuditEntry{
		Action:     services.ActionWebAuthnEnroll,
		TargetType: "webauthn",
		TargetId:   strconv.FormatInt(view.Id, 10),
		Detail:     "security key enrolled: " + view.Label,
	})
	controllers.SendResult(w, view)
}

func (h *webAuthnApi) rename(w http.ResponseWriter, r *http.Request) {
	claims := h.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	rowId, ok := h.pathId(w, r)
	if !ok {
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := h.service.Rename(r.Context(), claims.Id, rowId, body.Label); err != nil {
		h.writeErr(w, err)
		return
	}
	h.record(r, services.AuditEntry{
		Action:     services.ActionWebAuthnRename,
		TargetType: "webauthn",
		TargetId:   strconv.FormatInt(rowId, 10),
		Detail:     "security key renamed to " + strings.TrimSpace(body.Label),
	})
	controllers.SendResult(w, map[string]bool{"ok": true})
}

// remove deletes one of the caller's own keys.
//
// It re-proves identity first, for the same reason disabling TOTP does: a hijacked session
// must not be able to strip the factor that would have stopped it. The proof accepted is
// the account password — or, for an account with no local password (directory/SSO), the
// fact that it has none, since there is nothing to prove against. A TOTP code is accepted
// as an alternative when one is enrolled.
func (h *webAuthnApi) remove(w http.ResponseWriter, r *http.Request) {
	claims := h.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	rowId, ok := h.pathId(w, r)
	if !ok {
		return
	}
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&body)

	if err := h.reproveIdentity(r, claims, body.Password, body.Code); err != nil {
		controllers.SendError(w, controllers.ErrAuthFailed, err.Error())
		return
	}
	if err := h.service.Delete(r.Context(), claims.Id, rowId); err != nil {
		h.writeErr(w, err)
		return
	}
	h.record(r, services.AuditEntry{
		Action:     services.ActionWebAuthnRemove,
		TargetType: "webauthn",
		TargetId:   strconv.FormatInt(rowId, 10),
		Detail:     "security key removed by its owner",
	})
	controllers.SendResult(w, map[string]bool{"ok": true})
}

func (h *webAuthnApi) adminResetAll(w http.ResponseWriter, r *http.Request) {
	userId, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || userId <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid user id")
		return
	}
	if err := h.service.DeleteAllForUser(r.Context(), userId); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	h.record(r, services.AuditEntry{
		Action:     services.ActionWebAuthnAdminReset,
		TargetType: "user",
		TargetId:   strconv.FormatInt(userId, 10),
		Detail:     "administrator removed every security key on this account",
	})
	controllers.SendResult(w, map[string]bool{"ok": true})
}

// reproveIdentity accepts either the account password or a valid TOTP code. Returns nil
// when the caller has re-proven who they are, or when the account genuinely has no local
// credential to prove against.
func (h *webAuthnApi) reproveIdentity(r *http.Request, claims *models.JwtCustomClaims, password, code string) error {
	if strings.TrimSpace(code) != "" && h.mfa != nil {
		if ok, err := h.mfa.VerifyCode(r.Context(), claims.Id, code); err == nil && ok {
			return nil
		}
	}
	if h.users == nil {
		return errors.New("current password is required")
	}
	if _, err := h.users.AuthenticateDefault(r.Context(), claims.Email, password); err != nil {
		if errors.Is(err, services.ErrThirdPartyOnlyAccount) {
			// No local password exists for this account; there is nothing to re-prove.
			return nil
		}
		return errors.New("current password is incorrect")
	}
	return nil
}

func (h *webAuthnApi) pathId(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid security key id")
		return 0, false
	}
	return id, true
}

// writeErr maps service errors onto status codes. A ceremony failure is deliberately NOT
// reported verbatim in every case: the library's messages are precise about which check
// failed, which is useful in a log and needlessly instructive in a response.
func (h *webAuthnApi) writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrWebAuthnDisabled):
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
	case errors.Is(err, services.ErrWebAuthnNoCeremony):
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
	case errors.Is(err, services.ErrWebAuthnNotFound):
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
	case errors.Is(err, services.ErrWebAuthnNoKeys):
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
	default:
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
	}
}
