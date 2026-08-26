package apis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
)

// stubStepUp answers Verify from a script instead of checking a real credential.
type stubStepUp struct {
	err          error
	usedRecovery bool
	calls        int
}

func (s *stubStepUp) Verify(context.Context, int64, string, string, string, string) (bool, error) {
	s.calls++
	return s.usedRecovery, s.err
}
func (s *stubStepUp) IsRecent(context.Context, string) bool { return false }
func (s *stubStepUp) Window() time.Duration                 { return services.StepUpWindow }

// stepUpRequest builds an authenticated POST /api/step-up. The identity comes from the
// claims, exactly as the middleware would supply it.
func stepUpRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/step-up", strings.NewReader(`{"password":"guess"}`))
	r.RemoteAddr = "203.0.113.7:41234"
	claims := &models.JwtCustomClaims{Id: 1, Email: "admin@bench.test", SessionId: "sess-1"}
	return r.WithContext(context.WithValue(r.Context(), enumauth.Claims, claims))
}

func stepUpApiForTest(stub *stubStepUp) (*stepUpApi, *recordingAudit) {
	audit := &recordingAudit{}
	// Three attempts and no artificial delay: the point under test is that the counter
	// exists and engages, not the shipped tuning, and a real FailedDelay would just make
	// this test slow.
	guard := sharedapis.NewLoginGuard(sharedapis.LoginGuardConfig{
		Enabled:     true,
		MaxAttempts: 3,
		Window:      5 * time.Minute,
		BaseLockout: time.Minute,
		MaxLockout:  time.Hour,
	})
	return &stepUpApi{stepUp: stub, guard: guard, auditRecorder: newAuditRecorder(audit, nil)}, audit
}

// A live bench guessed twelve wrong step-up passwords in 0.6 seconds, all refused, none
// throttled. POST /api/step-up takes a password and reports whether it was right, which
// makes it a password-checking endpoint — and it was the only one on this server with no
// lockout behind it. An attacker holding a stolen cookie cannot sign in (they lack the
// password) but could ask this endpoint about candidates at wire speed until they found it,
// which is the one credential the whole step-up control rests on.
func TestStepUpLocksOutRepeatedWrongPasswords(t *testing.T) {
	api, _ := stepUpApiForTest(&stubStepUp{err: services.ErrInvalidCredential})

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		api.verify(w, stepUpRequest())
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d want %d", i+1, w.Code, http.StatusUnauthorized)
		}
	}

	w := httptest.NewRecorder()
	api.verify(w, stepUpRequest())
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("a fourth wrong password was still answered %d — step-up is an unmetered "+
			"password-guessing oracle for anyone holding the session cookie", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After on the lockout: the client cannot tell a throttle from a refusal")
	}
	// The SPA shows this text verbatim inside the re-authentication prompt. Telling a
	// signed-in operator there were "too many failed LOGIN attempts" describes something
	// they did not do, and the wait has to be readable without opening dev tools.
	body := w.Body.String()
	if strings.Contains(body, "login attempts") {
		t.Errorf("step-up lockout reports a LOGIN lockout to someone who is signed in: %s", body)
	}
	if !strings.Contains(body, "re-authentication") || !strings.Contains(body, "seconds") {
		t.Errorf("the lockout does not say what was throttled or for how long: %s", body)
	}
}

// The lockout must be checked BEFORE the credential, or a locked-out caller still costs a
// bcrypt comparison per attempt and the throttle becomes the load.
func TestStepUpLockoutRefusesWithoutCheckingTheCredential(t *testing.T) {
	stub := &stubStepUp{err: services.ErrInvalidCredential}
	api, _ := stepUpApiForTest(stub)

	for i := 0; i < 4; i++ {
		api.verify(httptest.NewRecorder(), stepUpRequest())
	}
	before := stub.calls
	api.verify(httptest.NewRecorder(), stepUpRequest())
	if stub.calls != before {
		t.Fatalf("the credential was verified %d more times while locked out — the throttle "+
			"is doing the work it exists to prevent", stub.calls-before)
	}
}

// A correct password clears the counters, so an operator who fat-fingers twice and then
// succeeds is not left locked out of their own re-authentication.
func TestStepUpSuccessClearsTheLockoutCounters(t *testing.T) {
	stub := &stubStepUp{err: services.ErrInvalidCredential}
	api, _ := stepUpApiForTest(stub)

	api.verify(httptest.NewRecorder(), stepUpRequest())
	api.verify(httptest.NewRecorder(), stepUpRequest())

	stub.err = nil
	w := httptest.NewRecorder()
	api.verify(w, stepUpRequest())
	if w.Code != http.StatusOK {
		t.Fatalf("the correct password was answered %d", w.Code)
	}

	stub.err = services.ErrInvalidCredential
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		api.verify(w, stepUpRequest())
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("locked out after %d fresh failures — the successful step-up did not "+
				"reset the counters", i+1)
		}
	}
}

// A recovery code spent to elevate is a break-glass secret gone for good. The step-up entry
// alone says somebody re-authenticated, which is the routine case and looks identical.
func TestStepUpRecordsARecoveryCodeBurn(t *testing.T) {
	api, audit := stepUpApiForTest(&stubStepUp{usedRecovery: true})

	w := httptest.NewRecorder()
	api.verify(w, stepUpRequest())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d want 200", w.Code)
	}

	var burn *services.AuditEntry
	for i := range audit.entries {
		if audit.entries[i].Action == services.ActionMfaRecovery {
			burn = &audit.entries[i]
		}
	}
	if burn == nil {
		t.Fatal("spending a recovery code at step-up recorded nothing distinguishable from " +
			"an ordinary re-authentication")
	}
	if burn.Metadata["method"] != services.MethodRecovery {
		t.Errorf("method = %v want %q", burn.Metadata["method"], services.MethodRecovery)
	}
}

// A TOTP-backed step-up must NOT look like a recovery burn, or the alert that matters fires
// on every routine elevation and stops meaning anything.
func TestStepUpDoesNotClaimARecoveryBurnForAnOrdinaryCode(t *testing.T) {
	api, audit := stepUpApiForTest(&stubStepUp{usedRecovery: false})

	api.verify(httptest.NewRecorder(), stepUpRequest())

	for _, e := range audit.entries {
		if e.Action == services.ActionMfaRecovery {
			t.Fatal("an ordinary step-up was recorded as a recovery-code burn")
		}
	}
}
