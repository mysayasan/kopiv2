package apphost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/login"
)

func TestBuildListenerSpecsRejectsSharedTLSAndNonTLSPort(t *testing.T) {
	enableTLS := true
	enableNonTLS := true
	cfg := &config.AppConfigModel{}
	cfg.Server.Hostnames = []string{"*"}
	cfg.Server.Ports = []int{3000}
	cfg.Server.EnableTLS = &enableTLS
	cfg.Server.EnableNonTLS = &enableNonTLS

	_, err := buildListenerSpecs(cfg)
	if err == nil {
		t.Fatal("expected shared TLS/non-TLS port config to be rejected")
	}
}

func TestBuildListenerSpecsAllowsExplicitTLSAndNonTLSPorts(t *testing.T) {
	cfg := &config.AppConfigModel{}
	cfg.Server.Hostnames = []string{"*"}
	cfg.Server.TLSPorts = []int{1001, 1002}
	cfg.Server.NonTLSPorts = []int{1003, 1004}

	listeners, err := buildListenerSpecs(cfg)
	if err != nil {
		t.Fatalf("expected listener specs, got error: %v", err)
	}
	if len(listeners) != 4 {
		t.Fatalf("expected four listeners, got %d", len(listeners))
	}

	expected := map[string]bool{
		":1001": true,
		":1002": true,
		":1003": false,
		":1004": false,
	}
	for _, listener := range listeners {
		useTLS, ok := expected[listener.Addr]
		if !ok {
			t.Fatalf("unexpected listener addr: %+v", listener)
		}
		if listener.UseTLS != useTLS {
			t.Fatalf("unexpected listener TLS mode for %s: %+v", listener.Addr, listener)
		}
		delete(expected, listener.Addr)
	}
	if len(expected) != 0 {
		t.Fatalf("missing listeners: %+v", expected)
	}
}

func TestBuildListenerSpecsRejectsExplicitOverlappingPort(t *testing.T) {
	cfg := &config.AppConfigModel{}
	cfg.Server.Hostnames = []string{"*"}
	cfg.Server.TLSPorts = []int{3000}
	cfg.Server.NonTLSPorts = []int{3000}

	_, err := buildListenerSpecs(cfg)
	if err == nil {
		t.Fatal("expected overlapping TLS/non-TLS port config to be rejected")
	}
}

func TestBuildListenerSpecsAllowsSingleServerMode(t *testing.T) {
	enableTLS := false
	enableNonTLS := true
	cfg := &config.AppConfigModel{}
	cfg.Server.Hostnames = []string{"*"}
	cfg.Server.Ports = []int{3000}
	cfg.Server.EnableTLS = &enableTLS
	cfg.Server.EnableNonTLS = &enableNonTLS

	listeners, err := buildListenerSpecs(cfg)
	if err != nil {
		t.Fatalf("expected listener specs, got error: %v", err)
	}
	if len(listeners) != 1 {
		t.Fatalf("expected one listener, got %d", len(listeners))
	}
	if listeners[0].Addr != ":3000" || listeners[0].UseTLS {
		t.Fatalf("unexpected listener spec: %+v", listeners[0])
	}
}

func TestNormalizeMetricsPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "default", in: "", want: "/metrics"},
		{name: "adds slash", in: "metrics", want: "/metrics"},
		{name: "keeps slash", in: "/internal/metrics", want: "/internal/metrics"},
		{name: "collapses double", in: "//metrics", want: "/metrics"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMetricsPath(tt.in); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestBuildCacheStoreDefaultProviderUsesInMemory(t *testing.T) {
	cfg := &config.AppConfigModel{}
	cfg.Cache.Provider = "default"
	cfg.Cache.TTLSeconds = 30

	store, provider, err := buildCacheStore(cfg)
	if err != nil {
		t.Fatalf("expected cache store, got error: %v", err)
	}
	defer store.Close()

	if provider != "inmemory" {
		t.Fatalf("provider got %q want inmemory", provider)
	}
}

func TestApplySensitiveConfigRequiresOAuthSecretsWhenProviderConfigured(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")

	cfg := &config.AppConfigModel{
		Login: &login.OAuthProvidersConfigModel{
			Google: &login.OAuth2ConfigModel{
				ClientId:    "google-client",
				RedirectUrl: "http://localhost/callback",
				Scopes:      []string{"profile"},
			},
		},
	}
	cfg.Jwt.Secret = "unit-test-secret"

	if err := applySensitiveConfig(cfg, ""); err == nil {
		t.Fatalf("expected configured oauth provider to require oauth secret")
	}
}

func TestApplySensitiveConfigReplacesWeakJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	const original = "{\n  \"jwt\": {\n    \"secret\": \"standalone-change-me\"\n  },\n  \"server\": {}\n}\n"
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := &config.AppConfigModel{}
	cfg.Jwt.Secret = "standalone-change-me"

	if err := applySensitiveConfig(cfg, configPath); err != nil {
		t.Fatalf("applySensitiveConfig: %v", err)
	}
	if isWeakJWTSecret(cfg.Jwt.Secret) {
		t.Fatalf("secret still weak after hardening: %q", cfg.Jwt.Secret)
	}

	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(saved), "standalone-change-me") {
		t.Fatal("placeholder secret still present in config file")
	}
	if !strings.Contains(string(saved), cfg.Jwt.Secret) {
		t.Fatal("generated secret was not persisted to config file")
	}
	// The surgical rewrite must leave the rest of the file intact.
	if !strings.Contains(string(saved), "\"server\": {}") {
		t.Fatal("surgical rewrite damaged the rest of the config file")
	}
}

func TestApplySensitiveConfigKeepsEnvJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "an-explicitly-strong-env-secret")

	cfg := &config.AppConfigModel{}
	cfg.Jwt.Secret = "standalone-change-me"

	if err := applySensitiveConfig(cfg, ""); err != nil {
		t.Fatalf("applySensitiveConfig: %v", err)
	}
	if cfg.Jwt.Secret != "an-explicitly-strong-env-secret" {
		t.Fatalf("env secret not applied: %q", cfg.Jwt.Secret)
	}
}
