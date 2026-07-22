package login

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jcmturner/gokrb5/v8/iana/etypeID"
	"github.com/jcmturner/gokrb5/v8/keytab"
)

func writeTestKeytab(t *testing.T) string {
	t.Helper()
	kt := keytab.New()
	if err := kt.AddEntry("HTTP/myidsan.corp.local", "CORP.LOCAL", "s3cret", time.Now(), 1, etypeID.AES256_CTS_HMAC_SHA1_96); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	raw, err := kt.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.keytab")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write keytab: %v", err)
	}
	return path
}

func TestNewKerberosAuthenticator_LoadsKeytab(t *testing.T) {
	auth, err := NewKerberosAuthenticator(KerberosSettings{
		KeytabPath:       writeTestKeytab(t),
		ServicePrincipal: "HTTP/myidsan.corp.local",
		OnlyRealms:       []string{" corp.local "},
	})
	if err != nil {
		t.Fatalf("NewKerberosAuthenticator: %v", err)
	}
	if !auth.onlyRealms["CORP.LOCAL"] {
		t.Fatal("realm allow-list not normalized to uppercase")
	}

	if _, err := NewKerberosAuthenticator(KerberosSettings{KeytabPath: ""}); err == nil {
		t.Fatal("empty keytab path accepted")
	}
	if _, err := NewKerberosAuthenticator(KerberosSettings{KeytabPath: filepath.Join(t.TempDir(), "missing.keytab")}); err == nil {
		t.Fatal("missing keytab file accepted")
	}
}

// No Authorization header (or a non-Negotiate one) must yield ErrKerberosNoToken —
// the "issue the challenge" signal — never a hard failure.
func TestNegotiate_NoTokenMeansChallenge(t *testing.T) {
	auth, err := NewKerberosAuthenticator(KerberosSettings{KeytabPath: writeTestKeytab(t)})
	if err != nil {
		t.Fatalf("NewKerberosAuthenticator: %v", err)
	}

	for _, header := range []string{"", "Bearer abc123", "Basic dXNlcjpwdw=="} {
		r := httptest.NewRequest(http.MethodGet, "/api/login/kerberos", nil)
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		if _, err := auth.Negotiate(r); !errors.Is(err, ErrKerberosNoToken) {
			t.Errorf("header %q: err = %v, want ErrKerberosNoToken", header, err)
		}
	}
}

// Garbage Negotiate tokens are rejections (a token WAS presented), not challenges —
// re-challenging a broken client would loop the browser.
func TestNegotiate_GarbageTokenRejected(t *testing.T) {
	auth, err := NewKerberosAuthenticator(KerberosSettings{KeytabPath: writeTestKeytab(t)})
	if err != nil {
		t.Fatalf("NewKerberosAuthenticator: %v", err)
	}

	for _, token := range []string{"!!!not-base64!!!", "aGVsbG8gd29ybGQ="} { // invalid b64; valid b64, not SPNEGO
		r := httptest.NewRequest(http.MethodGet, "/api/login/kerberos", nil)
		r.Header.Set("Authorization", "Negotiate "+token)
		if _, err := auth.Negotiate(r); !errors.Is(err, ErrKerberosRejected) {
			t.Errorf("token %q: err = %v, want ErrKerberosRejected", token, err)
		}
	}
}

func TestKerberosChallenge(t *testing.T) {
	w := httptest.NewRecorder()
	KerberosChallenge(w)
	if w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") != "Negotiate" {
		t.Fatalf("challenge = %d %q", w.Code, w.Header().Get("WWW-Authenticate"))
	}
}

func TestStandaloneKerberosIdentity(t *testing.T) {
	id := StandaloneKerberosIdentity(&KerberosPrincipal{Username: "Alice", Realm: "CORP.LOCAL"})
	if id.Provider != "kerberos" || id.Subject != "alice@corp.local" || id.Email != "alice@corp.local" {
		t.Fatalf("unexpected identity: %+v", id)
	}
	if StandaloneKerberosIdentity(nil) != nil {
		t.Fatal("nil principal must yield nil identity")
	}
}
