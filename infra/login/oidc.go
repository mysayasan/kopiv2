package login

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OidcProviderKeyPrefix namespaces Identity.Provider for generic OIDC logins:
// accounts bind to "oidc:<key>", so a config key can never collide with a built-in
// provider's identity space (or another future provider named the same).
const OidcProviderKeyPrefix = "oidc:"

const oidcDiscoveryTimeout = 10 * time.Second

// OidcLogin is a generic OpenID Connect relying party implementing
// RedirectProvider — one instance per configured IdP (Keycloak, Authentik, ADFS,
// Entra, ...). The authorization-code flow runs with PKCE (S256) and a nonce; the
// id_token is verified against the IdP's JWKS (discovered at startup).
type OidcLogin struct {
	key               string
	displayName       string
	conf              oauth2.Config
	verifier          *oidc.IDTokenVerifier
	httpClient        *http.Client // nil = default; set when a CA is pinned
	groupsClaim       string
	skipEmailVerified bool
}

// NewOidcLogin runs OIDC discovery against the issuer and prepares the relying
// party. Errors are configuration/reachability problems — callers should log and
// skip the provider (fail soft), not abort boot; the IdP being down at boot then
// costs one restart, never the whole login page.
func NewOidcLogin(ctx context.Context, cfg OidcProviderConfigModel) (*OidcLogin, error) {
	key := strings.ToLower(strings.TrimSpace(cfg.Key))
	if key == "" {
		return nil, errors.New("oidc provider key is required")
	}
	issuer := strings.TrimRight(strings.TrimSpace(cfg.IssuerUrl), "/")
	if issuer == "" {
		return nil, fmt.Errorf("oidc provider %q: issuer_url is required", key)
	}
	if strings.TrimSpace(cfg.ClientId) == "" || strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, fmt.Errorf("oidc provider %q: client_id and client_secret are required", key)
	}
	if strings.TrimSpace(cfg.RedirectUrl) == "" {
		return nil, fmt.Errorf("oidc provider %q: redirect_url is required", key)
	}

	httpClient, err := oidcHTTPClient(cfg.CaCertPath)
	if err != nil {
		return nil, fmt.Errorf("oidc provider %q: %w", key, err)
	}

	discoverCtx, cancel := context.WithTimeout(ctx, oidcDiscoveryTimeout)
	defer cancel()
	if httpClient != nil {
		discoverCtx = oidc.ClientContext(discoverCtx, httpClient)
	}
	provider, err := oidc.NewProvider(discoverCtx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider %q: discovery failed for %s: %w", key, issuer, err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	displayName := strings.TrimSpace(cfg.DisplayName)
	if displayName == "" {
		displayName = key
	}

	return &OidcLogin{
		key:         key,
		displayName: displayName,
		conf: oauth2.Config{
			ClientID:     cfg.ClientId,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  strings.TrimSpace(cfg.RedirectUrl),
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
		verifier:          provider.Verifier(&oidc.Config{ClientID: cfg.ClientId}),
		httpClient:        httpClient,
		groupsClaim:       strings.TrimSpace(cfg.GroupsClaim),
		skipEmailVerified: cfg.InsecureSkipEmailVerified,
	}, nil
}

func (m *OidcLogin) Key() string {
	return m.key
}

func (m *OidcLogin) DisplayName() string {
	return m.displayName
}

func (m *OidcLogin) Login(w http.ResponseWriter, r *http.Request) {
	stateCookie, state, err := NewOAuthState(m.key, r.TLS != nil)
	if err != nil {
		http.Error(w, "oauth state generation failed", http.StatusInternalServerError)
		return
	}

	// PKCE verifier and nonce ride single-use cookies scoped to the callback,
	// exactly like the state cookie: the browser that started the flow is the
	// only one that can finish it.
	pkceVerifier := oauth2.GenerateVerifier()
	nonce, err := randomURLToken()
	if err != nil {
		http.Error(w, "oauth nonce generation failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, stateCookie)
	http.SetCookie(w, oidcFlowCookie(m.key, "pkce", pkceVerifier, r.TLS != nil))
	http.SetCookie(w, oidcFlowCookie(m.key, "nonce", nonce, r.TLS != nil))

	authURL := m.conf.AuthCodeURL(state, oauth2.S256ChallengeOption(pkceVerifier), oidc.Nonce(nonce))
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func (m *OidcLogin) Callback(r *http.Request) (*Identity, error) {
	if err := ValidateOAuthState(r, m.key, r.URL.Query().Get("state")); err != nil {
		return nil, err
	}
	pkceVerifier := readOidcFlowCookie(r, m.key, "pkce")
	nonce := readOidcFlowCookie(r, m.key, "nonce")
	if pkceVerifier == "" || nonce == "" {
		return nil, errors.New("oidc login flow cookies are missing — restart the sign-in")
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		return nil, errors.New("oauth code is required")
	}

	ctx := r.Context()
	if m.httpClient != nil {
		ctx = oidc.ClientContext(ctx, m.httpClient)
	}
	token, err := m.conf.Exchange(ctx, code, oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		return nil, fmt.Errorf("code-token exchange failed: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, errors.New("token response carried no id_token")
	}
	idToken, err := m.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("id_token verification failed: %w", err)
	}
	if idToken.Nonce != nonce {
		return nil, errors.New("id_token nonce does not match this login flow")
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("id_token claims parsing failed: %w", err)
	}

	return OidcIdentityFromClaims(m.key, idToken.Subject, claims, m.groupsClaim, m.skipEmailVerified)
}

// OidcIdentityFromClaims maps standard OIDC claims onto the normalized Identity.
// Exported for tests; pure.
func OidcIdentityFromClaims(key, subject string, claims map[string]any, groupsClaim string, skipEmailVerified bool) (*Identity, error) {
	if strings.TrimSpace(subject) == "" {
		return nil, errors.New("id_token carried no subject")
	}
	email := claimString(claims, "email")
	verified, hasVerified := claims["email_verified"].(bool)
	if email != "" && !skipEmailVerified && hasVerified && !verified {
		// A present-but-false claim is an explicit "don't trust this address".
		//
		// The address is named because this error becomes the audit record's Detail, and a
		// refusal that does not say WHICH account was refused leaves an operator with
		// nothing to act on. Disclosing it costs nothing: reaching this line requires
		// having just authenticated at the IdP as that very account.
		return nil, fmt.Errorf("identity provider reports the account email as unverified: %s", email)
	}

	name := claimString(claims, "name")
	if name == "" {
		name = claimString(claims, "preferred_username")
	}

	return &Identity{
		Provider:      OidcProviderKeyPrefix + key,
		Subject:       subject,
		Email:         email,
		EmailVerified: verified || skipEmailVerified,
		Name:          name,
		GivenName:     claimString(claims, "given_name"),
		FamilyName:    claimString(claims, "family_name"),
		Picture:       claimString(claims, "picture"),
		Groups:        claimStrings(claims, groupsClaim),
	}, nil
}

func claimString(claims map[string]any, key string) string {
	if key == "" {
		return ""
	}
	s, _ := claims[key].(string)
	return strings.TrimSpace(s)
}

// claimStrings reads a claim that may be a JSON array of strings or a single
// string (ADFS emits a bare string for single-group users).
func claimStrings(claims map[string]any, key string) []string {
	if key == "" {
		return nil
	}
	switch v := claims[key].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return []string{s}
		}
	}
	return nil
}

func oidcHTTPClient(caCertPath string) (*http.Client, error) {
	path := strings.TrimSpace(caCertPath)
	if path == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ca cert %s: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("ca cert %s could not be parsed", path)
	}
	return &http.Client{
		Timeout: oidcDiscoveryTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}, nil
}

func oidcFlowCookie(provider, kind, value string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     "oauth_" + kind + "_" + strings.ToLower(provider),
		Value:    value,
		Path:     "/api/callback/" + strings.ToLower(provider),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((5 * time.Minute).Seconds()),
	}
}

func readOidcFlowCookie(r *http.Request, provider, kind string) string {
	cookie, err := r.Cookie("oauth_" + kind + "_" + strings.ToLower(provider))
	if err != nil {
		return ""
	}
	return cookie.Value
}

func randomURLToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
