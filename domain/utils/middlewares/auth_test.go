package middlewares

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/infra/cache"
)

func TestAuthMiddlewareMissingToken(t *testing.T) {
	auth := NewAuth("test-secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestAuthMiddlewareValidTokenInjectsClaims(t *testing.T) {
	auth := NewAuth("test-secret")

	claims := models.JwtCustomClaims{
		Id:    1,
		Email: "user@example.com",
		Name:  "user",
	}

	token, err := auth.JwtToken(claims)
	if err != nil {
		t.Fatalf("failed to build token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: AuthCookieNameForRequest(req), Value: token})
	rr := httptest.NewRecorder()

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxClaims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
		if !ok || ctxClaims == nil {
			t.Fatalf("claims not found in context")
		}
		if ctxClaims.Email != claims.Email {
			t.Fatalf("expected email %s, got %s", claims.Email, ctxClaims.Email)
		}
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestAuthMiddlewareRejectsWrongSecret(t *testing.T) {
	issuer := NewAuth("issuer-secret")
	token, err := issuer.JwtToken(models.JwtCustomClaims{
		Id:    1,
		Email: "user@example.com",
		Name:  "user",
	})
	if err != nil {
		t.Fatalf("failed to build token: %v", err)
	}

	validator := NewAuth("validator-secret")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: AuthCookieNameForRequest(req), Value: token})
	rr := httptest.NewRecorder()

	handler := validator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestAuthJwtTokenGeneratesSignedToken(t *testing.T) {
	auth := NewAuth("test-secret")
	_, err := auth.JwtToken(models.JwtCustomClaims{
		Id:    1,
		Email: "user@example.com",
		Name:  "user",
	})
	if err != nil {
		t.Fatalf("expected no error when generating token: %v", err)
	}
}

func TestAuthMiddlewareUnsafeMethodRequiresCSRF(t *testing.T) {
	auth := NewAuth("test-secret")
	token, err := auth.JwtToken(models.JwtCustomClaims{
		Id:    1,
		Email: "user@example.com",
		Name:  "user",
	})
	if err != nil {
		t.Fatalf("failed to build token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: AuthCookieNameForRequest(req), Value: token})
	rr := httptest.NewRecorder()

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestAuthMiddlewareUnsafeMethodAcceptsMatchingCSRF(t *testing.T) {
	auth := NewAuth("test-secret")
	token, err := auth.JwtToken(models.JwtCustomClaims{
		Id:    1,
		Email: "user@example.com",
		Name:  "user",
	})
	if err != nil {
		t.Fatalf("failed to build token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	csrf := "csrf-token"
	req.AddCookie(&http.Cookie{Name: AuthCookieNameForRequest(req), Value: token})
	req.AddCookie(&http.Cookie{Name: CSRFCookieNameForRequest(req), Value: csrf})
	req.Header.Set(CSRFHeaderName, csrf)
	rr := httptest.NewRecorder()

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestAuthMiddlewareSecureRequestRequiresSecureCookieName(t *testing.T) {
	auth := NewAuth("test-secret")
	token, err := auth.JwtToken(models.JwtCustomClaims{
		Id:    1,
		Email: "user@example.com",
		Name:  "user",
	})
	if err != nil {
		t.Fatalf("failed to build token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	req.AddCookie(&http.Cookie{Name: DevAuthCookieName, Value: token})
	rr := httptest.NewRecorder()

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestIssueAuthCookiesSetsSessionCookies(t *testing.T) {
	auth := NewAuth("test-secret")
	req := httptest.NewRequest(http.MethodPost, "https://example.com/api/login/default", nil)
	rr := httptest.NewRecorder()

	err := auth.IssueAuthCookies(rr, req, models.JwtCustomClaims{
		Id:    1,
		Email: "user@example.com",
		Name:  "user",
	})
	if err != nil {
		t.Fatalf("expected no error issuing cookies: %v", err)
	}

	cookies := rr.Result().Cookies()
	authCookie := findCookie(cookies, SecureAuthCookieName)
	if authCookie == nil {
		t.Fatalf("expected auth cookie")
	}
	if !authCookie.HttpOnly || !authCookie.Secure || authCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected secure HttpOnly Lax auth cookie, got %#v", authCookie)
	}

	csrfCookie := findCookie(cookies, SecureCSRFCookieName)
	if csrfCookie == nil {
		t.Fatalf("expected csrf cookie")
	}
	if csrfCookie.HttpOnly || !csrfCookie.Secure || csrfCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected secure readable Lax csrf cookie, got %#v", csrfCookie)
	}
}

func TestAuthWithConfigValidatesIssuerAudienceAndSessionCache(t *testing.T) {
	store := cache.NewMemoryStore(time.Minute, time.Minute)
	auth := NewAuthWithConfig(AuthConfig{
		Secret:       "test-secret",
		Issuer:       "myidsan",
		Audience:     "myidsan,mymatasan",
		AppCode:      "myidsan",
		SessionCache: store,
		SessionTTL:   time.Minute,
	})

	req := httptest.NewRequest(http.MethodPost, "https://example.com/api/login/default", nil)
	rr := httptest.NewRecorder()
	err := auth.IssueAuthCookies(rr, req, models.JwtCustomClaims{
		Id:     42,
		Email:  "user@example.com",
		Name:   "user",
		RoleId: 7,
	})
	if err != nil {
		t.Fatalf("expected no error issuing cookies: %v", err)
	}

	authCookie := findCookie(rr.Result().Cookies(), SecureAuthCookieName)
	if authCookie == nil {
		t.Fatalf("expected auth cookie")
	}

	claims, err := auth.ClaimsFromToken(req.Context(), authCookie.Value)
	if err != nil {
		t.Fatalf("expected token to validate: %v", err)
	}
	if claims.Issuer != "myidsan" {
		t.Fatalf("expected issuer myidsan, got %s", claims.Issuer)
	}
	if !claimStringsContain(claims.Audience, "mymatasan") {
		t.Fatalf("expected mymatasan audience, got %#v", claims.Audience)
	}
	if claims.SessionId == "" {
		t.Fatalf("expected session id")
	}

	validator := NewAuthWithConfig(AuthConfig{
		Secret:       "test-secret",
		Issuer:       "myidsan",
		Audience:     "mymatasan",
		SessionCache: store,
	})
	if _, err := validator.ClaimsFromToken(req.Context(), authCookie.Value); err != nil {
		t.Fatalf("expected relying app audience to validate: %v", err)
	}
}

func TestAuthWithConfigRejectsWrongAudience(t *testing.T) {
	issuer := NewAuthWithConfig(AuthConfig{
		Secret:   "test-secret",
		Issuer:   "myidsan",
		Audience: "myidsan",
	})
	token, err := issuer.JwtToken(models.JwtCustomClaims{
		Id:        1,
		Email:     "user@example.com",
		Name:      "user",
		RoleId:    1,
		AppCode:   "myidsan",
		SessionId: "sid",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "myidsan",
			Audience:  jwt.ClaimStrings{"myidsan"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
		},
	})
	if err != nil {
		t.Fatalf("failed to build token: %v", err)
	}

	validator := NewAuthWithConfig(AuthConfig{
		Secret:   "test-secret",
		Issuer:   "myidsan",
		Audience: "mymatasan",
	})
	if _, err := validator.ClaimsFromToken(context.Background(), token); err == nil {
		t.Fatalf("expected wrong audience to fail")
	}
}

func claimStringsContain(values jwt.ClaimStrings, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
