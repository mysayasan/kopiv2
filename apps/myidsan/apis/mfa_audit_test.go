package apis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
)

// stubMfa scripts the second-factor service. Only the methods these handlers reach are
// implemented; the embedded interface makes the rest a compile-time promise rather than a
// wall of panics.
type stubMfa struct {
	services.IMfaService
	verify       services.MfaVerifyResult
	verifyErr    error
	confirmCodes []string
	confirmErr   error
	disabled     []int64
}

func (s *stubMfa) VerifyCode(context.Context, int64, string) (services.MfaVerifyResult, error) {
	return s.verify, s.verifyErr
}
func (s *stubMfa) ConfirmEnroll(context.Context, int64, string) ([]string, error) {
	return s.confirmCodes, s.confirmErr
}
func (s *stubMfa) RegenerateRecovery(context.Context, int64) ([]string, error) {
	return s.confirmCodes, nil
}
func (s *stubMfa) Disable(_ context.Context, userId int64) error {
	s.disabled = append(s.disabled, userId)
	return nil
}

// stubUsers accepts any password, so a disable test measures the audit rather than the
// credential check that has its own coverage.
type stubUsers struct{ services.IUserLoginService }

func (stubUsers) AuthenticateDefault(context.Context, string, string) (*entities.UserLogin, error) {
	return &entities.UserLogin{Id: 1, Email: "admin@bench.test"}, nil
}

func mfaApiForTest(mfa *stubMfa) (*mfaApi, *recordingAudit) {
	audit := &recordingAudit{}
	return &mfaApi{service: mfa, users: stubUsers{}, auditRecorder: newAuditRecorder(audit, nil)}, audit
}

func mfaRequest(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/api/mfa", strings.NewReader(body))
	r.RemoteAddr = "203.0.113.9:5555"
	claims := &models.JwtCustomClaims{Id: 1, Email: "admin@bench.test", SessionId: "sess-1"}
	return r.WithContext(context.WithValue(r.Context(), enumauth.Claims, claims))
}

func actionsIn(audit *recordingAudit) map[string]int {
	out := map[string]int{}
	for _, e := range audit.entries {
		out[e.Action]++
	}
	return out
}

// A live bench found the whole MFA lifecycle absent from the trail: five actions were
// declared in services/audit.go and never written by anything. `mfa.disable` is the worst
// of them, because removing the second factor DELETES the factor row and every
// recovery-code hash — afterwards the account is indistinguishable from one that never
// enrolled, so without this entry an operator cannot even establish that a factor existed,
// let alone who removed it.
func TestDisablingTheSecondFactorIsAudited(t *testing.T) {
	api, audit := mfaApiForTest(&stubMfa{verify: services.MfaVerifyResult{Ok: true}})

	w := httptest.NewRecorder()
	api.disable(w, mfaRequest(http.MethodDelete, `{"password":"pw","code":"123456"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d want 200: %s", w.Code, w.Body.String())
	}
	if got := actionsIn(audit)[services.ActionMfaDisable]; got != 1 {
		t.Fatalf("mfa.disable entries = %d want 1 — the single most important line this "+
			"trail can hold, and the act erases its own evidence", got)
	}
}

// Recorded at CONFIRMATION rather than at BeginEnroll: staging a secret changes nothing
// about how the account authenticates. Without it the trail cannot date the control it
// later shows being removed.
func TestConfirmingEnrollmentIsAudited(t *testing.T) {
	api, audit := mfaApiForTest(&stubMfa{confirmCodes: []string{"a", "b", "c"}})

	w := httptest.NewRecorder()
	api.confirmEnroll(w, mfaRequest(http.MethodPost, `{"code":"123456"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d want 200: %s", w.Code, w.Body.String())
	}
	entries := actionsIn(audit)
	if entries[services.ActionMfaEnroll] != 1 {
		t.Fatalf("mfa.enroll entries = %d want 1", entries[services.ActionMfaEnroll])
	}
}

// A refused enrollment must not be recorded as one that happened.
func TestARefusedEnrollmentIsNotAuditedAsAnEnrollment(t *testing.T) {
	api, audit := mfaApiForTest(&stubMfa{confirmErr: services.ErrMfaBadCode})

	w := httptest.NewRecorder()
	api.confirmEnroll(w, mfaRequest(http.MethodPost, `{"code":"000000"}`))
	if w.Code == http.StatusOK {
		t.Fatalf("a bad code confirmed the factor")
	}
	if got := actionsIn(audit)[services.ActionMfaEnroll]; got != 0 {
		t.Fatalf("mfa.enroll entries = %d want 0 — a refusal was written as an enrollment", got)
	}
}

// Rotating the set invalidates every code the holder has written down. Done by the owner it
// is housekeeping; done by whoever has taken the session over it is how the real owner
// loses their recovery path — and afterwards the old hashes are gone, so there is nothing
// left to compare against.
func TestRegeneratingRecoveryCodesIsAudited(t *testing.T) {
	api, audit := mfaApiForTest(&stubMfa{
		verify:       services.MfaVerifyResult{Ok: true},
		confirmCodes: []string{"a", "b"},
	})

	w := httptest.NewRecorder()
	api.regenerateRecovery(w, mfaRequest(http.MethodPost, `{"code":"123456"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d want 200: %s", w.Code, w.Body.String())
	}
	if got := actionsIn(audit)[services.ActionMfaRegenerate]; got != 1 {
		t.Fatalf("mfa.recovery_regenerate entries = %d want 1", got)
	}
}

// A recovery code spent at a teardown gate means someone is dismantling the second factor
// WITHOUT the authenticator — a materially different event from doing it with one, and one
// the burn count is the only record of.
func TestARecoveryCodeSpentAtTeardownIsAuditedSeparately(t *testing.T) {
	api, audit := mfaApiForTest(&stubMfa{
		verify: services.MfaVerifyResult{Ok: true, UsedRecovery: true},
	})

	api.disable(httptest.NewRecorder(), mfaRequest(http.MethodDelete, `{"password":"pw","code":"AAAAA-BBBBB"}`))

	entries := actionsIn(audit)
	if entries[services.ActionMfaRecovery] != 1 {
		t.Fatalf("mfa.recovery_used entries = %d want 1", entries[services.ActionMfaRecovery])
	}
	if entries[services.ActionMfaDisable] != 1 {
		t.Fatalf("mfa.disable entries = %d want 1 — the burn must not replace the removal", entries[services.ActionMfaDisable])
	}
}

// The mirror image: a TOTP-driven teardown must not be reported as a recovery burn, or the
// signal fires on the routine case and stops meaning anything.
func TestATotpTeardownIsNotReportedAsARecoveryBurn(t *testing.T) {
	api, audit := mfaApiForTest(&stubMfa{verify: services.MfaVerifyResult{Ok: true}})

	api.disable(httptest.NewRecorder(), mfaRequest(http.MethodDelete, `{"password":"pw","code":"123456"}`))

	if got := actionsIn(audit)[services.ActionMfaRecovery]; got != 0 {
		t.Fatalf("mfa.recovery_used entries = %d want 0", got)
	}
}
