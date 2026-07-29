package apis

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/infra/login"

	"github.com/gorilla/mux"
)

// recordingAudit captures entries instead of persisting them.
type recordingAudit struct {
	services.IAuditService
	entries []services.AuditEntry
}

func (a *recordingAudit) Record(_ context.Context, e services.AuditEntry) {
	a.entries = append(a.entries, e)
}

// stubProvider is a RedirectProvider whose Callback always fails, standing in for a
// tampered state, a failed code exchange, or an IdP that reports the email unverified.
type stubProvider struct {
	key string
	err error
	id  *login.Identity
}

func (s *stubProvider) Key() string                                 { return s.key }
func (s *stubProvider) DisplayName() string                         { return s.key }
func (s *stubProvider) Login(http.ResponseWriter, *http.Request)    {}
func (s *stubProvider) Callback(*http.Request) (*login.Identity, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.id, nil
}

func callbackFor(t *testing.T, p *stubProvider) *recordingAudit {
	t.Helper()
	audit := &recordingAudit{}
	registry := login.NewRegistry()
	registry.Register(p)
	api := &loginApi{providers: registry, audit: audit}

	router := mux.NewRouter()
	router.HandleFunc("/api/callback/{provider}", api.providerCallback).Methods("GET")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/callback/"+p.key+"?code=c&state=s", nil))
	return audit
}

// A live bench against Keycloak found that the federated callback audited only successes:
// a refused SSO sign-in — a tampered state, an unverified email, a disabled account — left
// no trace at all, while a refused PASSWORD login did. "Show me the failed sign-in
// attempts" silently omitted every SSO one. This locks the fix in.
func TestFederatedCallbackAuditsARefusedSignIn(t *testing.T) {
	audit := callbackFor(t, &stubProvider{
		key: "keycloak",
		err: errors.New("identity provider reports the account email as unverified: bob@bench.test"),
	})

	if len(audit.entries) == 0 {
		t.Fatal("a refused federated sign-in recorded nothing — the audit page would show silence where an SSO rejection happened")
	}
	e := audit.entries[0]
	if e.Action != services.ActionLoginFailure {
		t.Fatalf("action = %q want %q", e.Action, services.ActionLoginFailure)
	}
	if e.Outcome != services.OutcomeDenied {
		t.Fatalf("outcome = %q want %q", e.Outcome, services.OutcomeDenied)
	}
	// The reason has to survive into the record, or the event says only "something failed".
	if !strings.Contains(e.Detail, "unverified") {
		t.Fatalf("detail lost the reason: %q", e.Detail)
	}
	// And the event must name which login surface it came from.
	if e.Metadata["provider"] != "keycloak" {
		t.Fatalf("metadata provider = %v want keycloak", e.Metadata["provider"])
	}
	if e.Metadata["method"] != services.MethodOIDC {
		t.Fatalf("metadata method = %v want %q", e.Metadata["method"], services.MethodOIDC)
	}
}

// An identity that arrives without an email is refused too, and that refusal is equally
// worth recording — it is the shape a GitHub account with no public address produces.
func TestFederatedCallbackAuditsAnIdentityWithNoEmail(t *testing.T) {
	audit := callbackFor(t, &stubProvider{
		key: "github",
		id:  &login.Identity{Provider: "github", Subject: "12345", Email: ""},
	})

	if len(audit.entries) == 0 {
		t.Fatal("an identity with no email was refused without being audited")
	}
	e := audit.entries[0]
	if e.Action != services.ActionLoginFailure {
		t.Fatalf("action = %q want %q", e.Action, services.ActionLoginFailure)
	}
	// Nothing else identifies the caller, so the subject is the only attribution available.
	if e.ActorEmail != "12345" {
		t.Fatalf("actor = %q want the subject as the only available attribution", e.ActorEmail)
	}
	if e.Metadata["method"] != services.MethodSocial {
		t.Fatalf("github must classify as %q, got %v", services.MethodSocial, e.Metadata["method"])
	}
}

// Classification must not silently mislabel a generic OIDC provider as a social one: the
// audit filter offers a closed list of methods, and "social" for a corporate Keycloak
// would hide those events from the query an auditor actually runs.
func TestFederatedMethodClassification(t *testing.T) {
	for key, want := range map[string]string{
		"google":   services.MethodSocial,
		"github":   services.MethodSocial,
		"":         services.MethodSocial,
		"keycloak": services.MethodOIDC,
		"entra":    services.MethodOIDC,
	} {
		if got := federatedMethodForKey(key); got != want {
			t.Fatalf("federatedMethodForKey(%q) = %q want %q", key, got, want)
		}
	}
}

// The lockout must NOT advance on a federated failure. The credential was verified at the
// IdP, so counting these would let a misconfigured provider — a rotated secret, a clock
// skew — lock real users out of an address they never guessed a password from.
func TestFederatedCallbackFailureDoesNotFeedTheLockout(t *testing.T) {
	audit := &recordingAudit{}
	registry := login.NewRegistry()
	registry.Register(&stubProvider{key: "keycloak", err: errors.New("oauth state does not match")})
	// A REAL guard, armed to trip on the very first failure. If the handler routed through
	// recordLoginFailure (which advances the guard) instead of recordFederatedLoginFailure,
	// one callback would be enough to lock the address out — which is exactly what this
	// test has to be able to observe.
	guard := sharedapis.NewLoginGuard(sharedapis.LoginGuardConfig{
		Enabled:     true,
		MaxAttempts: 1,
		Window:      time.Hour,
		BaseLockout: time.Hour,
		MaxLockout:  time.Hour,
	})
	api := &loginApi{providers: registry, audit: audit, guard: guard}

	router := mux.NewRouter()
	router.HandleFunc("/api/callback/{provider}", api.providerCallback).Methods("GET")
	req := httptest.NewRequest(http.MethodGet, "/api/callback/keycloak?code=c&state=s", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code < 400 {
		t.Fatalf("a failed callback returned %d — it must not succeed", w.Code)
	}
	if locked, _ := guard.Locked(loginGuardKeys(req, "")...); locked {
		t.Fatal("one refused federated callback locked the address out — a misconfigured IdP could lock out every user, and none of them ever guessed a password")
	}
	for _, e := range audit.entries {
		if e.Action == services.ActionLoginLockout {
			t.Fatal("a federated callback failure engaged the failed-login lockout")
		}
	}
}
