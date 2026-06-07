package apphost

import (
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

	if err := applySensitiveConfig(cfg); err == nil {
		t.Fatalf("expected configured oauth provider to require oauth secret")
	}
}
