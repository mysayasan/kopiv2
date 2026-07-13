package apis

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/login"
)

func renderFor(t *testing.T, providers *login.OAuthProvidersConfigModel) string {
	t.Helper()
	m := &federatedAuthApi{cfg: &config.AppConfigModel{Login: providers}}
	w := httptest.NewRecorder()
	m.renderLoginPage(w, http.StatusOK, "/api/auth/authorize?client_id=myseliasan", "", "")
	return w.Body.String()
}

// The federated login page is the one server-rendered page in the suite, and it is the
// page myseliasan hands the browser to during SSO. myseliasan is deployed on an
// air-gapped intranet, so nothing on this page may reach a host we do not serve.
func TestLoginPageHasNoExternalReferences(t *testing.T) {
	body := renderFor(t, nil)
	for _, host := range []string{
		"googleapis.com", "gstatic.com", "fonts.google", "cdn.", "unpkg.com", "jsdelivr", "cdnjs",
	} {
		if strings.Contains(body, host) {
			t.Errorf("login page references external host %q — it must be fully self-hosted (intranet deployment)", host)
		}
	}
	// Every asset it does load must be same-origin (a rooted path, not a scheme).
	for _, ref := range []string{`href="/assets/fonts.css"`, `href="/assets/favicon.svg"`} {
		if !strings.Contains(body, ref) {
			t.Errorf("login page is missing self-hosted asset %s", ref)
		}
	}
	if strings.Contains(body, `href="http`) || strings.Contains(body, `src="http`) {
		t.Error("login page loads an absolute-URL asset; all assets must be same-origin paths")
	}
}

// A provider block with blank credentials is NOT a configured provider. The stock
// config.json ships empty google/github objects; rendering their buttons would send an
// intranet user off to accounts.google.com (and would fail with an empty client_id even
// on the internet).
func TestSocialButtonsRequireCredentials(t *testing.T) {
	cases := []struct {
		name      string
		providers *login.OAuthProvidersConfigModel
		want      bool
	}{
		{"no login block", nil, false},
		{"empty providers", &login.OAuthProvidersConfigModel{
			Google: &login.OAuth2ConfigModel{},
			GitHub: &login.OAuth2ConfigModel{},
		}, false},
		{"client id but no secret", &login.OAuthProvidersConfigModel{
			Google: &login.OAuth2ConfigModel{ClientId: "gid"},
		}, false},
		{"fully configured", &login.OAuthProvidersConfigModel{
			Google: &login.OAuth2ConfigModel{ClientId: "gid", ClientSecret: "gsec"},
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// ".oauth-btn" also appears in the page's <style> block, so match the link.
			got := strings.Contains(renderFor(t, c.providers), `href="/api/login/`)
			if got != c.want {
				t.Errorf("social buttons rendered=%v, want %v", got, c.want)
			}
		})
	}
}
