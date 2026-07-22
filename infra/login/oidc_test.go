package login

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOidcIdentityFromClaims(t *testing.T) {
	claims := map[string]any{
		"email":          "alice@corp.local",
		"email_verified": true,
		"name":           "Alice Doe",
		"given_name":     "Alice",
		"family_name":    "Doe",
		"groups":         []any{"kopiv2-admins", "everyone", ""},
	}
	id, err := OidcIdentityFromClaims("keycloak", "sub-1", claims, "groups", false)
	if err != nil {
		t.Fatalf("OidcIdentityFromClaims: %v", err)
	}
	if id.Provider != "oidc:keycloak" {
		t.Errorf("provider = %q, want namespaced oidc:keycloak", id.Provider)
	}
	if id.Subject != "sub-1" || id.Email != "alice@corp.local" || !id.EmailVerified {
		t.Errorf("unexpected identity: %+v", id)
	}
	if len(id.Groups) != 2 || id.Groups[0] != "kopiv2-admins" {
		t.Errorf("groups = %v (empties must be dropped)", id.Groups)
	}
}

// SECURITY: an explicit email_verified=false is the IdP saying "don't trust this
// address" — refused unless the per-provider override is set.
func TestOidcIdentityFromClaims_UnverifiedEmail(t *testing.T) {
	claims := map[string]any{"email": "a@b.c", "email_verified": false}
	if _, err := OidcIdentityFromClaims("kc", "s", claims, "", false); err == nil {
		t.Fatal("unverified email accepted without override")
	}
	id, err := OidcIdentityFromClaims("kc", "s", claims, "", true)
	if err != nil || id.Email != "a@b.c" {
		t.Fatalf("override rejected: id=%+v err=%v", id, err)
	}
	// Absent claim (many IdPs omit it): accepted — only present-and-false refuses.
	if _, err := OidcIdentityFromClaims("kc", "s", map[string]any{"email": "a@b.c"}, "", false); err != nil {
		t.Fatalf("absent email_verified refused: %v", err)
	}
	if _, err := OidcIdentityFromClaims("kc", "", claims, "", true); err == nil {
		t.Fatal("empty subject accepted")
	}
}

// ADFS emits a bare string for single-group users; both shapes must work.
func TestOidcIdentityFromClaims_GroupsClaimShapes(t *testing.T) {
	id, err := OidcIdentityFromClaims("adfs", "s", map[string]any{"email": "a@b.c", "roles": "Domain Admins"}, "roles", true)
	if err != nil || len(id.Groups) != 1 || id.Groups[0] != "Domain Admins" {
		t.Fatalf("bare-string groups claim: id=%+v err=%v", id, err)
	}
	id, _ = OidcIdentityFromClaims("adfs", "s", map[string]any{"email": "a@b.c"}, "", true)
	if len(id.Groups) != 0 {
		t.Fatalf("no groupsClaim configured must yield no groups: %v", id.Groups)
	}
}

// Discovery against a minimal in-process issuer: NewOidcLogin must succeed and
// carry the discovered endpoints.
func TestNewOidcLogin_Discovery(t *testing.T) {
	var issuer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/auth",
			"token_endpoint":         issuer + "/token",
			"jwks_uri":               issuer + "/jwks",
		})
	}))
	defer srv.Close()
	issuer = srv.URL

	p, err := NewOidcLogin(context.Background(), OidcProviderConfigModel{
		Key:          "Corp-IdP",
		IssuerUrl:    issuer,
		ClientId:     "cid",
		ClientSecret: "csec",
		RedirectUrl:  "https://localhost:3001/api/callback/corp-idp",
	})
	if err != nil {
		t.Fatalf("NewOidcLogin: %v", err)
	}
	if p.Key() != "corp-idp" {
		t.Errorf("key not normalized: %q", p.Key())
	}
	if p.DisplayName() != "corp-idp" {
		t.Errorf("display name default: %q", p.DisplayName())
	}
	if !strings.HasPrefix(p.conf.Endpoint.AuthURL, issuer) {
		t.Errorf("discovered auth endpoint: %q", p.conf.Endpoint.AuthURL)
	}
}

func TestNewOidcLogin_Validation(t *testing.T) {
	base := OidcProviderConfigModel{Key: "kc", IssuerUrl: "https://idp.test", ClientId: "a", ClientSecret: "b", RedirectUrl: "https://x/cb"}
	for name, mutate := range map[string]func(*OidcProviderConfigModel){
		"no key":      func(c *OidcProviderConfigModel) { c.Key = " " },
		"no issuer":   func(c *OidcProviderConfigModel) { c.IssuerUrl = "" },
		"no secret":   func(c *OidcProviderConfigModel) { c.ClientSecret = "" },
		"no redirect": func(c *OidcProviderConfigModel) { c.RedirectUrl = "" },
	} {
		cfg := base
		mutate(&cfg)
		if _, err := NewOidcLogin(context.Background(), cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Reserved keys (fixed routes) and duplicates must never enter the registry.
func TestRegistry_ReservedAndDuplicateKeys(t *testing.T) {
	reg := NewRegistry()
	for _, key := range []string{"default", "providers", "list", "ldap", "kerberos", ""} {
		reg.Register(&stubProvider{key: key})
		if reg.Get(key) != nil {
			t.Errorf("reserved key %q registered", key)
		}
	}
	reg.Register(&stubProvider{key: "one"})
	reg.Register(&stubProvider{key: "ONE"}) // same key after normalization
	if len(reg.Keys()) != 1 {
		t.Fatalf("duplicate key registered: %v", reg.Keys())
	}
}

type stubProvider struct{ key string }

func (s *stubProvider) Key() string                                  { return s.key }
func (s *stubProvider) DisplayName() string                          { return s.key }
func (s *stubProvider) Login(w http.ResponseWriter, r *http.Request) {}
func (s *stubProvider) Callback(r *http.Request) (*Identity, error)  { return nil, nil }
