package apis

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/models"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
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
	users services.IControlUserService
	// guard is the failed-login lockout. This app had none: local-login called
	// AuthenticateLocal and returned, so the only thing between a password list and the
	// fleet console was the generic rate limiter — a budget of 30 attempts a minute that
	// refills forever, never escalates, never notices one account being attacked from many
	// addresses, and is per-instance besides, so a two-node cluster simply doubles it.
	// A live two-instance bench served 13 consecutive guesses and then signed in with the
	// correct password as if nothing had happened.
	guard *sharedapis.LoginGuard
	// audit records the authentication events. Nil-safe: the trail is best-effort by design
	// and must never be able to fail a sign-in.
	audit services.IAuditService
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

func NewAuthApi(router *mux.Router, cfg *config.AppConfigModel, auth *middlewares.AuthMidware, store cache.Store,
	users services.IControlUserService, guard *sharedapis.LoginGuard, audit services.IAuditService) {
	handler := &authApi{cfg: cfg, auth: auth, store: store, users: users, guard: guard, audit: audit}
	group := router.PathPrefix("/auth").Subrouter()
	group.HandleFunc("/start", handler.start).Methods("GET")
	group.HandleFunc("/callback", handler.callback).Methods("GET")
	group.HandleFunc("/logout", handler.logout).Methods("POST")
	// Local login is the bootstrap stock-superadmin path (username/password). The
	// change-password endpoint requires a session and is reachable while a user is
	// flagged must-change (the control-session middleware is not mounted on /auth).
	group.HandleFunc("/local-login", handler.localLogin).Methods("POST")
	group.HandleFunc("/change-password", handler.changePassword).Methods("POST")
	// Public: lets the login screen know whether federated sign-in is even available,
	// so a standalone install (no myidsan) does not offer a button that cannot work.
	group.HandleFunc("/config", handler.authConfig).Methods("GET")
}

// authConfig reports which sign-in paths this deployment actually offers. The shipped
// package leaves sso.providerBaseUrl empty (local accounts only), and the SPA hides the
// "Continue with myidsan" button when ssoEnabled is false.
func (m *authApi) authConfig(w http.ResponseWriter, r *http.Request) {
	ssoEnabled := strings.TrimSpace(m.cfg.SSO.ProviderBaseURL) != ""
	_ = controllers.SendResult(w, map[string]bool{"ssoEnabled": ssoEnabled})
}

func (m *authApi) localLogin(w http.ResponseWriter, r *http.Request) {
	if m.users == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "local login is not configured")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request")
		return
	}
	// Checked AFTER decoding so the per-account key is available: a lockout keyed only on
	// the source address never sees a spray distributed across many addresses, which is the
	// shape credential stuffing actually takes. Checked BEFORE the credential so a locked
	// caller costs no bcrypt work — that is what keeps the refusal cheap to serve under a
	// flood, and it is why waiting a lockout out by "just signing in correctly" cannot work.
	if locked, retry := m.guardLocked(r, body.Username); locked {
		sharedapis.WriteLockoutJSON(w, retry, "too many failed sign-in attempts")
		return
	}

	user, err := m.users.AuthenticateLocal(r.Context(), body.Username, body.Password)
	if err != nil {
		// A disabled account is NOT a wrong password, but it is still a refused credential
		// and still worth counting: an attacker who has learned that one account is disabled
		// would otherwise have an unmetered oracle for every other guess against it.
		reason := "invalid username or password"
		if errors.Is(err, services.ErrUserDisabled) {
			reason = "this account has been disabled"
		}
		m.recordCredentialFailure(r, services.ActionLoginFailure, body.Username, reason)
		controllers.SendError(w, controllers.ErrLimitedAccess, reason)
		return
	}
	m.guardSuccess(r, body.Username)
	// The shared auth middleware rejects any token with an empty Email claim, so the
	// session cookie MUST carry one. The stock superadmin has no real email, so fall
	// back to its username (e.g. "admin") — a stable, non-empty identifier.
	email := strings.TrimSpace(user.Email)
	if email == "" {
		email = strings.TrimSpace(user.Username)
	}
	if err := m.auth.IssueAuthCookies(w, r, models.JwtCustomClaims{
		Id:            user.Id,
		Email:         email,
		Name:          user.Name,
		RoleId:        user.RoleId,
		VerifiedEmail: false,
		AppCode:       audience(m.cfg),
	}); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	m.recordAudit(r, services.AuditEntry{
		Action:     services.ActionLoginSuccess,
		ActorId:    user.Id,
		ActorEmail: email,
		ActorRole:  user.RoleId,
		TargetType: "user",
		TargetId:   strconv.FormatInt(user.Id, 10),
		Outcome:    services.OutcomeSuccess,
		Detail:     "signed in",
		Metadata:   map[string]any{"method": "local"},
	})
	controllers.SendResult(w, map[string]any{
		"ok":                 true,
		"mustChangePassword": user.MustChangePassword,
	}, "succeed")
}

func (m *authApi) changePassword(w http.ResponseWriter, r *http.Request) {
	if m.users == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "local login is not configured")
		return
	}
	claims, err := m.auth.ClaimsFromRequest(r)
	if err != nil || claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request")
		return
	}
	// The front door being guarded is worth little if THIS endpoint is an unmetered password
	// oracle for whoever holds a stolen cookie: it checks the same account's password, and a
	// live bench walked eleven current-password guesses through it in a row. Same guard, same
	// keys — the identifier is the signed-in account, so a compromised session cannot escape
	// the account counter by never touching the login form.
	identifier := strings.TrimSpace(claims.Email)
	if locked, retry := m.guardLocked(r, identifier); locked {
		sharedapis.WriteLockoutJSON(w, retry, "too many failed password attempts")
		return
	}

	if err := m.users.ChangePassword(r.Context(), claims.Id, body.CurrentPassword, body.NewPassword); err != nil {
		if errors.Is(err, services.ErrInvalidCredentials) {
			m.recordCredentialFailure(r, services.ActionLoginFailure, identifier,
				"current password is incorrect")
			controllers.SendError(w, controllers.ErrLimitedAccess, "current password is incorrect")
			return
		}
		// A rejected NEW password (too short, same as the old one) is a policy refusal, not a
		// failed credential. Counting it would let a user lock themselves out by trying to
		// pick a password the policy keeps refusing — while they are already holding a valid
		// session and have proven nothing about them is in doubt.
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	m.guardSuccess(r, identifier)
	m.recordAudit(r, services.AuditEntry{
		Action:     services.ActionPasswordChange,
		ActorId:    claims.Id,
		ActorEmail: identifier,
		ActorRole:  claims.RoleId,
		TargetType: "user",
		TargetId:   strconv.FormatInt(claims.Id, 10),
		Outcome:    services.OutcomeSuccess,
		Detail:     "changed own password",
	})
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

// ---- the lockout and the trail -------------------------------------------------------
//
// Every helper below is nil-safe on purpose. A deployment that configures no guard, and the
// unit tests that construct this API without one, must behave exactly as they did before.

func (m *authApi) guardLocked(r *http.Request, identifier string) (bool, time.Duration) {
	if m.guard == nil {
		return false, 0
	}
	return m.guard.Locked(sharedapis.LoginGuardKeys(r, identifier)...)
}

// guardSuccess clears BOTH keys: a correct password proves this source is not spraying and
// that this account's owner is present, so neither counter should keep counting. It is also
// what stops a user who mistypes twice and then remembers from being left one attempt away
// from being shut out.
func (m *authApi) guardSuccess(r *http.Request, identifier string) {
	if m.guard == nil {
		return
	}
	m.guard.RecordSuccess(sharedapis.LoginGuardKeys(r, identifier)...)
}

// recordCredentialFailure writes the refusal to the trail, delays the response, and advances
// the lockout — recording the lockout itself only on the attempt that engages it, so the one
// event worth alerting on is not buried under every refusal that follows.
func (m *authApi) recordCredentialFailure(r *http.Request, action, attempted, reason string) {
	m.recordAudit(r, services.AuditEntry{
		Action:     action,
		ActorEmail: attempted,
		TargetType: "user",
		TargetId:   attempted,
		Outcome:    services.OutcomeDenied,
		Detail:     reason,
		Metadata:   map[string]any{"method": "local"},
	})
	if m.guard == nil {
		return
	}
	// Applied to every failure, not just the ones that lock: it costs an attacker far more
	// than it costs a person who mistyped, and it is the part that still works when the
	// attempt count is deliberately kept below the threshold.
	time.Sleep(m.guard.FailedDelay())
	if lockedNow, retry := m.guard.RecordFailure(sharedapis.LoginGuardKeys(r, attempted)...); lockedNow {
		log.Printf("login lockout engaged ip=%s account=%q retryAfter=%s",
			sharedapis.LoginGuardSourceKey(r), attempted, retry)
		m.recordAudit(r, services.AuditEntry{
			Action:     services.ActionLoginLockout,
			ActorEmail: attempted,
			TargetType: "user",
			TargetId:   attempted,
			Outcome:    services.OutcomeDenied,
			Detail:     "too many failed sign-in attempts",
			Metadata: map[string]any{
				"method":            "local",
				"retryAfterSeconds": int(retry.Seconds()),
			},
		})
	}
}

// recordAudit stamps the source address onto an entry and files it.
//
// ClientIp is the PEER address — the same one the lockout keys on — and never the
// X-Forwarded-For the app's other entries use. On a login entry the address IS the evidence,
// and a header the caller writes is evidence of nothing; a forged one would also make the
// trail disagree with the lockout about who was attacking. A proxy's claim is still kept when
// it made one, labelled as claimed rather than observed, because it is the only way to
// identify the client when the deployment really is behind a proxy.
func (m *authApi) recordAudit(r *http.Request, e services.AuditEntry) {
	if m.audit == nil {
		return
	}
	e.ClientIp = strings.TrimPrefix(sharedapis.LoginGuardSourceKey(r), "ip:")
	e.UserAgent = r.UserAgent()
	if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); fwd != "" {
		if e.Metadata == nil {
			e.Metadata = map[string]any{}
		}
		e.Metadata["claimedForwardedFor"] = fwd
	}
	m.audit.Record(r.Context(), e)
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

	// myidsan is identity-only and no longer carries an app role: we ignore
	// token.RoleID and resolve myseliasan's OWN role. Provision the federated user
	// (viewer on first sight), then stamp the myseliasan user id + role id into the
	// session so every downstream RBAC decision keys on myseliasan's own identity.
	user, err := m.users.UpsertFederated(r.Context(), token.UserID, token.Email, token.Name)
	if err != nil {
		if errors.Is(err, services.ErrUserDisabled) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "your access to this control plane has been disabled")
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	expiresAt := time.Unix(token.ExpiresAt, 0)
	if token.ExpiresAt <= 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(m.cfg.SSO.SessionTTLSeconds) * time.Second)
	}
	if err := m.auth.IssueAuthCookies(w, r, models.JwtCustomClaims{
		Id:            user.Id,
		Email:         user.Email,
		VerifiedEmail: true,
		Name:          user.Name,
		RoleId:        user.RoleId,
		SessionId:     token.SessionID,
		AppCode:       audience(m.cfg),
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
