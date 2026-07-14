package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type fakeAccessRoleRepo struct {
	dbsql.IGenericRepo[entities.AccessRole]
	rows   []*entities.AccessRole
	nextID int64
}

func (f *fakeAccessRoleRepo) GetByUnique(_ context.Context, _ string, field string, uids ...any) (*entities.AccessRole, error) {
	for _, r := range f.rows {
		if (field == "name" && r.Name == uids[0]) || (field == "id" && r.Id == uids[0]) {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}
func (f *fakeAccessRoleRepo) GetById(_ context.Context, _ string, id uint64) (*entities.AccessRole, error) {
	for _, r := range f.rows {
		if uint64(r.Id) == id {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}
func (f *fakeAccessRoleRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.AccessRole, uint64, error) {
	return f.rows, uint64(len(f.rows)), nil
}
func (f *fakeAccessRoleRepo) Create(_ context.Context, _ string, m entities.AccessRole) (uint64, error) {
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}

type fakeAccessPermRepo struct {
	dbsql.IGenericRepo[entities.AccessRolePermission]
	rows   []*entities.AccessRolePermission
	nextID int64
}

func (f *fakeAccessPermRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.AccessRolePermission, uint64, error) {
	var out []*entities.AccessRolePermission
	for _, r := range f.rows {
		match := true
		for _, fl := range filters {
			if fl.FieldName == "RoleId" {
				if id, ok := fl.Value.(int64); ok && r.RoleId != id {
					match = false
				}
			}
		}
		if match {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, uint64(len(out)), nil
}
func (f *fakeAccessPermRepo) Create(_ context.Context, _ string, m entities.AccessRolePermission) (uint64, error) {
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}
func (f *fakeAccessPermRepo) UpdateById(_ context.Context, _ string, m entities.AccessRolePermission) (uint64, error) {
	for _, r := range f.rows {
		if r.Id == m.Id {
			*r = m
			return 1, nil
		}
	}
	return 0, nil
}
func (f *fakeAccessPermRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, r := range f.rows {
		if uint64(r.Id) == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func TestAccessRoleEnsureBuiltinsIdempotent(t *testing.T) {
	svc := NewAccessRoleServiceWithRepo(&fakeAccessRoleRepo{})
	ctx := context.Background()
	if err := svc.EnsureBuiltins(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := svc.EnsureBuiltins(ctx); err != nil {
		t.Fatalf("ensure 2: %v", err)
	}
	list, _ := svc.List(ctx)
	if len(list) != 2 {
		t.Fatalf("want 2 roles, got %d", len(list))
	}
	sa, _ := svc.GetByName(ctx, RoleSuperadmin)
	if sa == nil || !sa.IsSuperadmin {
		t.Fatalf("superadmin wrong: %+v", sa)
	}
}

func TestAccessPermissionAuthorizeMatrix(t *testing.T) {
	perms := NewAccessPermissionServiceWithRepo(&fakeAccessPermRepo{})
	ctx := context.Background()
	const viewer = int64(2)

	// An admin grants the role a read scope (the matrix no longer seeds a wildcard).
	if _, err := perms.Set(ctx, entities.AccessRolePermission{RoleId: viewer, Path: "/api", CanGet: true}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if ok, _ := perms.Authorize(ctx, viewer, "/api/nodes", "GET"); !ok {
		t.Fatal("viewer GET /api/nodes should be allowed")
	}
	if ok, _ := perms.Authorize(ctx, viewer, "/api/nodes/adopt", "POST"); ok {
		t.Fatal("viewer POST should be denied")
	}
	if _, err := perms.Set(ctx, entities.AccessRolePermission{RoleId: viewer, Path: "/api/nodes", CanGet: true, CanPost: true}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if ok, _ := perms.Authorize(ctx, viewer, "/api/nodes/adopt", "POST"); !ok {
		t.Fatal("after grant, viewer POST /api/nodes/* should be allowed (longest prefix)")
	}
	if ok, _ := perms.Authorize(ctx, 99, "/api/nodes", "GET"); ok {
		t.Fatal("unconfigured role should be denied")
	}
}

func TestEnsureViewerDefaultsStripsLegacyWildcard(t *testing.T) {
	perms := NewAccessPermissionServiceWithRepo(&fakeAccessPermRepo{})
	ctx := context.Background()
	const viewer = int64(2)

	// Simulate a legacy deployment: viewer carries the read-everything GET /api wildcard
	// plus a legitimate narrower grant.
	if _, err := perms.Set(ctx, entities.AccessRolePermission{RoleId: viewer, Path: "/api", CanGet: true}); err != nil {
		t.Fatalf("seed wildcard: %v", err)
	}
	if _, err := perms.Set(ctx, entities.AccessRolePermission{RoleId: viewer, Path: "/api/nodes", CanGet: true}); err != nil {
		t.Fatalf("seed grant: %v", err)
	}

	if err := perms.EnsureViewerDefaults(ctx, viewer); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// The /api wildcard is gone (no more read-everything)…
	if ok, _ := perms.Authorize(ctx, viewer, "/api/app-auth-config", "GET"); ok {
		t.Fatal("legacy GET /api wildcard should have been stripped")
	}
	// …but the narrower, intentional grant survives.
	if ok, _ := perms.Authorize(ctx, viewer, "/api/nodes", "GET"); !ok {
		t.Fatal("narrower /api/nodes grant must be preserved")
	}

	// Idempotent on a clean (least-privilege) viewer.
	if err := perms.EnsureViewerDefaults(ctx, viewer); err != nil {
		t.Fatalf("ensure 2: %v", err)
	}
}


// The wildcard is what makes an ACTION permission expressible at all. REST routes put the
// action AFTER the id, so a pure string prefix cannot see past "/api/cameras" — there was
// no way to let a role move a camera without also letting it CREATE one.
func TestAccessPathMatches_WildcardSegment(t *testing.T) {
	cases := []struct {
		allowed string
		request string
		want    bool
	}{
		// The case that motivated the wildcard.
		{"/api/cameras/*/ptz", "/api/cameras/7/ptz/move", true},
		{"/api/cameras/*/ptz", "/api/cameras/7/ptz/stop", true},
		{"/api/cameras/*/ptz", "/api/cameras/7", false},
		{"/api/cameras/*/ptz", "/api/cameras", false},
		{"/api/cameras/*/ptz", "/api/cameras/7/encoder", false},

		{"/api/vision/alerts/*/ack", "/api/vision/alerts/12/ack", true},
		{"/api/vision/alerts/*/ack", "/api/vision/alerts/12", false},
		{"/api/vision/alerts/*/ack", "/api/vision/rules", false},

		{"/api/notifications/*/read", "/api/notifications/3/read", true},
		{"/api/notifications/*/read", "/api/notifications", false},

		// A wildcard matches exactly ONE segment — never zero.
		{"/api/cameras/*", "/api/cameras", false},
		{"/api/cameras/*", "/api/cameras/7", true},
		{"/api/cameras/*", "/api/cameras/7/ptz/move", true}, // still a prefix

		// Plain prefixes behave exactly as before: every existing row is wildcard-free.
		{"/api/recording", "/api/recording", true},
		{"/api/recording", "/api/recording/segments/9/download", true},
		{"/api/recording", "/api/vision", false},

		// Segment-wise matching closes a hole a raw string prefix had.
		{"/api/node", "/api/nodes-secret", false},
		{"/api/node", "/api/node/1", true},

		// Trailing slashes are noise.
		{"/api/cameras/", "/api/cameras/7", true},
		{"/api/cameras", "/api/cameras/", true},
	}
	for _, tc := range cases {
		if got := accessPathMatches(tc.allowed, tc.request); got != tc.want {
			t.Errorf("accessPathMatches(%q, %q) = %v, want %v", tc.allowed, tc.request, got, tc.want)
		}
	}
}

// The most specific matching row decides. That is what lets a role be granted a whole area
// and denied one dangerous corner of it — "/api/recording" readable, but not its purge.
func TestAccessMoreSpecific(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"/api/recording/segments/purge", "/api/recording", true},
		{"/api/recording", "/api/recording/segments/purge", false},

		// On a tie, naming the action beats wildcarding it.
		{"/api/cameras/*/ptz", "/api/cameras/*/*", true},
		{"/api/cameras/*/*", "/api/cameras/*/ptz", false},

		{"/api/cameras", "/api/vision", false},

		// The old implementation compared raw string LENGTH, so a longer but SHALLOWER path
		// outranked a deeper one. This is the regression that caused.
		{"/api/vision/alerts/x/ack", "/api/cameras-archive", true},
	}
	for _, tc := range cases {
		if got := accessMoreSpecific(tc.a, tc.b); got != tc.want {
			t.Errorf("accessMoreSpecific(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// The evidentiary line, end to end through the real service: an operator may PTZ a camera
// and acknowledge an alert, and may NOT delete a recording or reconfigure the system.
func TestAccessPermission_OperatorCannotDestroyEvidence(t *testing.T) {
	perms := NewAccessPermissionServiceWithRepo(&fakeAccessPermRepo{})
	ctx := context.Background()
	const operator = int64(3)

	for _, p := range []entities.AccessRolePermission{
		{RoleId: operator, Path: "/api/cameras", CanGet: true},
		{RoleId: operator, Path: "/api/cameras/*/ptz", CanPost: true},
		{RoleId: operator, Path: "/api/recording", CanGet: true},
		{RoleId: operator, Path: "/api/vision", CanGet: true},
		{RoleId: operator, Path: "/api/vision/alerts/*/ack", CanPost: true},
	} {
		if _, err := perms.Set(ctx, p); err != nil {
			t.Fatalf("seed %s: %v", p.Path, err)
		}
	}

	for _, c := range []struct{ path, method string }{
		{"/api/cameras", "GET"},
		{"/api/cameras/7/ptz/move", "POST"},
		{"/api/recording/segments/9/download", "GET"},
		{"/api/vision/alerts/4/ack", "POST"},
	} {
		if ok, _ := perms.Authorize(ctx, operator, c.path, c.method); !ok {
			t.Errorf("operator %s %s should be ALLOWED", c.method, c.path)
		}
	}

	// The line: an operator cannot destroy evidence, and cannot reconfigure the system.
	for _, c := range []struct{ path, method string }{
		{"/api/recording/segments/9", "DELETE"},
		{"/api/recording/segments/purge", "POST"},
		{"/api/recording/purge-camera", "POST"},
		{"/api/cameras", "POST"},
		{"/api/cameras/7", "DELETE"},
		{"/api/vision/rules", "POST"},
		{"/api/settings/runtime", "PUT"},
		{"/api/system/reset", "POST"},
	} {
		if ok, _ := perms.Authorize(ctx, operator, c.path, c.method); ok {
			t.Errorf("operator %s %s must be DENIED", c.method, c.path)
		}
	}
}

// One row silently granting the entire API, on every verb ticked, is not something an admin
// should be able to click into existence. A role that should have everything is a superadmin.
func TestAccessPermission_RootPathIsRefused(t *testing.T) {
	perms := NewAccessPermissionServiceWithRepo(&fakeAccessPermRepo{})
	ctx := context.Background()

	for _, path := range []string{"/", "", "   "} {
		if _, err := perms.Set(ctx, entities.AccessRolePermission{
			RoleId: 2, Path: path, CanGet: true, CanPost: true, CanPut: true, CanDelete: true,
		}); err == nil {
			t.Fatalf("a root path %q must be refused — it grants the entire API", path)
		}
	}
}
