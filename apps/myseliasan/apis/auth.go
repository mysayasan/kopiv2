package apis

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
)

const ssoStateTTL = 10 * time.Minute

type authApi struct {
	cfg   *config.AppConfigModel
	auth  *middlewares.AuthMidware
	store cache.Store
}

type stateEntry struct {
	State       string `json:"state"`
	ReturnTo    string `json:"returnTo"`
	RedirectURI string `json:"redirectUri"`
	CreatedAt   int64  `json:"createdAt"`
}

type tokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	RedirectURI  string `json:"redirect_uri"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type providerTokenResult struct {
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

type providerTokenResponse struct {
	Message    string              `json:"message"`
	DurationMs int64               `json:"durationMs"`
	Result     providerTokenResult `json:"result"`
}

func NewAuthApi(router *mux.Router, cfg *config.AppConfigModel, auth *middlewares.AuthMidware, store cache.Store) {
	handler := &authApi{cfg: cfg, auth: auth, store: store}
	group := router.PathPrefix("/auth").Subrouter()
	group.HandleFunc("/start", handler.start).Methods("GET")
	group.HandleFunc("/callback", handler.callback).Methods("GET")
	group.HandleFunc("/logout", handler.logout).Methods("POST")
}

func (m *authApi) start(w http.ResponseWriter, r *http.Request) {
	if _, err := m.auth.ClaimsFromRequest(r); err == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	providerBase := strings.TrimRight(strings.TrimSpace(m.cfg.SSO.ProviderBaseURL), "/")
	if providerBase == "" {
		controllers.SendError(w, controllers.ErrInternalServerError, "sso providerBaseUrl is required")
		return
	}
	clientID := strings.TrimSpace(m.cfg.SSO.ClientID)
	if clientID == "" {
		controllers.SendError(w, controllers.ErrInternalServerError, "sso clientId is required")
		return
	}

	state, err := newOpaqueToken()
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	redirectURI := callbackURL(r, m.cfg)
	returnTo := cleanReturnTo(r.URL.Query().Get("returnTo"))
	if err := m.store.Set(r.Context(), stateCacheKey(state), stateEntry{
		State:       state,
		ReturnTo:    returnTo,
		RedirectURI: redirectURI,
		CreatedAt:   time.Now().UTC().Unix(),
	}, ssoStateTTL); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	authURL, err := url.Parse(providerBase + "/api/auth/authorize")
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("audience", audience(m.cfg))
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	authURL.RawQuery = q.Encode()
	http.Redirect(w, r, authURL.String(), http.StatusFound)
}

func (m *authApi) callback(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "code and state are required")
		return
	}

	var entry stateEntry
	found, err := m.store.Get(r.Context(), stateCacheKey(state), &entry)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if !found || entry.State != state {
		controllers.SendError(w, controllers.ErrLimitedAccess, "sso state not valid")
		return
	}
	_ = m.store.Delete(r.Context(), stateCacheKey(state))

	token, err := m.exchangeCode(r.Context(), code, entry.RedirectURI)
	if err != nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		return
	}

	expiresAt := time.Unix(token.ExpiresAt, 0)
	if token.ExpiresAt <= 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(m.cfg.SSO.SessionTTLSeconds) * time.Second)
	}
	if err := m.auth.IssueAuthCookies(w, r, models.JwtCustomClaims{
		Id:            token.UserID,
		Email:         token.Email,
		VerifiedEmail: true,
		Name:          token.Name,
		RoleId:        token.RoleID,
		SessionId:     token.SessionID,
		AppCode:       token.AppCode,
		PolicyVersion: token.PolicyVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    token.Issuer,
			Audience:  jwt.ClaimStrings(token.Audience),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ID:        token.SessionID,
		},
	}); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	http.Redirect(w, r, cleanReturnTo(entry.ReturnTo), http.StatusFound)
}

func (m *authApi) logout(w http.ResponseWriter, r *http.Request) {
	m.auth.ClearAuthCookies(w, r)
	controllers.SendResult(w, map[string]bool{"ok": true})
}

func (m *authApi) exchangeCode(ctx context.Context, code string, redirectURI string) (providerTokenResult, error) {
	providerBase := strings.TrimRight(strings.TrimSpace(m.cfg.SSO.ProviderBaseURL), "/")
	if providerBase == "" {
		return providerTokenResult{}, fmt.Errorf("sso providerBaseUrl is required")
	}
	payload, err := json.Marshal(tokenRequest{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  redirectURI,
		ClientID:     strings.TrimSpace(m.cfg.SSO.ClientID),
		ClientSecret: m.cfg.SSO.ClientSecret,
	})
	if err != nil {
		return providerTokenResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, providerBase+"/api/auth/token", bytes.NewReader(payload))
	if err != nil {
		return providerTokenResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client, err := providerHTTPClient(m.cfg)
	if err != nil {
		return providerTokenResult{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return providerTokenResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Message == "" {
			errResp.Message = resp.Status
		}
		return providerTokenResult{}, fmt.Errorf("%s", errResp.Message)
	}
	var wrapper providerTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return providerTokenResult{}, err
	}
	if wrapper.Result.AccessToken == "" {
		return providerTokenResult{}, fmt.Errorf("sso token response is empty")
	}
	return wrapper.Result, nil
}

func providerHTTPClient(cfg *config.AppConfigModel) (*http.Client, error) {
	caCertPath := ""
	if cfg != nil {
		caCertPath = strings.TrimSpace(cfg.SSO.CACertPath)
	}
	if caCertPath == "" {
		return http.DefaultClient, nil
	}

	pemBytes, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("read sso caCertPath: %w", err)
	}
	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if !rootCAs.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("sso caCertPath does not contain a valid PEM certificate")
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: rootCAs},
		},
	}, nil
}

func redirectPath(cfg *config.AppConfigModel) string {
	if cfg != nil && strings.TrimSpace(cfg.SSO.RedirectPath) != "" {
		return cleanPath(cfg.SSO.RedirectPath)
	}
	return "/api/auth/callback"
}

func callbackURL(r *http.Request, cfg *config.AppConfigModel) string {
	if cfg != nil {
		base := strings.TrimRight(strings.TrimSpace(cfg.SSO.RedirectBaseURL), "/")
		if base != "" {
			if parsed, err := url.Parse(base); err == nil && parsed.IsAbs() && parsed.Host != "" {
				return base + redirectPath(cfg)
			}
		}
	}
	return externalURL(r, redirectPath(cfg))
}

func audience(cfg *config.AppConfigModel) string {
	if cfg != nil && strings.TrimSpace(cfg.SSO.Audience) != "" {
		return strings.TrimSpace(strings.Split(cfg.SSO.Audience, ",")[0])
	}
	return "myseliasan"
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return "/api/auth/callback"
	}
	return path
}

func cleanReturnTo(value string) string {
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

func externalURL(r *http.Request, path string) string {
	scheme := "http"
	if middlewares.IsSecureRequest(r) {
		scheme = "https"
	}
	host := r.Host
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = forwardedHost
	}
	return scheme + "://" + host + cleanPath(path)
}

func stateCacheKey(state string) string {
	return "myseliasan:sso-state:" + strings.TrimSpace(state)
}

func newOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
