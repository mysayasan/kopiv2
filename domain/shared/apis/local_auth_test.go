package apis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/shared/services"
)

type fakeLocalUserService struct {
	sessionUsername string
	sessionHash     string
	mustChange      bool
}

func (f *fakeLocalUserService) EnsureDefaultAdmin(context.Context, string, string) (services.AdminSeedResult, error) {
	return services.AdminSeedResult{}, nil
}

func (f *fakeLocalUserService) ResetAdmin(context.Context, string, string) (services.AdminSeedResult, error) {
	return services.AdminSeedResult{}, nil
}

func (f *fakeLocalUserService) Authenticate(_ context.Context, username string, password string) (*services.AuthenticatedUser, error) {
	if username != "admin" || password != "secret" {
		return nil, services.ErrLocalUserInvalidCredential
	}
	f.sessionUsername = username
	f.sessionHash = "session-hash"
	return &services.AuthenticatedUser{Id: 1, Username: username, DisplayName: "Admin", IsAdmin: true, MustChangePassword: f.mustChange, SessionHash: f.sessionHash}, nil
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

func (f *fakeLocalUserService) ChangePassword(context.Context, int64, string, string) (*services.AuthenticatedUser, error) {
	return nil, nil
}

func (f *fakeLocalUserService) BackfillRoles(context.Context, int64, int64) (int, error) {
	return 0, nil
}

func (f *fakeLocalUserService) Delete(context.Context, uint64) (uint64, error) {
	return 0, nil
}

// testAuthConfig binds the middleware to a test app name, which is what names the session
// cookie and the Basic realm.
var testAuthConfig = LocalAuthConfig{AppName: "testapp"}

func TestLocalBasicAuth(t *testing.T) {
	userService := &fakeLocalUserService{}
	middleware := NewLocalBasicAuth(testAuthConfig, userService, NewLoginGuard(LoginGuardConfig{}))
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
	if len(cookies) == 0 || cookies[0].Name != testAuthConfig.cookieName() {
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

func TestLocalBasicAuthWrongBasicDoesNotRideCookie(t *testing.T) {
	userService := &fakeLocalUserService{}
	middleware := NewLocalBasicAuth(testAuthConfig, userService, NewLoginGuard(LoginGuardConfig{}))
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// Establish a valid session cookie via a good login.
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/onvif/devices", nil)
	req.SetBasicAuth("admin", "secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected a session cookie from the good login")
	}

	// A wrong Basic credential must be rejected even though a valid cookie is
	// present — the cookie must not silently authenticate a bad login.
	req = httptest.NewRequest(http.MethodGet, "http://example.com/api/onvif/devices", nil)
	req.SetBasicAuth("admin", "wrong")
	req.AddCookie(cookies[0])
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong Basic with valid cookie: status = %d, want 401", rr.Code)
	}

	// A request with no Basic header still rides the cookie (media elements).
	req = httptest.NewRequest(http.MethodGet, "http://example.com/api/onvif/devices/1/live.mjpeg", nil)
	req.AddCookie(cookies[0])
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("no-Basic + valid cookie: status = %d, want 204", rr.Code)
	}
}

// TestLocalBasicAuthLockoutOnlyCountsLoginAttempts guards the fix for a
// self-lockout: the SPA replays its stored credential on every protected route,
// so a client whose credential just went stale (reinstall/reseed, password change
// in another tab) fires a burst of failing background/data requests. Only the
// interactive login probe (GET /auth/session) may consume the attempt budget —
// otherwise one page load locks a legitimate user out.
func TestLocalBasicAuthLockoutOnlyCountsLoginAttempts(t *testing.T) {
	userService := &fakeLocalUserService{}
	guard := NewLoginGuard(LoginGuardConfig{
		Enabled:     true,
		MaxAttempts: 3,
		Window:      time.Minute,
		BaseLockout: time.Minute,
		FailedDelay: 0,
	})
	middleware := NewLocalBasicAuth(testAuthConfig, userService, guard)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	wrong := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, "http://example.com"+path, nil)
		req.SetBasicAuth("admin", "wrong")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	// A flood of wrong credentials on ordinary protected routes (background polls,
	// media, page-load fetches) must never trip the lockout — each is denied 401.
	for i := 0; i < 10; i++ {
		if code := wrong("/api/cameras"); code != http.StatusUnauthorized {
			t.Fatalf("non-login attempt %d: status = %d, want 401 (must not count toward lockout)", i, code)
		}
	}

	// The login probe still locks after MaxAttempts. If the flood above had counted,
	// this would already be locked; instead it takes a full run of real attempts.
	if code := wrong("/api/auth/session"); code != http.StatusUnauthorized {
		t.Fatalf("login attempt 1: status = %d, want 401", code)
	}
	if code := wrong("/api/auth/session"); code != http.StatusUnauthorized {
		t.Fatalf("login attempt 2: status = %d, want 401", code)
	}
	if code := wrong("/api/auth/session"); code != http.StatusTooManyRequests {
		t.Fatalf("login attempt 3: status = %d, want 429 (lockout trips)", code)
	}
}

func TestLocalBasicAuthForcesPasswordChange(t *testing.T) {
	userService := &fakeLocalUserService{mustChange: true}
	middleware := NewLocalBasicAuth(testAuthConfig, userService, NewLoginGuard(LoginGuardConfig{}))
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// A flagged user is blocked from a normal route with the machine-readable code.
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/onvif/devices", nil)
	req.SetBasicAuth("admin", "secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("gated route status = %d, want 403", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), passwordChangeRequiredCode) {
		t.Fatalf("expected %q code in body, got %s", passwordChangeRequiredCode, rr.Body.String())
	}

	// The change-password and session paths stay reachable while flagged.
	for _, path := range []string{"http://example.com/api/auth/change-password", "http://example.com/api/auth/session"} {
		req = httptest.NewRequest(http.MethodGet, path, nil)
		req.SetBasicAuth("admin", "secret")
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("allow-listed path %s status = %d, want 204", path, rr.Code)
		}
	}
}

// A page refresh is a request with a cookie and NO Basic header. That is the whole mechanism
// behind "reloading does not sign me out", so it is asserted directly: the reloaded client is
// authenticated, and a cookie that is still young is NOT rewritten on every request.
func TestLocalAuthCookieOnlyRequestIsAuthenticatedAndNotChurned(t *testing.T) {
	userService := &fakeLocalUserService{}
	middleware := NewLocalBasicAuth(testAuthConfig, userService, NewLoginGuard(LoginGuardConfig{}))
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := LocalUserFromContext(r.Context()); !ok {
			t.Fatalf("expected an authenticated user")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/auth/session", nil)
	req.SetBasicAuth("admin", "secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("sign-in issued no cookie")
	}
	if parts := strings.Split(cookies[0].Value, ":"); len(parts) != 3 {
		t.Fatalf("cookie value = %q, want username:hash:issuedAt", cookies[0].Value)
	}

	// The reload: cookie only.
	req = httptest.NewRequest(http.MethodGet, "http://example.com/api/auth/session", nil)
	req.AddCookie(cookies[0])
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("cookie-only status = %d, want 204", rr.Code)
	}
	if got := rr.Result().Cookies(); len(got) != 0 {
		t.Fatalf("a fresh cookie was rewritten on a young session: %#v", got)
	}
}

// The sliding window: a session past its half-life is re-issued, so a client that never sends
// a Basic credential (every SPA on this middleware, after a reload) does not hit a fixed wall.
func TestLocalAuthCookieRenewsWhenHalfSpent(t *testing.T) {
	userService := &fakeLocalUserService{sessionUsername: "admin", sessionHash: "session-hash"}
	middleware := NewLocalBasicAuth(testAuthConfig, userService, NewLoginGuard(LoginGuardConfig{}))
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	aged := time.Now().Add(-localAuthCookieRenewAfter - time.Minute).Unix()
	for name, value := range map[string]string{
		"half-spent": "admin:session-hash:" + strconv.FormatInt(aged, 10),
		// The two-field shape written before this existed: an unknown age is due.
		"pre-upgrade": "admin:session-hash",
	} {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/api/doors", nil)
		req.AddCookie(&http.Cookie{Name: testAuthConfig.cookieName(), Value: value})
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("%s: status = %d, want 204", name, rr.Code)
		}
		got := rr.Result().Cookies()
		if len(got) != 1 || got[0].Name != testAuthConfig.cookieName() {
			t.Fatalf("%s: expected the cookie to be renewed, got %#v", name, got)
		}
		if got[0].Value == value {
			t.Fatalf("%s: renewed cookie carries the old issue time", name)
		}
	}
}

func TestShouldRenewLocalAuthCookie(t *testing.T) {
	cases := map[string]struct {
		issuedAt time.Time
		want     bool
	}{
		"unknown age":  {time.Time{}, true},
		"just issued":  {time.Now(), false},
		"young":        {time.Now().Add(-time.Minute), false},
		"half spent":   {time.Now().Add(-localAuthCookieRenewAfter - time.Second), true},
		"clock jumped": {time.Now().Add(time.Hour), true},
	}
	for name, tc := range cases {
		if got := shouldRenewLocalAuthCookie(tc.issuedAt); got != tc.want {
			t.Fatalf("%s: shouldRenewLocalAuthCookie = %v, want %v", name, got, tc.want)
		}
	}
}

func TestParseLocalAuthCookieAcceptsBothShapes(t *testing.T) {
	if _, _, issuedAt, ok := parseLocalAuthCookie("admin:hash"); !ok || !issuedAt.IsZero() {
		t.Fatalf("two-field cookie: ok=%v issuedAt=%v", ok, issuedAt)
	}
	user, hash, issuedAt, ok := parseLocalAuthCookie("admin:hash:1700000000")
	if !ok || user != "admin" || hash != "hash" || issuedAt.Unix() != 1700000000 {
		t.Fatalf("three-field cookie: %q %q %v %v", user, hash, issuedAt, ok)
	}
	if _, _, _, ok := parseLocalAuthCookie("admin"); ok {
		t.Fatalf("a one-field cookie must be rejected")
	}
	if _, _, _, ok := parseLocalAuthCookie(""); ok {
		t.Fatalf("an empty cookie must be rejected")
	}
}
