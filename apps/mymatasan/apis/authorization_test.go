package apis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

func withUser(r *http.Request, user *services.AuthenticatedUser) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), localAuthContextKey{}, user))
}

// stubRoles/stubPerms exercise the MIDDLEWARE's decisions. The catalog itself — what each
// built-in role may actually do — is tested against the real matcher in
// services.TestPolicy_*, which is where the evidentiary line is asserted.
type stubRoles struct {
	sharedservices.IAccessRoleService
}

type stubPerms struct {
	sharedservices.IAccessPermissionService
	allow bool
	err   error
}

func (s stubPerms) Authorize(context.Context, int64, string, string) (bool, error) {
	return s.allow, s.err
}

func middleware(perms sharedservices.IAccessPermissionService) http.Handler {
	return NewRequireRolePermission(stubRoles{}, perms)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
}

func TestRequireRolePermission(t *testing.T) {
	cases := []struct {
		name  string
		user  *services.AuthenticatedUser
		perms sharedservices.IAccessPermissionService
		want  int
	}{
		{
			// A superadmin bypasses the matrix. This is the only place "admin gets everything"
			// still lives, and it is now a flag on a ROLE, not a bool on a user.
			name: "superadmin bypasses the matrix",
			user: &services.AuthenticatedUser{Id: 1, Username: "admin", RoleId: 1, IsAdmin: true},
			// Deliberately denying: the bypass must not consult it.
			perms: stubPerms{allow: false},
			want:  http.StatusNoContent,
		},
		{
			name:  "matrix allows",
			user:  &services.AuthenticatedUser{Id: 2, Username: "op", RoleId: 2},
			perms: stubPerms{allow: true},
			want:  http.StatusNoContent,
		},
		{
			name:  "matrix denies",
			user:  &services.AuthenticatedUser{Id: 2, Username: "op", RoleId: 2},
			perms: stubPerms{allow: false},
			want:  http.StatusForbidden,
		},
		{
			// A freshly created account an admin has not finished setting up can do nothing.
			name:  "a user with no role is denied",
			user:  &services.AuthenticatedUser{Id: 3, Username: "new", RoleId: 0},
			perms: stubPerms{allow: true},
			want:  http.StatusForbidden,
		},
		{
			// An authorization check that cannot RUN is not a reason to let the request
			// through. Fail closed.
			name:  "a matrix error fails closed",
			user:  &services.AuthenticatedUser{Id: 2, Username: "op", RoleId: 2},
			perms: stubPerms{allow: true, err: context.DeadlineExceeded},
			want:  http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := withUser(httptest.NewRequest(http.MethodPost, "http://example.com/api/cameras", nil), tc.user)
			rr := httptest.NewRecorder()
			middleware(tc.perms).ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d", rr.Code, tc.want)
			}
		})
	}
}

// No principal at all means the auth middleware did not run. Never let that through.
func TestRequireRolePermission_FailsClosedWithoutUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/settings/runtime", nil)
	rr := httptest.NewRecorder()
	middleware(stubPerms{allow: true}).ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("missing principal: status = %d, want 403", rr.Code)
	}
}

var _ = sharedentities.AccessRole{}
