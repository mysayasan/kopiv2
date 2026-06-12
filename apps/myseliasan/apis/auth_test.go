package apis

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
)

func TestStartUsesConfiguredRedirectBaseURL(t *testing.T) {
	cfg := &config.AppConfigModel{}
	cfg.Jwt.Secret = "test-secret"
	cfg.SSO.ProviderBaseURL = "http://localhost:3001"
	cfg.SSO.Audience = "myseliasan"
	cfg.SSO.ClientID = "myseliasan"
	cfg.SSO.RedirectBaseURL = "http://localhost:3002"
	cfg.SSO.RedirectPath = "/api/auth/callback"
	cfg.SSO.SessionTTLSeconds = 3600

	router := mux.NewRouter()
	auth := middlewares.NewAuthWithConfig(middlewares.AuthConfig{
		Secret:       cfg.Jwt.Secret,
		Issuer:       "myidsan",
		Audience:     cfg.SSO.Audience,
		AppCode:      "myseliasan",
		SessionCache: cache.NewMemoryStore(time.Minute, time.Minute),
		SessionTTL:   time.Hour,
	})
	NewAuthApi(router, cfg, auth, cache.NewMemoryStore(time.Minute, time.Minute))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3002/auth/start?returnTo=/", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status got %d want %d body=%s", rr.Code, http.StatusFound, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	redirect, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location %q: %v", location, err)
	}
	if redirect.String() == "" {
		t.Fatalf("expected redirect location")
	}
	if got, want := redirect.Query().Get("redirect_uri"), "http://localhost:3002/api/auth/callback"; got != want {
		t.Fatalf("redirect_uri got %q want %q", got, want)
	}
}

func TestCallbackURLFallsBackToRequestHost(t *testing.T) {
	cfg := &config.AppConfigModel{}
	cfg.SSO.RedirectPath = "/api/auth/callback"

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3002/api/auth/start", nil)
	if got, want := callbackURL(req, cfg), "http://127.0.0.1:3002/api/auth/callback"; got != want {
		t.Fatalf("callbackURL got %q want %q", got, want)
	}
}

func TestExchangeCodeUsesConfiguredCACertPath(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/token" {
			t.Fatalf("unexpected token path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"accessToken":"token","userId":1,"roleId":2,"email":"user@example.test","name":"User","sessionId":"sid","issuer":"myidsan","audience":["myseliasan"],"appCode":"myseliasan","policyVersion":1}}`))
	}))
	defer server.Close()

	certPath := filepath.Join(t.TempDir(), "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if certPEM == nil {
		t.Fatalf("failed to encode test certificate")
	}
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		t.Fatalf("write ca cert: %v", err)
	}

	cfg := &config.AppConfigModel{}
	cfg.SSO.ProviderBaseURL = server.URL
	cfg.SSO.CACertPath = certPath
	cfg.SSO.ClientID = "myseliasan"
	cfg.SSO.ClientSecret = "secret"

	result, err := (&authApi{cfg: cfg}).exchangeCode(t.Context(), "code", "https://localhost/callback")
	if err != nil {
		t.Fatalf("exchangeCode failed: %v", err)
	}
	if result.AppCode != "myseliasan" || result.AccessToken != "token" {
		t.Fatalf("unexpected token result: %+v", result)
	}
}
