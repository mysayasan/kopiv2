package login

// Google User Info represents user profile
type GoogleUserInfoModel struct {
	Id            string `json:"id" form:"id" query:"id" validate:"required"`
	Email         string `json:"email" form:"email" query:"email" validate:"required"`
	VerifiedEmail bool   `json:"verified_email" form:"verified_email" query:"verified_email"`
	Name          string `json:"name" form:"name" query:"name" validate:"required"`
	GivenName     string `json:"given_name" form:"given_name" query:"given_name"`
	FamilyName    string `json:"family_name" form:"family_name" query:"family_name"`
	Picture       string `json:"picture" form:"picture" query:"picture"`
}

type OAuth2ConfigModel struct {
	ClientId     string   `json:"client_id" validate:"required"`
	ClientSecret string   `json:"client_secret" validate:"required"`
	RedirectUrl  string   `json:"redirect_url" validate:"required"`
	Scopes       []string `json:"scopes" validate:"required"`
}

type OAuthProvidersConfigModel struct {
	Google *OAuth2ConfigModel `json:"google"`
	GitHub *OAuth2ConfigModel `json:"github"`
	// Oidc lists generic OpenID Connect providers (Keycloak, Authentik, ADFS,
	// Entra, ...). Each configured entry registers as its own login option; a
	// half-configured entry is skipped with a warning, never fatal.
	Oidc []OidcProviderConfigModel `json:"oidc"`
}

// OidcProviderConfigModel configures one OpenID Connect relying-party client.
type OidcProviderConfigModel struct {
	// Key is the stable lowercase identifier: it names the routes
	// (/api/login/{key}), and accounts bind to "oidc:{key}" — CHANGING IT ORPHANS
	// existing federated accounts. Must not collide with built-in provider keys.
	Key string `json:"key"`
	// DisplayName is the login-page button label (defaults to Key).
	DisplayName string `json:"display_name"`
	// IssuerUrl is the IdP's issuer; discovery loads
	// {issuer}/.well-known/openid-configuration at startup.
	IssuerUrl    string `json:"issuer_url"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectUrl  string `json:"redirect_url"`
	// Scopes defaults to ["openid", "profile", "email"].
	Scopes []string `json:"scopes"`
	// CaCertPath optionally pins the IdP's CA for discovery/JWKS/token calls
	// (air-gapped intranet IdPs with a private CA).
	CaCertPath string `json:"ca_cert_path"`
	// GroupsClaim optionally names an id_token claim (e.g. "groups", "roles")
	// whose values feed the federated group→role mappings.
	GroupsClaim string `json:"groups_claim"`
	// InsecureSkipEmailVerified accepts accounts whose email_verified claim is
	// false/absent. Intranet IdPs (Keycloak without SMTP) often never verify
	// emails; only set this when the IdP is the org's own system of record.
	InsecureSkipEmailVerified bool `json:"insecure_skip_email_verified"`
}

type GitHubUserInfoModel struct {
	Id        int64  `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

type DefaultLoginRequestModel struct {
	Username string `json:"username" form:"username" query:"username" validate:"required"`
	Password string `json:"password" form:"password" query:"password" validate:"required"`
}

type DefaultRegisterRequestModel struct {
	Username  string `json:"username" form:"username" query:"username" validate:"required"`
	Password  string `json:"password" form:"password" query:"password" validate:"required"`
	FirstName string `json:"firstName" form:"firstName" query:"firstName"`
	LastName  string `json:"lastName" form:"lastName" query:"lastName"`
}
