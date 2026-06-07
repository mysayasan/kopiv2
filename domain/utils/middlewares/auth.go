package middlewares

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// AuthMidware struct
type AuthMidware struct {
	secret string
}

const (
	SecureAuthCookieName = "__Host-kopiv2_access"
	DevAuthCookieName    = "kopiv2_access"
	SecureCSRFCookieName = "__Host-kopiv2_csrf"
	DevCSRFCookieName    = "kopiv2_csrf"
	CSRFHeaderName       = "X-CSRF-Token"
)

// Create NewAuth
func NewAuth(secret string) *AuthMidware {
	return &AuthMidware{secret: secret}
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

	token, err := jwt.Parse(authCookie.Value, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(m.secret), nil
	})
	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, fmt.Errorf("token not valid")
	}

	jwtClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("token not valid")
	}

	tmp, _ := json.Marshal(jwtClaims)
	claims := &models.JwtCustomClaims{}
	_ = json.Unmarshal(tmp, claims)

	if claims.Email == "" {
		return nil, fmt.Errorf("token not valid")
	}

	return claims, nil
}

// IssueAuthCookies writes the signed JWT session cookie and its CSRF companion cookie.
func (m *AuthMidware) IssueAuthCookies(w http.ResponseWriter, r *http.Request, claims models.JwtCustomClaims) error {
	token, err := m.JwtToken(claims)
	if err != nil {
		return err
	}

	csrf, err := newCSRFToken()
	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(72 * time.Hour)
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
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
func (m *AuthMidware) ClearAuthCookies(w http.ResponseWriter, _ *http.Request) {
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
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
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
