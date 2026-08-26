package apis

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// MetricMfaChallengeTotal counts second-factor verification outcomes. A spike in
// "failed" is the signal that matters (an online guessing attempt against a known
// password), so the result label is a bounded enum, never a raw error string.
const MetricMfaChallengeTotal = "myidsan_mfa_challenge_total"

// mfaApi is the self-service second-factor surface: a signed-in user manages their
// OWN factor (enroll, confirm, regenerate recovery codes, disable). The pre-session
// login challenge lives in login.go (it has no session yet); admin reset of another
// user's factor is superadmin-gated and lives on its own sub-router below.
type mfaApi struct {
	auditRecorder
	auth    middlewares.AuthMidware
	service services.IMfaService
	users   services.IUserLoginService
	metrics telemetry.Metrics
	// guard throttles the ONE password check on this surface (disable). Without it a
	// hijacked session could grind the account password here as fast as the network allowed.
	guard *sharedapis.LoginGuard
}

// NewMfaApi wires the self-service MFA routes (auth-only: every route acts on the
// caller's own account) and the superadmin admin-reset route.
func NewMfaApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	access *middlewares.AccessSessionMidware,
	service services.IMfaService,
	users services.IUserLoginService,
	metrics telemetry.Metrics,
	audit services.IAuditService,
	stepUp services.IStepUpService,
	guard *sharedapis.LoginGuard,
	trustedProxies []string,
) {
	handler := &mfaApi{auth: auth, service: service, users: users, metrics: metrics, guard: guard, auditRecorder: newAuditRecorder(audit, trustedProxies)}

	// Self-service: any authenticated user, acting on their own account. No RBAC
	// matrix — mirrors /api/login/default/change-password.
	self := router.PathPrefix("/mfa").Subrouter()
	self.Use(auth.Middleware)
	self.HandleFunc("", handler.status).Methods("GET")
	self.HandleFunc("/enroll", handler.beginEnroll).Methods("POST")
	self.HandleFunc("/enroll/verify", handler.confirmEnroll).Methods("POST")
	self.HandleFunc("/recovery", handler.regenerateRecovery).Methods("POST")
	self.HandleFunc("", handler.disable).Methods("DELETE")

	// Admin reset of ANOTHER user's factor (lost-device case). Superadmin-only —
	// clearing a factor is a privilege-affecting action, same gate as role changes.
	admin := router.PathPrefix("/mfa-admin").Subrouter()
	admin.Use(auth.Middleware)
	admin.Use(access.Middleware)
	admin.Use(access.RequireSuperadmin)
	// Clearing someone else's second factor is exactly what an attacker with a stolen
	// cookie would do to take over an account, so it requires a fresh credential.
	admin.HandleFunc("/{id}", requireStepUp(stepUp, handler.adminReset)).Methods("DELETE")
}

func (m *mfaApi) claims(r *http.Request) *models.JwtCustomClaims {
	claims, _ := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	return claims
}

func (m *mfaApi) recordChallenge(result string) {
	if m.metrics == nil {
		return
	}
	m.metrics.Inc(MetricMfaChallengeTotal, telemetry.Labels{"result": result})
}

func (m *mfaApi) status(w http.ResponseWriter, r *http.Request) {
	claims := m.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	st, err := m.service.Status(r.Context(), claims.Id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, st)
}

func (m *mfaApi) beginEnroll(w http.ResponseWriter, r *http.Request) {
	claims := m.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&body)

	challenge, err := m.service.BeginEnroll(r.Context(), claims.Id, claims.Email, strings.TrimSpace(body.Label))
	if err != nil {
		if errors.Is(err, services.ErrMfaAlreadyEnrolled) {
			controllers.SendError(w, controllers.ErrConflict, err.Error())
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, challenge)
}

func (m *mfaApi) confirmEnroll(w http.ResponseWriter, r *http.Request) {
	claims := m.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	codes, err := m.service.ConfirmEnroll(r.Context(), claims.Id, body.Code)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrMfaBadCode):
			m.recordChallenge("enroll_failed")
			controllers.SendError(w, controllers.ErrAuthFailed, "that code did not match — check your authenticator and try again")
		case errors.Is(err, services.ErrMfaNotEnrolling):
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		case errors.Is(err, services.ErrMfaAlreadyEnrolled):
			controllers.SendError(w, controllers.ErrConflict, err.Error())
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}
	m.recordChallenge("enroll_confirmed")
	// Recorded at CONFIRMATION, not at BeginEnroll: staging a secret changes nothing about
	// how the account authenticates, whereas this is the moment the account starts owing a
	// second factor. Without it the trail cannot date the control it later shows being
	// removed — and "when did this account gain MFA" is the first question asked of any
	// account that mysteriously stops having it.
	m.record(r, services.AuditEntry{
		Action:     services.ActionMfaEnroll,
		TargetType: "self",
		TargetId:   strconv.FormatInt(claims.Id, 10),
		Detail:     "confirmed a second factor for this account",
		Metadata:   map[string]any{"recoveryCodes": len(codes)},
	})
	controllers.SendResult(w, map[string]any{"recoveryCodes": codes})
}

func (m *mfaApi) regenerateRecovery(w http.ResponseWriter, r *http.Request) {
	claims := m.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	// Re-prove possession before minting a new set — otherwise a hijacked session
	// could quietly rotate the break-glass codes.
	usedRecovery, err := m.requireValidCode(r, claims.Id, body.Code)
	if err != nil {
		m.writeCodeGateError(w, err)
		return
	}
	if usedRecovery {
		m.recordRecoveryBurn(r, claims.Id, "recovery-regenerate")
	}
	codes, err := m.service.RegenerateRecovery(r.Context(), claims.Id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// Rotating the set INVALIDATES every code the account holder has written down. Done by
	// the owner it is routine housekeeping; done by someone who has taken the session over
	// it is how the real owner is locked out of their own recovery path, and afterwards
	// there is nothing left to compare against — the old hashes are gone.
	m.record(r, services.AuditEntry{
		Action:     services.ActionMfaRegenerate,
		TargetType: "self",
		TargetId:   strconv.FormatInt(claims.Id, 10),
		Detail:     "issued a new recovery-code set, invalidating the previous one",
		Metadata:   map[string]any{"recoveryCodes": len(codes)},
	})
	controllers.SendResult(w, map[string]any{"recoveryCodes": codes})
}

func (m *mfaApi) disable(w http.ResponseWriter, r *http.Request) {
	claims := m.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	if selfThrottleLocked(w, r, m.guard, claims.Email) {
		return
	}
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	// Disabling MFA must re-prove possession of the factor (a valid code) so a
	// hijacked session cannot silently strip it. For local accounts we also require
	// the current password; directory (LDAP) accounts have no local password, so the
	// code alone stands.
	usedRecovery, err := m.requireValidCode(r, claims.Id, body.Code)
	if err != nil {
		m.writeCodeGateError(w, err)
		return
	}
	if _, err := m.users.AuthenticateDefault(r.Context(), claims.Email, body.Password); err != nil {
		// A third-party-only (directory/SSO) account has no local password to check;
		// the code gate above is sufficient. Any other failure is a real refusal.
		if !errors.Is(err, services.ErrThirdPartyOnlyAccount) {
			selfThrottleFailure(m.guard, r, claims.Email)
			controllers.SendError(w, controllers.ErrAuthFailed, "current password is incorrect")
			return
		}
	}
	if usedRecovery {
		m.recordRecoveryBurn(r, claims.Id, "mfa-disable")
	}
	if err := m.service.Disable(r.Context(), claims.Id); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// THE most important line this trail can hold. Disabling the second factor is what a
	// hijacked session does to make its hold on the account permanent, and the act erases
	// its own evidence: the factor row and every recovery-code hash are deleted, so
	// afterwards the account is indistinguishable from one that never enrolled. Without
	// this entry the operator cannot even establish that a factor once existed.
	m.record(r, services.AuditEntry{
		Action:     services.ActionMfaDisable,
		TargetType: "self",
		TargetId:   strconv.FormatInt(claims.Id, 10),
		Detail:     "removed the second factor from this account",
	})
	controllers.SendResult(w, map[string]bool{"ok": true})
}

func (m *mfaApi) adminReset(w http.ResponseWriter, r *http.Request) {
	userId, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || userId <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid user id")
		return
	}
	if err := m.service.Disable(r.Context(), userId); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// Clearing someone else's second factor removes a control they rely on and cannot
	// see happen. If it was not them who asked for it, this entry is how they find out.
	m.record(r, services.AuditEntry{
		Action:     services.ActionMfaAdminReset,
		TargetType: "user",
		TargetId:   strconv.FormatInt(userId, 10),
		Detail:     "administrator cleared this account's second factor",
	})
	controllers.SendResult(w, map[string]bool{"ok": true})
}

// requireValidCode verifies a TOTP or recovery code for the user, translating the
// service outcome into the sentinel errors writeCodeGateError understands. It reports
// whether a RECOVERY code was what satisfied the gate, so the caller can record the burn.
func (m *mfaApi) requireValidCode(r *http.Request, userId int64, code string) (bool, error) {
	if strings.TrimSpace(code) == "" {
		return false, services.ErrMfaBadCode
	}
	res, err := m.service.VerifyCode(r.Context(), userId, code)
	if err != nil {
		return false, err
	}
	if !res.Ok {
		return false, services.ErrMfaBadCode
	}
	return res.UsedRecovery, nil
}

// recordRecoveryBurn notes that one of the account's finite, single-use recovery codes was
// spent. `surface` says where, because the same burn means different things depending on
// it: at a login it means the authenticator is gone, while at a teardown gate it means
// someone is dismantling the second factor without one.
func (m *mfaApi) recordRecoveryBurn(r *http.Request, userId int64, surface string) {
	m.record(r, services.AuditEntry{
		Action:     services.ActionMfaRecovery,
		TargetType: "self",
		TargetId:   strconv.FormatInt(userId, 10),
		Detail:     "a single-use recovery code was spent",
		Metadata:   map[string]any{"method": services.MethodRecovery, "surface": surface},
	})
}

func (m *mfaApi) writeCodeGateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrMfaBadCode):
		m.recordChallenge("failed")
		controllers.SendError(w, controllers.ErrAuthFailed, "invalid verification code")
	case errors.Is(err, services.ErrMfaNotEnrolled):
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
	default:
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
	}
}
