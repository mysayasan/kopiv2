package apis

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/models"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/login"
)

const (
	defaultAuthCodeTTLSeconds    = 300
	defaultAccessTokenTTLSeconds = 900
	defaultFederatedSessionTTL   = 259200
)

type federatedAuthApi struct {
	cfg       *config.AppConfigModel
	auth      *middlewares.AuthMidware
	providers *login.Registry
	directory services.IDirectoryService
	// kerbLabel non-empty renders the Kerberos SSO button under that name.
	kerbLabel        string
	guard            *sharedapis.LoginGuard
	userService      services.IUserLoginService
	apps             dbsql.IGenericRepo[entities.AppRegistry]
	authConfigs      dbsql.IGenericRepo[entities.AppAuthConfig]
	redirectURIs     dbsql.IGenericRepo[entities.AppRedirectUri]
	store            cache.Store
	authCodeTTL      time.Duration
	accessTokenTTL   time.Duration
	defaultSessionTL time.Duration
}

type authCodeCacheEntry struct {
	Code        string    `json:"code"`
	ClientID    string    `json:"clientId"`
	AppCode     string    `json:"appCode"`
	Audience    string    `json:"audience"`
	RedirectURI string    `json:"redirectUri"`
	UserID      int64     `json:"userId"`
	RoleID      int64     `json:"roleId"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	GivenName   string    `json:"givenName"`
	FamilyName  string    `json:"familyName"`
	Picture     string    `json:"picture"`
	SessionID   string    `json:"sessionId"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type tokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	RedirectURI  string `json:"redirect_uri"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type tokenResponse struct {
	AccessToken   string   `json:"accessToken"`
	TokenType     string   `json:"tokenType"`
	ExpiresIn     int64    `json:"expiresIn"`
	ExpiresAt     int64    `json:"expiresAt"`
	UserID        int64    `json:"userId"`
	RoleID        int64    `json:"roleId"`
	Email         string   `json:"email"`
	Name          string   `json:"name"`
	SessionID     string   `json:"sessionId"`
	Issuer        string   `json:"issuer"`
	Audience      []string `json:"audience"`
	AppCode       string   `json:"appCode"`
	PolicyVersion int64    `json:"policyVersion"`
}

func NewFederatedAuthApi(
	router *mux.Router,
	cfg *config.AppConfigModel,
	auth *middlewares.AuthMidware,
	providers *login.Registry,
	directory services.IDirectoryService,
	kerberosLabel string,
	guard *sharedapis.LoginGuard,
	userService services.IUserLoginService,
	apps dbsql.IGenericRepo[entities.AppRegistry],
	authConfigs dbsql.IGenericRepo[entities.AppAuthConfig],
	redirectURIs dbsql.IGenericRepo[entities.AppRedirectUri],
	store cache.Store,
) {
	handler := &federatedAuthApi{
		cfg:              cfg,
		auth:             auth,
		providers:        providers,
		directory:        directory,
		kerbLabel:        strings.TrimSpace(kerberosLabel),
		guard:            guard,
		userService:      userService,
		apps:             apps,
		authConfigs:      authConfigs,
		redirectURIs:     redirectURIs,
		store:            store,
		authCodeTTL:      secondsDuration(configInt(cfg, "authCode"), defaultAuthCodeTTLSeconds),
		accessTokenTTL:   secondsDuration(configInt(cfg, "accessToken"), defaultAccessTokenTTLSeconds),
		defaultSessionTL: secondsDuration(configInt(cfg, "session"), defaultFederatedSessionTTL),
	}

	group := router.PathPrefix("/auth").Subrouter()
	group.HandleFunc("/authorize", handler.authorize).Methods("GET")
	group.HandleFunc("/login", handler.loginPage).Methods("GET")
	group.HandleFunc("/login", handler.loginPost).Methods("POST")
	group.HandleFunc("/token", handler.token).Methods("POST")
}

func (m *federatedAuthApi) authorize(w http.ResponseWriter, r *http.Request) {
	req, err := m.parseAuthorizeRequest(r)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}

	client, app, err := m.loadClient(r.Context(), req.clientID)
	if err != nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		return
	}
	if err := m.validateRedirectURI(r.Context(), client.Id, req.redirectURI); err != nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		return
	}
	if req.audience != "" && !strings.EqualFold(req.audience, app.Audience) {
		controllers.SendError(w, controllers.ErrLimitedAccess, "audience not registered for client")
		return
	}

	claims, err := m.auth.ClaimsFromRequest(r)
	if err != nil {
		m.redirectToLogin(w, r)
		return
	}

	code, err := newFederatedOpaqueToken()
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	ttl := durationOverride(client.AuthCodeTTLSeconds, m.authCodeTTL)
	entry := authCodeCacheEntry{
		Code:        code,
		ClientID:    client.ClientId,
		AppCode:     app.Code,
		Audience:    app.Audience,
		RedirectURI: req.redirectURI,
		UserID:      claims.Id,
		Email:       claims.Email,
		Name:        claims.Name,
		GivenName:   claims.GivenName,
		FamilyName:  claims.FamilyName,
		Picture:     claims.Picture,
		SessionID:   claims.SessionId,
		ExpiresAt:   time.Now().UTC().Add(ttl),
	}
	if err := m.store.Set(r.Context(), authCodeCacheKey(code), entry, ttl); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	redirectURL, err := url.Parse(req.redirectURI)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	q := redirectURL.Query()
	q.Set("code", code)
	if req.state != "" {
		q.Set("state", req.state)
	}
	redirectURL.RawQuery = q.Encode()
	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

func (m *federatedAuthApi) loginPage(w http.ResponseWriter, r *http.Request) {
	errMsg := ""
	// A failed Kerberos SSO attempt bounces back here rather than dead-ending on a
	// 401 body; the reason stays in the server log (never leaked to the browser).
	if r.URL.Query().Get("error") == "sso_failed" {
		errMsg = "Single sign-on failed. Sign in another way, or contact your administrator."
	}
	m.renderLoginPage(w, http.StatusOK, cleanContinuePath(r.URL.Query().Get("continue")), "", "local", m.directoryLabel(r), errMsg)
}

// directoryLabel returns the directory login option's display label, or "" when
// directory login is not enabled (the page then renders no account-type choice).
func (m *federatedAuthApi) directoryLabel(r *http.Request) string {
	if m.directory == nil {
		return ""
	}
	enabled, label := m.directory.LoginOption(r.Context())
	if !enabled {
		return ""
	}
	return label
}

// renderLoginPage draws the federated sign-in page. It is the suite's standard login
// card — the same centered panel, brand mark, fields and buttons as mymatasan's and
// myseliasan's React login screens — but rendered server-side, because this page sits
// in the middle of the OAuth redirect chain and has to exist before any SPA loads.
// The colours are the light-theme literals of the apps' --* tokens; there is no
// stylesheet to import them from here. Assets (favicon, self-hosted Quicksand) come
// from static/assets, copied in by the webpack build.
//
// `errMsg` is empty on the initial GET and set when a POST failed, so a bad password
// re-renders the branded card instead of dumping raw text; `username` is echoed back
// so the user only has to retype the password. `method` ("local"/"ldap") is the
// account-type selection to preserve across a failed POST; `ldapLabel` non-empty
// renders the account-type choice (directory login is enabled) under that name.
func (m *federatedAuthApi) renderLoginPage(w http.ResponseWriter, status int, continueTo, username, method, ldapLabel, errMsg string) {
	social := m.socialButtonsHTML(continueTo)
	errHTML := ""
	if errMsg != "" {
		errHTML = fmt.Sprintf(`<p class="status-line" role="alert">%s</p>`, html.EscapeString(errMsg))
	}
	methodHTML := ""
	if ldapLabel != "" {
		localSel, ldapSel := ` selected`, ``
		if method == "ldap" {
			localSel, ldapSel = ``, ` selected`
		}
		methodHTML = fmt.Sprintf(`<label>Account type<select name="method">`+
			`<option value="local"%s>Local account</option>`+
			`<option value="ldap"%s>%s</option>`+
			`</select></label>`, localSel, ldapSel, html.EscapeString(ldapLabel))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="icon" type="image/svg+xml" href="/assets/favicon.svg">
  <link rel="alternate icon" href="/favicon.ico">
  <meta name="theme-color" content="#0f1a14">
  <link rel="stylesheet" href="/assets/fonts.css">
  <title>MyIDSan Auth</title>
  <style>
    * { box-sizing: border-box; }
    body { margin: 0; font-family: Inter, "Segoe UI", Arial, sans-serif; background: #f4f6f8; color: #18212f; }
    .login-screen { min-height: 100vh; display: flex; padding: 24px; }
    .login-panel { margin: auto; width: min(100%%, 380px); display: grid; gap: 16px; padding: 24px;
      border: 1px solid #d6dee7; border-radius: 8px; background: #fff; box-shadow: 0 12px 30px rgba(32, 42, 54, .08); }
    .login-brand { display: grid; justify-items: center; text-align: center; }
    .brand-logo { display: flex; flex-direction: column; align-items: center; gap: 6px; line-height: 1; user-select: none; }
    .brand-mark { display: block; color: #6f4d9d; }
    .brand-wordmark { font-family: Quicksand, Fredoka, system-ui, sans-serif; font-weight: 600; font-size: 34px; letter-spacing: .5px; color: #6f4d9d; }
    .login-brand p { margin: 6px 0 0; color: #6a7888; }
    form { display: grid; gap: 16px; }
    label { display: grid; gap: 6px; color: #59687a; font-size: 13px; font-weight: 650; }
    input, select { width: 100%%; min-height: 38px; padding: 8px 10px; font: inherit;
      border: 1px solid #c7d1dc; border-radius: 6px; background: #fff; color: #18212f; }
    button { width: 100%%; min-height: 38px; padding: 0 14px; font: inherit; font-weight: 650; cursor: pointer;
      border: 1px solid #2d6cdf; border-radius: 6px; background: #2d6cdf; color: #fff; }
    .status-line { margin: 0; padding: 10px 12px; font-size: 13px;
      border-left: 4px solid #d28d1f; background: #fff8e9; color: #64450d; }
    .login-divider { display: flex; align-items: center; gap: 10px; color: #6a7888; font-size: 12px; }
    .login-divider::before, .login-divider::after { content: ""; flex: 1; height: 1px; background: #c7d1dc; }
    .oauth-row { display: flex; gap: 10px; }
    .oauth-btn { flex: 1; text-align: center; text-decoration: none; border: 1px solid #c7d1dc; border-radius: 6px;
      padding: 10px; color: #233044; background: #fff; font-weight: 650; font-size: 14px; }
    .oauth-btn:hover { background: #f7f9fb; }
  </style>
</head>
<body>
  <main class="login-screen">
    <section class="login-panel">
      <div class="login-brand">
        <div class="brand-logo" aria-label="myidsan">
          <svg class="brand-mark" viewBox="0 0 64 64" width="104" height="104" role="img" aria-hidden="true">
            <g fill="none" stroke="currentColor" stroke-width="2.8" stroke-linecap="round" stroke-linejoin="round">
              <path d="M19 13.5 H45 Q50 13.5 50 18 V33 Q50 45 32 54 Q14 45 14 33 V18 Q14 13.5 19 13.5 Z"/>
              <path d="M20 30 Q31 21 42 30 Q31 39 20 30 Z"/>
              <circle cx="31" cy="30" r="6"/>
              <path d="M24 41 L31 48 L46 30" stroke-width="3.4"/>
            </g>
            <circle cx="31" cy="30" r="3" fill="currentColor"/>
            <circle cx="29.5" cy="28.6" r="1" fill="#f0eaf6"/>
          </svg>
          <span class="brand-wordmark">myidsan</span>
        </div>
        <p>Sign in to continue</p>
      </div>
      %s
      <form method="post" action="/api/auth/login">
        <input type="hidden" name="continue" value="%s">
        <label>Username or email<input name="username" value="%s" autocomplete="username" autofocus required></label>
        <label>Password<input name="password" type="password" autocomplete="current-password" required></label>%s
        <button type="submit">Log in</button>
      </form>%s
    </section>
  </main>
</body>
</html>`, errHTML, html.EscapeString(continueTo), html.EscapeString(username), methodHTML, social)
}

// socialButtonsHTML renders a sign-in link for every registered identity provider.
// Each link carries the pending `continue` target (the /api/auth/authorize URL)
// through the login round-trip so the user lands back at the authorization step
// afterwards.
//
// The registry only holds providers whose credentials are actually present (see
// login.BuildRegistry), so no button can send the browser off with an empty
// client_id — an OAuth error on the internet, and a dead end on an air-gapped
// intranet, which is where myseliasan runs.
func (m *federatedAuthApi) socialButtonsHTML(continueTo string) string {
	keys := m.providers.Keys()
	if len(keys) == 0 && m.kerbLabel == "" {
		return ""
	}
	cont := url.QueryEscape(continueTo)
	var b strings.Builder
	b.WriteString(`<div class="login-divider"><span>or continue with</span></div><div class="oauth-row">`)
	for _, key := range keys {
		p := m.providers.Get(key)
		if p == nil {
			continue
		}
		fmt.Fprintf(&b, `<a class="oauth-btn" href="/api/login/%s?continue=%s">%s</a>`,
			url.PathEscape(key), cont, html.EscapeString(p.DisplayName()))
	}
	if m.kerbLabel != "" {
		// Plain navigation: the 401/Negotiate dance happens transparently in the
		// browser; a failure bounces back to this page with error=sso_failed.
		fmt.Fprintf(&b, `<a class="oauth-btn" href="/api/login/kerberos?continue=%s">%s</a>`,
			cont, html.EscapeString(m.kerbLabel))
	}
	b.WriteString(`</div>`)
	return b.String()
}

func (m *federatedAuthApi) loginPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	if err := r.ParseForm(); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	continueTo := cleanContinuePath(r.Form.Get("continue"))
	username := r.Form.Get("username")
	method := r.Form.Get("method")
	ldapLabel := m.directoryLabel(r)
	// The account-type choice only exists while directory login is enabled — an
	// "ldap" POST against a disabled directory falls back to a local check.
	useLdap := method == "ldap" && ldapLabel != ""
	if !useLdap {
		method = "local"
	}

	if locked, retry := guardLocked(m.guard, r); locked {
		m.renderLoginPage(w, http.StatusTooManyRequests, continueTo, username, method, ldapLabel,
			fmt.Sprintf("Too many failed attempts — try again in %d seconds.", int(retry.Seconds())+1))
		return
	}

	var user *entities.UserLogin
	var err error
	if useLdap {
		user, err = m.directory.AuthenticateLdap(r.Context(), username, r.Form.Get("password"))
	} else {
		user, err = m.userService.AuthenticateDefault(r.Context(), username, r.Form.Get("password"))
	}
	if err != nil {
		if errors.Is(err, services.ErrInvalidCredential) || errors.Is(err, login.ErrLdapInvalidCredential) {
			if m.guard != nil {
				time.Sleep(m.guard.FailedDelay())
				m.guard.RecordFailure(loginGuardKey(r))
			}
		}
		// Re-render the branded card with the failure inline, rather than the bare
		// "Login failed" text this used to dump.
		m.renderLoginPage(w, http.StatusUnauthorized, continueTo, username, method, ldapLabel, "Login failed: "+err.Error())
		return
	}
	if m.guard != nil {
		m.guard.RecordSuccess(loginGuardKey(r))
	}
	if err := m.issueProviderSession(w, r, user); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, continueTo, http.StatusFound)
}

func (m *federatedAuthApi) token(w http.ResponseWriter, r *http.Request) {
	body, err := decodeTokenRequest(w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if body.GrantType != "" && body.GrantType != "authorization_code" {
		controllers.SendError(w, controllers.ErrBadRequest, "unsupported grant_type")
		return
	}

	client, app, err := m.loadClient(r.Context(), body.ClientID)
	if err != nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		return
	}
	if !secretMatches(client.ClientSecretHash, body.ClientSecret) {
		controllers.SendError(w, controllers.ErrLimitedAccess, "client secret not valid")
		return
	}
	if err := m.validateRedirectURI(r.Context(), client.Id, body.RedirectURI); err != nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		return
	}

	var entry authCodeCacheEntry
	found, err := m.store.Get(r.Context(), authCodeCacheKey(body.Code), &entry)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if !found || entry.Code == "" || time.Now().UTC().After(entry.ExpiresAt) {
		controllers.SendError(w, controllers.ErrLimitedAccess, "authorization code not valid")
		return
	}
	_ = m.store.Delete(r.Context(), authCodeCacheKey(body.Code))

	if entry.ClientID != client.ClientId || entry.RedirectURI != body.RedirectURI {
		controllers.SendError(w, controllers.ErrLimitedAccess, "authorization code does not match client")
		return
	}

	ttl := durationOverride(client.AccessTokenTTLSeconds, m.accessTokenTTL)
	sessionTTL := durationOverride(client.SessionTTLSeconds, m.defaultSessionTL)
	if sessionTTL > ttl {
		ttl = sessionTTL
	}
	expiresAt := time.Now().UTC().Add(ttl)
	// myidsan is an identity provider only: the federated token carries identity
	// (subject, email, name) but NO authorization role. Each relying app (e.g.
	// myseliasan) owns its own RBAC and resolves a local role from this identity.
	claims := models.JwtCustomClaims{
		Id:            entry.UserID,
		Email:         entry.Email,
		VerifiedEmail: true,
		Name:          entry.Name,
		GivenName:     entry.GivenName,
		FamilyName:    entry.FamilyName,
		Picture:       entry.Picture,
		SessionId:     entry.SessionID,
		AppCode:       app.Code,
		PolicyVersion: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer(),
			Audience:  jwt.ClaimStrings{app.Audience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ID:        entry.SessionID,
		},
	}
	token, err := m.auth.JwtToken(claims)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, tokenResponse{
		AccessToken:   token,
		TokenType:     "Bearer",
		ExpiresIn:     int64(time.Until(expiresAt).Seconds()),
		ExpiresAt:     expiresAt.Unix(),
		UserID:        claims.Id,
		Email:         claims.Email,
		Name:          claims.Name,
		SessionID:     claims.SessionId,
		Issuer:        claims.Issuer,
		Audience:      []string(claims.Audience),
		AppCode:       claims.AppCode,
		PolicyVersion: claims.PolicyVersion,
	})
}

type federatedAuthorizeRequest struct {
	clientID    string
	redirectURI string
	audience    string
	state       string
}

func (m *federatedAuthApi) parseAuthorizeRequest(r *http.Request) (federatedAuthorizeRequest, error) {
	q := r.URL.Query()
	req := federatedAuthorizeRequest{
		clientID:    strings.TrimSpace(q.Get("client_id")),
		redirectURI: strings.TrimSpace(q.Get("redirect_uri")),
		audience:    strings.TrimSpace(q.Get("audience")),
		state:       strings.TrimSpace(q.Get("state")),
	}
	if req.clientID == "" {
		return req, errors.New("client_id is required")
	}
	if req.redirectURI == "" {
		return req, errors.New("redirect_uri is required")
	}
	if responseType := strings.TrimSpace(q.Get("response_type")); responseType != "" && responseType != "code" {
		return req, errors.New("unsupported response_type")
	}
	return req, nil
}

func (m *federatedAuthApi) loadClient(ctx context.Context, clientID string) (*entities.AppAuthConfig, *entities.AppRegistry, error) {
	rows, _, err := m.authConfigs.Get(ctx, "", 1, 0, []sqldataenums.Filter{
		{FieldName: "ClientId", Compare: sqldataenums.Equal, Value: clientID},
	}, nil)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 || rows[0] == nil || !rows[0].IsActive {
		return nil, nil, fmt.Errorf("client is not registered")
	}
	app, err := m.apps.GetById(ctx, "", uint64(rows[0].AppRegistryId))
	if err != nil {
		return nil, nil, err
	}
	if app == nil || !app.IsActive {
		return nil, nil, fmt.Errorf("app is not active")
	}
	return rows[0], app, nil
}

func (m *federatedAuthApi) validateRedirectURI(ctx context.Context, authConfigID int64, redirectURI string) error {
	rows, _, err := m.redirectURIs.Get(ctx, "", 1, 0, []sqldataenums.Filter{
		{FieldName: "AppAuthConfigId", Compare: sqldataenums.Equal, Value: authConfigID},
		{FieldName: "RedirectUri", Compare: sqldataenums.Equal, Value: redirectURI},
	}, nil)
	if err != nil {
		return err
	}
	if len(rows) == 0 || rows[0] == nil || !rows[0].IsActive {
		return fmt.Errorf("redirect_uri is not registered")
	}
	return nil
}

func (m *federatedAuthApi) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	continueTo := r.URL.RequestURI()
	http.Redirect(w, r, "/api/auth/login?continue="+url.QueryEscape(continueTo), http.StatusFound)
}

func (m *federatedAuthApi) issueProviderSession(w http.ResponseWriter, r *http.Request, user *entities.UserLogin) error {
	name := strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
	if name == "" {
		name = user.Email
	}
	return m.auth.IssueAuthCookies(w, r, models.JwtCustomClaims{
		Id:            user.Id,
		Name:          name,
		GivenName:     user.FirstName,
		FamilyName:    user.LastName,
		Email:         user.Email,
		VerifiedEmail: true,
		Picture:       user.PicUrl,
		RoleId:        user.UserRoleId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.defaultSessionTL)),
		},
	})
}

func decodeTokenRequest(w http.ResponseWriter, r *http.Request) (tokenRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return tokenRequest{}, err
		}
		return tokenRequest{
			GrantType:    r.Form.Get("grant_type"),
			Code:         r.Form.Get("code"),
			RedirectURI:  r.Form.Get("redirect_uri"),
			ClientID:     r.Form.Get("client_id"),
			ClientSecret: r.Form.Get("client_secret"),
		}, nil
	}
	var body tokenRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return body, dec.Decode(&body)
}

func cleanContinuePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		return "/"
	}
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "/"
	}
	return value
}

func secretMatches(storedHash string, secret string) bool {
	storedHash = strings.TrimSpace(strings.ToLower(storedHash))
	if storedHash == "" || strings.TrimSpace(secret) == "" {
		return false
	}
	actual := hashClientSecret(secret)
	return subtle.ConstantTimeCompare([]byte(storedHash), []byte(actual)) == 1
}

func hashClientSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func authCodeCacheKey(code string) string {
	return "sso:auth-code:" + strings.TrimSpace(code)
}

func durationOverride(seconds int64, fallback time.Duration) time.Duration {
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func secondsDuration(seconds int, fallback int) time.Duration {
	if seconds <= 0 {
		seconds = fallback
	}
	return time.Duration(seconds) * time.Second
}

func configInt(cfg *config.AppConfigModel, key string) int {
	if cfg == nil {
		return 0
	}
	switch key {
	case "authCode":
		return cfg.SSO.AuthCodeTTLSeconds
	case "accessToken":
		return cfg.SSO.AccessTokenTTLSeconds
	case "session":
		return cfg.SSO.SessionTTLSeconds
	default:
		return 0
	}
}

func (m *federatedAuthApi) issuer() string {
	if m.cfg != nil && strings.TrimSpace(m.cfg.SSO.Issuer) != "" {
		return strings.TrimSpace(m.cfg.SSO.Issuer)
	}
	return "myidsan"
}

func newFederatedOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
