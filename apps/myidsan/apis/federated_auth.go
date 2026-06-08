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
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

const (
	defaultAuthCodeTTLSeconds    = 300
	defaultAccessTokenTTLSeconds = 900
	defaultFederatedSessionTTL   = 259200
)

type federatedAuthApi struct {
	cfg              *config.AppConfigModel
	auth             *middlewares.AuthMidware
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
	userService services.IUserLoginService,
	apps dbsql.IGenericRepo[entities.AppRegistry],
	authConfigs dbsql.IGenericRepo[entities.AppAuthConfig],
	redirectURIs dbsql.IGenericRepo[entities.AppRedirectUri],
	store cache.Store,
) {
	handler := &federatedAuthApi{
		cfg:              cfg,
		auth:             auth,
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
		RoleID:      claims.RoleId,
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
	continueTo := cleanContinuePath(r.URL.Query().Get("continue"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>MyIDSan Auth</title>
  <style>
    body { margin: 0; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #f6f8fb; color: #1f2937; }
    .page { min-height: 100vh; display: grid; place-items: center; padding: 24px; }
    .panel { width: min(420px, 100%%); background: #fff; border: 1px solid #d8dee8; border-radius: 8px; box-shadow: 0 16px 50px rgba(31, 41, 55, .10); padding: 28px; }
    .brand { display: flex; align-items: center; gap: 12px; margin-bottom: 24px; }
    .mark { width: 42px; height: 42px; display: grid; place-items: center; border-radius: 6px; background: #102a43; color: #fff; font-weight: 700; }
    h1 { font-size: 22px; margin: 0; }
    p { margin: 6px 0 0; color: #64748b; }
    label { display: grid; gap: 7px; font-size: 13px; font-weight: 650; margin-top: 16px; }
    input { border: 1px solid #cbd5e1; border-radius: 6px; padding: 11px 12px; font: inherit; }
    button { width: 100%%; margin-top: 22px; border: 0; border-radius: 6px; padding: 12px; background: #1d4ed8; color: white; font-weight: 700; cursor: pointer; }
    .error { margin-top: 14px; color: #b91c1c; font-size: 13px; }
  </style>
</head>
<body>
  <main class="page">
    <section class="panel">
      <div class="brand"><div class="mark">ID</div><div><h1>MyIDSan</h1><p>Sign in to continue</p></div></div>
      <form method="post" action="/api/auth/login">
        <input type="hidden" name="continue" value="%s">
        <label>Username or email<input name="username" autocomplete="username" required></label>
        <label>Password<input name="password" type="password" autocomplete="current-password" required></label>
        <button type="submit">Log in</button>
      </form>
    </section>
  </main>
</body>
</html>`, html.EscapeString(continueTo))
}

func (m *federatedAuthApi) loginPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	if err := r.ParseForm(); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	continueTo := cleanContinuePath(r.Form.Get("continue"))
	user, err := m.userService.AuthenticateDefault(r.Context(), r.Form.Get("username"), r.Form.Get("password"))
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `<p>Login failed: %s</p><p><a href="/api/auth/login?continue=%s">Try again</a></p>`, html.EscapeString(err.Error()), url.QueryEscape(continueTo))
		return
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
	claims := models.JwtCustomClaims{
		Id:            entry.UserID,
		Email:         entry.Email,
		VerifiedEmail: true,
		Name:          entry.Name,
		GivenName:     entry.GivenName,
		FamilyName:    entry.FamilyName,
		Picture:       entry.Picture,
		RoleId:        entry.RoleID,
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
		RoleID:        claims.RoleId,
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
