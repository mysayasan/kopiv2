package apis

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/infra/login"

	"github.com/jcmturner/gokrb5/v8/iana/etypeID"
	"github.com/jcmturner/gokrb5/v8/keytab"
)

func benchKerberos(t *testing.T) *login.KerberosAuthenticator {
	t.Helper()
	kt := keytab.New()
	if err := kt.AddEntry("HTTP/idsan.kopi.test", "KOPI.TEST", "s3cret", time.Now(), 1, etypeID.AES256_CTS_HMAC_SHA1_96); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	raw, err := kt.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "bench.keytab")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write keytab: %v", err)
	}
	auth, err := login.NewKerberosAuthenticator(login.KerberosSettings{
		KeytabPath:       path,
		ServicePrincipal: "HTTP/idsan.kopi.test",
		OnlyRealms:       []string{"KOPI.TEST"},
	})
	if err != nil {
		t.Fatalf("NewKerberosAuthenticator: %v", err)
	}
	return auth
}

func kerberosRequest(t *testing.T, negotiateHeader string) *recordingAudit {
	t.Helper()
	audit := &recordingAudit{}
	api := &loginApi{kerberos: benchKerberos(t), audit: audit}

	r := httptest.NewRequest(http.MethodGet, "/api/login/kerberos", nil)
	if negotiateHeader != "" {
		r.Header.Set("Authorization", negotiateHeader)
	}
	api.kerberosLogin(httptest.NewRecorder(), r)
	return audit
}

// A live bench against a real Samba AD DC found that a REJECTED Kerberos ticket left no
// audit record at all — it only incremented a Prometheus counter — while a rejected LDAP
// password login was recorded. A forged or replayed SPNEGO token is precisely the event an
// investigation needs, so it must reach the trail.
func TestKerberosRejectedTicketIsAudited(t *testing.T) {
	audit := kerberosRequest(t, "Negotiate YII3RH1sTk9UQVJFQUxUSUNLRVQ=")

	if len(audit.entries) == 0 {
		t.Fatal("a rejected Kerberos ticket recorded nothing — a forged SPNEGO token would be invisible on the audit page")
	}
	e := audit.entries[0]
	if e.Action != services.ActionLoginFailure {
		t.Fatalf("action = %q want %q", e.Action, services.ActionLoginFailure)
	}
	if e.Outcome != services.OutcomeDenied {
		t.Fatalf("outcome = %q want %q", e.Outcome, services.OutcomeDenied)
	}
	if e.Metadata["method"] != services.MethodKerberos {
		t.Fatalf("metadata method = %v want %q", e.Metadata["method"], services.MethodKerberos)
	}
	if strings.TrimSpace(e.Detail) == "" {
		t.Fatal("the record must carry why the ticket was refused")
	}
}

// The counterpart, and the reason this cannot simply audit every failure path: a request
// carrying NO token is not an attack, it is the first half of every SPNEGO handshake. The
// server answers with a challenge and the browser retries. Recording it would add one
// entry per request and bury the rejections that matter.
func TestKerberosChallengeIsNotAudited(t *testing.T) {
	audit := kerberosRequest(t, "")

	if len(audit.entries) != 0 {
		t.Fatalf("the no-token challenge wrote %d audit entries; every page load would add one and drown the real events: %+v",
			len(audit.entries), audit.entries)
	}
}

// The challenge itself still has to be issued, or Kerberos SSO never starts.
func TestKerberosChallengeIsStillSent(t *testing.T) {
	api := &loginApi{kerberos: benchKerberos(t), audit: &recordingAudit{}}
	w := httptest.NewRecorder()
	api.kerberosLogin(w, httptest.NewRequest(http.MethodGet, "/api/login/kerberos", nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d want 401", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Negotiate") {
		t.Fatalf("WWW-Authenticate = %q want a Negotiate challenge", got)
	}
}
