package apis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
)

type fakeLocalUserService struct {
	sessionUsername string
	sessionHash     string
}

func (f *fakeLocalUserService) EnsureDefaultAdmin(context.Context) error {
	return nil
}

func (f *fakeLocalUserService) Authenticate(_ context.Context, username string, password string) (*services.AuthenticatedUser, error) {
	if username != "admin" || password != "secret" {
		return nil, services.ErrLocalUserInvalidCredential
	}
	f.sessionUsername = username
	f.sessionHash = "session-hash"
	return &services.AuthenticatedUser{Id: 1, Username: username, DisplayName: "Admin", IsAdmin: true, SessionHash: f.sessionHash}, nil
}

func (f *fakeLocalUserService) AuthenticateSession(_ context.Context, username string, sessionHash string) (*services.AuthenticatedUser, error) {
	if username != f.sessionUsername || sessionHash != f.sessionHash {
		return nil, services.ErrLocalUserInvalidCredential
	}
	return &services.AuthenticatedUser{Id: 1, Username: username, DisplayName: "Admin", IsAdmin: true, SessionHash: sessionHash}, nil
}

func (f *fakeLocalUserService) Get(context.Context, uint64, uint64) ([]*entities.LocalUser, uint64, error) {
	return nil, 0, nil
}

func (f *fakeLocalUserService) Create(context.Context, services.CreateLocalUserRequest) (*entities.LocalUser, error) {
	return nil, nil
}

func (f *fakeLocalUserService) Update(context.Context, uint64, services.UpdateLocalUserRequest) (*entities.LocalUser, error) {
	return nil, nil
}

func (f *fakeLocalUserService) ResetPassword(context.Context, uint64, string) (*entities.LocalUser, error) {
	return nil, nil
}

func (f *fakeLocalUserService) Delete(context.Context, uint64) (uint64, error) {
	return 0, nil
}

func TestLocalBasicAuth(t *testing.T) {
	userService := &fakeLocalUserService{}
	middleware := NewLocalBasicAuth(userService)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := LocalUserFromContext(r.Context())
		if !ok || user.Username != "admin" || !user.IsAdmin {
			t.Fatalf("expected authenticated admin in context, got %#v", user)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/onvif/devices", nil)
	req.SetBasicAuth("admin", "secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("authorized status = %d", rr.Code)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != localAuthCookieName {
		t.Fatalf("expected local auth cookie, got %#v", cookies)
	}

	req = httptest.NewRequest(http.MethodGet, "http://example.com/api/onvif/devices/1/live.mjpeg", nil)
	req.AddCookie(cookies[0])
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("cookie authorized status = %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "http://example.com/api/onvif/devices", nil)
	req.SetBasicAuth("admin", "wrong")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", rr.Code)
	}
}
