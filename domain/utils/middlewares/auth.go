package middlewares

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/cache"
)

// AuthMidware struct
type AuthMidware struct {
	secret        string
	issuer        string
	audience      string
	audiences     []string
	appCode       string
	sessionCache  cache.Store
	sessionTTL    time.Duration
	policyVersion int64
}

const (
	SecureAuthCookieName = "__Host-kopiv2_access"
	DevAuthCookieName    = "kopiv2_access"
	SecureCSRFCookieName = "__Host-kopiv2_csrf"
	DevCSRFCookieName    = "kopiv2_csrf"
	CSRFHeaderName       = "X-CSRF-Token"
	defaultSessionTTL    = 72 * time.Hour
	defaultPolicyVersion = int64(1)
)

type AuthConfig struct {
	Secret        string
	Issuer        string
	Audience      string
	AppCode       string
	SessionCache  cache.Store
	SessionTTL    time.Duration
	PolicyVersion int64
}

type SessionCacheEntry struct {
	SessionId     string `json:"sessionId"`
	UserId        int64  `json:"userId"`
	RoleId        int64  `json:"roleId"`
	Email         string `json:"email"`
	AppCode       string `json:"appCode"`
	Issuer        string `json:"issuer"`
	Audience      string `json:"audience"`
	PolicyVersion int64  `json:"policyVersion"`
	ExpiresAt     int64  `json:"expiresAt"`
	Revoked       bool   `json:"revoked"`
}

// Create NewAuth
func NewAuth(secret string) *AuthMidware {
	return NewAuthWithConfig(AuthConfig{Secret: secret})
}

func NewAuthWithConfig(cfg AuthConfig) *AuthMidware {
	sessionTTL := cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = defaultSessionTTL
	}
	policyVersion := cfg.PolicyVersion
	if policyVersion <= 0 {
		policyVersion = defaultPolicyVersion
	}
	return &AuthMidware{
		secret:        cfg.Secret,
		issuer:        strings.TrimSpace(cfg.Issuer),
		audience:      firstCSVValue(cfg.Audience),
		audiences:     splitCSVValues(cfg.Audience),
		appCode:       strings.TrimSpace(cfg.AppCode),
		sessionCache:  cfg.SessionCache,
		sessionTTL:    sessionTTL,
		policyVersion: policyVersion,
	}
}

// Middleware function, which will be called for each request
func (m *AuthMidware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := validateCSRF(r); err != nil {
			controllers.SendError(w, controllers.ErrPermission, err.Error())
			return
		}

		claims, err := m.ClaimsFromRequest(r)
		if err != nil {
			controllers.SendError(w, controllers.ErrPermission, err.Error())
			return
		}

		ctx := context.WithValue(r.Context(), enumauth.Claims, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ClaimsFromRequest validates the auth cookie and returns JWT custom claims.
func (m *AuthMidware) ClaimsFromRequest(r *http.Request) (*models.JwtCustomClaims, error) {
	authCookie, err := r.Cookie(AuthCookieNameForRequest(r))
	if err != nil || strings.TrimSpace(authCookie.Value) == "" {
		return nil, fmt.Errorf("auth cookie not found")
	}

	return m.ClaimsFromToken(r.Context(), authCookie.Value)
}

// ClaimsFromToken validates a raw bearer/cookie JWT and returns SSO-aware claims.
func (m *AuthMidware) ClaimsFromToken(ctx context.Context, rawToken string) (*models.JwtCustomClaims, error) {
	rawToken = strings.TrimSpace(strings.TrimPrefix(rawToken, "Bearer "))
	if rawToken == "" {
		return nil, fmt.Errorf("token not found")
	}

	claims := &models.JwtCustomClaims{}
	parserOptions := make([]jwt.ParserOption, 0, 2)
	if m.issuer != "" {
		parserOptions = append(parserOptions, jwt.WithIssuer(m.issuer))
	}

	token, err := jwt.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(m.secret), nil
	}, parserOptions...)
	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, fmt.Errorf("token not valid")
	}

	if claims.Email == "" {
		return nil, fmt.Errorf("token not valid")
	}

	if !m.audienceMatches(claims.Audience) {
		return nil, fmt.Errorf("token audience not valid")
	}

	if err := m.validateSession(ctx, claims); err != nil {
		return nil, err
	}

	return claims, nil
}

// IssueAuthCookies writes the signed JWT session cookie and its CSRF companion cookie.
func (m *AuthMidware) IssueAuthCookies(w http.ResponseWriter, r *http.Request, claims models.JwtCustomClaims) error {
	now := time.Now()
	expiresAt := now.Add(m.sessionTTL)
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	} else {
		claims.ExpiresAt = jwt.NewNumericDate(expiresAt)
	}
	if claims.IssuedAt == nil {
		claims.IssuedAt = jwt.NewNumericDate(now)
	}
	if claims.Issuer == "" && m.issuer != "" {
		claims.Issuer = m.issuer
	}
	if len(claims.Audience) == 0 && len(m.audiences) > 0 {
		claims.Audience = jwt.ClaimStrings(m.audiences)
	}
	if claims.SessionId == "" {
		sessionId, err := newOpaqueToken()
		if err != nil {
			return err
		}
		claims.SessionId = sessionId
	}
	if claims.ID == "" {
		claims.ID = claims.SessionId
	}
	if claims.AppCode == "" {
		claims.AppCode = m.appCode
	}
	if claims.PolicyVersion <= 0 {
		claims.PolicyVersion = m.policyVersion
	}

	token, err := m.JwtToken(claims)
	if err != nil {
		return err
	}

	csrf, err := newCSRFToken()
	if err != nil {
		return err
	}

	if err := m.storeSession(r.Context(), claims, expiresAt); err != nil {
		return err
	}

	secure := IsSecureRequest(r)
	cookieBase := http.Cookie{
		Path:     "/",
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	}

	authCookie := cookieBase
	authCookie.Name = AuthCookieNameForRequest(r)
	authCookie.Value = token
	authCookie.HttpOnly = true
	http.SetCookie(w, &authCookie)

	csrfCookie := cookieBase
	csrfCookie.Name = CSRFCookieNameForRequest(r)
	csrfCookie.Value = csrf
	http.SetCookie(w, &csrfCookie)

	return nil
}

// ClearAuthCookies removes both secure and local-development auth cookies.
func (m *AuthMidware) ClearAuthCookies(w http.ResponseWriter, r *http.Request) {
	if r != nil && m.sessionCache != nil {
		if claims, err := m.ClaimsFromRequest(r); err == nil && claims != nil && claims.SessionId != "" {
			_ = m.sessionCache.Delete(r.Context(), sessionCacheKey(claims.SessionId))
		}
	}
	for _, cookie := range []http.Cookie{
		expiredCookie(SecureAuthCookieName, true, true),
		expiredCookie(SecureCSRFCookieName, true, false),
		expiredCookie(DevAuthCookieName, false, true),
		expiredCookie(DevCSRFCookieName, false, false),
	} {
		http.SetCookie(w, &cookie)
	}
}

// Jwt Token
func (m *AuthMidware) JwtToken(claims models.JwtCustomClaims) (string, error) {
	// Create token
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Generate encoded token for the auth cookie.
	t, err := jwtToken.SignedString([]byte(m.secret))
	if err != nil {
		return "", err
	}

	return t, nil
}

// AuthCookieNameForRequest returns the cookie name expected for the current transport.
func AuthCookieNameForRequest(r *http.Request) string {
	if IsSecureRequest(r) {
		return SecureAuthCookieName
	}
	return DevAuthCookieName
}

// CSRFCookieNameForRequest returns the CSRF cookie name expected for the current transport.
func CSRFCookieNameForRequest(r *http.Request) string {
	if IsSecureRequest(r) {
		return SecureCSRFCookieName
	}
	return DevCSRFCookieName
}

// IsSecureRequest detects TLS directly or through a trusted upstream proxy header.
func IsSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func validateCSRF(r *http.Request) error {
	if !requiresCSRF(r.Method) {
		return nil
	}

	headerValue := strings.TrimSpace(r.Header.Get(CSRFHeaderName))
	if headerValue == "" {
		return fmt.Errorf("csrf token not found")
	}

	csrfCookie, err := r.Cookie(CSRFCookieNameForRequest(r))
	if err != nil || strings.TrimSpace(csrfCookie.Value) == "" {
		return fmt.Errorf("csrf cookie not found")
	}

	if subtle.ConstantTimeCompare([]byte(headerValue), []byte(csrfCookie.Value)) != 1 {
		return fmt.Errorf("csrf token not valid")
	}

	return nil
}

func requiresCSRF(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func newCSRFToken() (string, error) {
	return newOpaqueToken()
}

func newOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (m *AuthMidware) storeSession(ctx context.Context, claims models.JwtCustomClaims, expiresAt time.Time) error {
	if m.sessionCache == nil || claims.SessionId == "" {
		return nil
	}

	entry := SessionCacheEntry{
		SessionId:     claims.SessionId,
		UserId:        claims.Id,
		RoleId:        claims.RoleId,
		Email:         claims.Email,
		AppCode:       claims.AppCode,
		Issuer:        claims.Issuer,
		PolicyVersion: claims.PolicyVersion,
		ExpiresAt:     expiresAt.Unix(),
	}
	if len(claims.Audience) > 0 {
		entry.Audience = claims.Audience[0]
	}

	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		ttl = m.sessionTTL
	}
	return m.sessionCache.Set(ctx, sessionCacheKey(claims.SessionId), entry, ttl)
}

func (m *AuthMidware) validateSession(ctx context.Context, claims *models.JwtCustomClaims) error {
	if m.sessionCache == nil || claims.SessionId == "" {
		return nil
	}

	var entry SessionCacheEntry
	found, err := m.sessionCache.Get(ctx, sessionCacheKey(claims.SessionId), &entry)
	if err != nil {
		return fmt.Errorf("session cache unavailable")
	}
	if !found || entry.Revoked {
		return fmt.Errorf("session not active")
	}
	if entry.ExpiresAt > 0 && time.Now().Unix() >= entry.ExpiresAt {
		return fmt.Errorf("session expired")
	}
	if entry.UserId != 0 && entry.UserId != claims.Id {
		return fmt.Errorf("session user mismatch")
	}
	if entry.RoleId != 0 && entry.RoleId != claims.RoleId {
		return fmt.Errorf("session role mismatch")
	}
	if entry.Email != "" && !strings.EqualFold(entry.Email, claims.Email) {
		return fmt.Errorf("session email mismatch")
	}
	return nil
}

func sessionCacheKey(sessionId string) string {
	return "sso:session:" + strings.TrimSpace(sessionId)
}

func (m *AuthMidware) audienceMatches(tokenAudiences jwt.ClaimStrings) bool {
	if len(m.audiences) == 0 {
		return true
	}
	for _, expected := range m.audiences {
		for _, actual := range tokenAudiences {
			if strings.EqualFold(strings.TrimSpace(actual), expected) {
				return true
			}
		}
	}
	return false
}

func firstCSVValue(value string) string {
	values := splitCSVValues(value)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func splitCSVValues(value string) []string {
	rawValues := strings.Split(value, ",")
	values := make([]string, 0, len(rawValues))
	for _, raw := range rawValues {
		v := strings.TrimSpace(raw)
		if v != "" {
			values = append(values, v)
		}
	}
	return values
}

func expiredCookie(name string, secure bool, httpOnly bool) http.Cookie {
	return http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Secure:   secure,
		HttpOnly: httpOnly,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	}
}
