package login

import (
	"context"
	"log"
	"net/http"
	"strings"
)

// Identity is the normalized result of any federated login, regardless of the
// protocol that produced it (OAuth2 today; LDAP/Kerberos/OIDC to follow).
type Identity struct {
	// Provider is the registry key of the provider that authenticated the user.
	Provider string
	// Subject is the provider's own stable unique id for the account (Google `id`,
	// GitHub numeric id, later AD objectGUID / OIDC `sub`). Account matching keys on
	// (Provider, Subject) — never on email alone, which the user can often change or
	// re-register at the provider.
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	GivenName     string
	FamilyName    string
	Picture       string
	// Groups carries provider-side group names/DNs for role mapping. Empty for the
	// social providers; populated by directory-backed providers.
	Groups []string
}

// RedirectProvider is a browser-redirect login method (OAuth2/OIDC): Login sends the
// browser to the provider, Callback consumes the provider's redirect and returns the
// normalized identity.
type RedirectProvider interface {
	// Key is the stable lowercase identifier used in /api/login/{key} and
	// /api/callback/{key} routes and stored as Identity.Provider.
	Key() string
	// DisplayName is the human label rendered on login-page buttons.
	DisplayName() string
	Login(w http.ResponseWriter, r *http.Request)
	Callback(r *http.Request) (*Identity, error)
}

// reservedProviderKeys are path segments under /api/login that are routes of their
// own; a provider claiming one would be shadowed (or shadow them) in the router.
// "ldap" and "kerberos" are the fixed non-redirect login routes.
var reservedProviderKeys = map[string]bool{
	"default":   true,
	"providers": true,
	"list":      true,
	"ldap":      true,
	"kerberos":  true,
}

// Registry holds the configured identity providers in registration order (which is
// also button render order on the login pages).
type Registry struct {
	order     []string
	providers map[string]RedirectProvider
}

func NewRegistry() *Registry {
	return &Registry{providers: map[string]RedirectProvider{}}
}

// Register adds a provider. Invalid, reserved or duplicate keys are ignored: a
// misconfigured provider must degrade to "not offered", never break login for the
// rest (same rationale as nilling half-configured OAuth blocks at boot).
func (m *Registry) Register(p RedirectProvider) {
	if m == nil || p == nil {
		return
	}
	key := strings.ToLower(strings.TrimSpace(p.Key()))
	if key == "" || reservedProviderKeys[key] || m.providers[key] != nil {
		return
	}
	m.providers[key] = p
	m.order = append(m.order, key)
}

// Get returns the provider registered under key, or nil.
func (m *Registry) Get(key string) RedirectProvider {
	if m == nil {
		return nil
	}
	return m.providers[strings.ToLower(strings.TrimSpace(key))]
}

// Keys returns the registered provider keys in registration order.
func (m *Registry) Keys() []string {
	if m == nil {
		return nil
	}
	return append([]string(nil), m.order...)
}

// BuildRegistry assembles the provider registry from the OAuth config block. A
// provider registers only when its credentials are actually present — the stock
// config ships empty google/github objects, and a blank client id would send the
// browser to the provider just to fail (a dead end on an air-gapped intranet).
//
// OIDC entries additionally need live discovery against their issuer; one that
// fails (typo'd issuer, IdP down at boot, bad CA) is logged and SKIPPED so the
// rest of the login page keeps working — it comes back on the next restart.
func BuildRegistry(conf *OAuthProvidersConfigModel) *Registry {
	reg := NewRegistry()
	if conf == nil {
		return reg
	}
	configured := func(p *OAuth2ConfigModel) bool {
		return p != nil && strings.TrimSpace(p.ClientId) != "" && strings.TrimSpace(p.ClientSecret) != ""
	}
	if configured(conf.Google) {
		reg.Register(NewGoogleLogin(*conf.Google))
	}
	if configured(conf.GitHub) {
		reg.Register(NewGithubLogin(*conf.GitHub))
	}
	for _, cfg := range conf.Oidc {
		if strings.TrimSpace(cfg.ClientId) == "" || strings.TrimSpace(cfg.ClientSecret) == "" {
			log.Printf("WARNING: oidc provider %q disabled — set client_id and a client secret (config or OIDC_%s_CLIENT_SECRET env)", cfg.Key, strings.ToUpper(strings.TrimSpace(cfg.Key)))
			continue
		}
		p, err := NewOidcLogin(context.Background(), cfg)
		if err != nil {
			log.Printf("WARNING: oidc provider %q disabled — %v", cfg.Key, err)
			continue
		}
		reg.Register(p)
	}
	return reg
}
