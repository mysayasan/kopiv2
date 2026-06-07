package apis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type fakeUserLoginService struct {
	authenticateResult *entities.UserLogin
	authenticateErr    error
	registerErr        error
	registerID         uint64

	authenticateCalls int
	registerCalls     int
	lastUsername      string
	lastPassword      string
	lastRegister      *entities.UserLogin
}

func (f *fakeUserLoginService) Get(_ context.Context, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.UserLogin, uint64, error) {
	return nil, 0, nil
}

func (f *fakeUserLoginService) GetByEmail(_ context.Context, _ string) (*entities.UserLogin, error) {
	return nil, nil
}

func (f *fakeUserLoginService) AuthenticateDefault(_ context.Context, username string, password string) (*entities.UserLogin, error) {
	f.authenticateCalls++
	f.lastUsername = username
	f.lastPassword = password
	if f.authenticateErr != nil {
		return nil, f.authenticateErr
	}
	return f.authenticateResult, nil
}

func (f *fakeUserLoginService) RegisterLocal(_ context.Context, model entities.UserLogin) (uint64, error) {
	f.registerCalls++
	copy := model
	f.lastRegister = &copy
	if f.registerErr != nil {
		return 0, f.registerErr
	}
	if f.registerID == 0 {
		return 1, nil
	}
	return f.registerID, nil
}

func (f *fakeUserLoginService) Create(_ context.Context, _ entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginService) Update(_ context.Context, _ entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginService) Delete(_ context.Context, _ uint64) (uint64, error) {
	return 0, nil
}

func TestDefaultLogin_SuccessIssuesSessionCookies(t *testing.T) {
	h, svc := newLoginHandlerForTest(t)
	svc.authenticateResult = &entities.UserLogin{
		Id:         21,
		Email:      "local-user",
		FirstName:  "Local",
		LastName:   "User",
		UserRoleId: 2,
		IsActive:   true,
	}

	rr := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/login/default", map[string]any{
		"username": "local-user",
		"password": "secret123",
	})

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var body struct {
		Result map[string]bool `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response decode failed: %v", err)
	}

	if !body.Result["ok"] {
		t.Fatalf("expected ok session result")
	}
	if cookieByName(rr.Result().Cookies(), middlewares.DevAuthCookieName) == nil {
		t.Fatalf("expected auth cookie")
	}
	if cookieByName(rr.Result().Cookies(), middlewares.DevCSRFCookieName) == nil {
		t.Fatalf("expected csrf cookie")
	}
	if svc.authenticateCalls != 1 {
		t.Fatalf("expected AuthenticateDefault to be called once, got %d", svc.authenticateCalls)
	}
}

func TestDefaultLogin_InvalidCredentialReturnsUnauthorized(t *testing.T) {
	h, svc := newLoginHandlerForTest(t)
	svc.authenticateErr = services.ErrInvalidCredential

	rr := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/login/default", map[string]any{
		"username": "local-user",
		"password": "wrong",
	})

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}
}

func TestDefaultLogin_UnknownFieldReturnsBadRequest(t *testing.T) {
	h, _ := newLoginHandlerForTest(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login/default", bytes.NewBufferString(`{"username":"local-user","password":"secret123","extra":"x"}`))
	req.Header.Set("Content-Type", "application/json")

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
}

func TestDefaultRegister_SuccessIssuesSessionCookies(t *testing.T) {
	h, svc := newLoginHandlerForTest(t)
	svc.registerID = 55
	svc.authenticateResult = &entities.UserLogin{
		Id:         55,
		Email:      "new-user",
		FirstName:  "New",
		LastName:   "User",
		UserRoleId: 0,
		IsActive:   true,
	}

	rr := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/login/default/register", map[string]any{
		"username":  "new-user",
		"password":  "new-pass-123",
		"firstName": "New",
		"lastName":  "User",
	})

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if svc.registerCalls != 1 {
		t.Fatalf("expected RegisterLocal to be called once, got %d", svc.registerCalls)
	}
	if svc.authenticateCalls != 1 {
		t.Fatalf("expected AuthenticateDefault to be called once, got %d", svc.authenticateCalls)
	}
	if svc.lastRegister == nil || svc.lastRegister.Email != "new-user" {
		t.Fatalf("expected register payload to be passed, got %#v", svc.lastRegister)
	}

	var body struct {
		Result map[string]bool `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response decode failed: %v", err)
	}
	if !body.Result["ok"] {
		t.Fatalf("expected ok session result")
	}
	if cookieByName(rr.Result().Cookies(), middlewares.DevAuthCookieName) == nil {
		t.Fatalf("expected auth cookie")
	}
	if cookieByName(rr.Result().Cookies(), middlewares.DevCSRFCookieName) == nil {
		t.Fatalf("expected csrf cookie")
	}
}

func TestDefaultLogoutClearsSessionCookies(t *testing.T) {
	h, _ := newLoginHandlerForTest(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login/default/logout", nil)

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	authCookie := cookieByName(rr.Result().Cookies(), middlewares.DevAuthCookieName)
	if authCookie == nil || authCookie.MaxAge != -1 {
		t.Fatalf("expected expired dev auth cookie, got %#v", authCookie)
	}

	csrfCookie := cookieByName(rr.Result().Cookies(), middlewares.DevCSRFCookieName)
	if csrfCookie == nil || csrfCookie.MaxAge != -1 {
		t.Fatalf("expected expired dev csrf cookie, got %#v", csrfCookie)
	}
}

func TestDefaultRegister_AccountExistsReturnsConflict(t *testing.T) {
	h, svc := newLoginHandlerForTest(t)
	svc.registerErr = services.ErrAccountAlreadyExists

	rr := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/login/default/register", map[string]any{
		"username": "existing-user",
		"password": "new-pass",
	})

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusConflict, rr.Code, rr.Body.String())
	}
	if svc.authenticateCalls != 0 {
		t.Fatalf("expected AuthenticateDefault not to be called on register failure")
	}
}

func TestDefaultRegister_ThirdPartyOnlyReturnsForbidden(t *testing.T) {
	h, svc := newLoginHandlerForTest(t)
	svc.registerErr = services.ErrThirdPartyOnlyAccount

	rr := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/login/default/register", map[string]any{
		"username": "oauth-only",
		"password": "new-pass",
	})

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusForbidden, rr.Code, rr.Body.String())
	}
}

func TestDefaultRegister_AuthAfterCreateFailureReturnsInternalServerError(t *testing.T) {
	h, svc := newLoginHandlerForTest(t)
	svc.registerID = 77
	svc.authenticateErr = errors.New("unexpected auth failure")

	rr := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/login/default/register", map[string]any{
		"username": "new-user",
		"password": "new-pass",
	})

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusInternalServerError, rr.Code, rr.Body.String())
	}
}

func newLoginHandlerForTest(t *testing.T) (http.Handler, *fakeUserLoginService) {
	t.Helper()
	r := mux.NewRouter()
	svc := &fakeUserLoginService{}
	auth := middlewares.NewAuth("unit-test-secret")
	NewLoginApi(r, nil, *auth, svc)
	return r, svc
}

func jsonRequest(method string, target string, body map[string]any) *http.Request {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, target, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func cookieByName(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
