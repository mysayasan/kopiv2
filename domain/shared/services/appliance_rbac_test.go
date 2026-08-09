package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// stubRoles is the minimum EnsureApplianceRoles needs: the two builtins already exist and the
// operator is created on demand.
type stubRoles struct {
	IAccessRoleService
	byName  map[string]*entities.AccessRole
	created []string
}

func newStubRoles() *stubRoles {
	return &stubRoles{byName: map[string]*entities.AccessRole{
		RoleSuperadmin: {Id: 1, Name: RoleSuperadmin, IsSuperadmin: true, Builtin: true},
		RoleViewer:     {Id: 2, Name: RoleViewer, Builtin: true},
	}}
}

func (s *stubRoles) EnsureBuiltins(context.Context) error { return nil }
func (s *stubRoles) GetByName(_ context.Context, name string) (*entities.AccessRole, error) {
	return s.byName[name], nil
}
func (s *stubRoles) Create(_ context.Context, name, _ string) (*entities.AccessRole, error) {
	role := &entities.AccessRole{Id: int64(len(s.byName) + 1), Name: name}
	s.byName[name] = role
	s.created = append(s.created, name)
	return role, nil
}

// stubPerms records what was written, so a test can assert not just the end state but that an
// existing row was never touched.
type stubPerms struct {
	IAccessPermissionService
	rows    map[int64]map[string]*entities.AccessRolePermission
	setCall []string // "roleId path" per Set, in order
}

func newStubPerms() *stubPerms {
	return &stubPerms{rows: map[int64]map[string]*entities.AccessRolePermission{}}
}

func (s *stubPerms) ListForRole(_ context.Context, roleId int64) ([]*entities.AccessRolePermission, error) {
	var out []*entities.AccessRolePermission
	for _, r := range s.rows[roleId] {
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

func (s *stubPerms) Set(_ context.Context, perm entities.AccessRolePermission) (*entities.AccessRolePermission, error) {
	if s.rows[perm.RoleId] == nil {
		s.rows[perm.RoleId] = map[string]*entities.AccessRolePermission{}
	}
	cp := perm
	s.rows[perm.RoleId][perm.Path] = &cp
	s.setCall = append(s.setCall, fmt.Sprintf("%d %s", perm.RoleId, perm.Path))
	return &cp, nil
}

func testPolicy() []PolicyRule {
	return []PolicyRule{
		{Path: "/api/alpha", Description: "alpha", Viewer: VerbsRead, Operator: VerbsRead},
		{Path: "/api/beta", Description: "beta", Viewer: VerbsNone, Operator: VerbsWrite},
	}
}

func TestEnsureApplianceRoles_SeedsAnEmptyRoleInFull(t *testing.T) {
	roles, perms := newStubRoles(), newStubPerms()
	if err := EnsureApplianceRoles(context.Background(), roles, perms, testPolicy(), "operator"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	viewer := roles.byName[RoleViewer].Id
	if got := len(perms.rows[viewer]); got != 2 {
		t.Fatalf("viewer should have every catalog row, got %d", got)
	}
	if !perms.rows[viewer]["/api/alpha"].CanGet {
		t.Error("viewer should have been granted GET on /api/alpha")
	}
}

// The property that keeps a site's tuning: a role that has been retuned is never reset.
func TestEnsureApplianceRoles_NeverOverwritesATunedRow(t *testing.T) {
	roles, perms := newStubRoles(), newStubPerms()
	viewer := roles.byName[RoleViewer].Id

	// A site granted its viewer POST on /api/alpha, which the catalog does not.
	perms.Set(context.Background(), entities.AccessRolePermission{RoleId: viewer, Path: "/api/alpha", CanGet: true, CanPost: true})
	perms.setCall = nil

	if err := EnsureApplianceRoles(context.Background(), roles, perms, testPolicy(), "operator"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !perms.rows[viewer]["/api/alpha"].CanPost {
		t.Error("the site's extra POST grant was reset — tuning must survive a boot")
	}
	for _, call := range perms.setCall {
		if call == fmt.Sprintf("%d /api/alpha", viewer) {
			t.Error("an existing row was written to; it should have been left alone entirely")
		}
	}
}

// THE regression test. Before this, a role with ANY rows was skipped wholesale, so a rule ADDED
// to the catalog only ever reached brand-new installs. That is how the appliance shipped a
// corrected sign-in rule that no existing install would ever receive.
func TestEnsureApplianceRoles_BackfillsAPathAddedToTheCatalogLater(t *testing.T) {
	roles, perms := newStubRoles(), newStubPerms()
	viewer := roles.byName[RoleViewer].Id

	// An install seeded from an OLDER catalog that had only /api/alpha.
	old := []PolicyRule{{Path: "/api/alpha", Description: "alpha", Viewer: VerbsRead, Operator: VerbsRead}}
	if err := EnsureApplianceRoles(context.Background(), roles, perms, old, "operator"); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	if _, ok := perms.rows[viewer]["/api/beta"]; ok {
		t.Fatal("precondition: the old catalog should not have granted /api/beta")
	}

	// Upgrade: the catalog now also governs /api/beta.
	if err := EnsureApplianceRoles(context.Background(), roles, perms, testPolicy(), "operator"); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if _, ok := perms.rows[viewer]["/api/beta"]; !ok {
		t.Error("a path added to the catalog never reached the existing install")
	}
}

// Backfilling must be idempotent: a second boot with an unchanged catalog writes nothing.
func TestEnsureApplianceRoles_IsIdempotent(t *testing.T) {
	roles, perms := newStubRoles(), newStubPerms()
	if err := EnsureApplianceRoles(context.Background(), roles, perms, testPolicy(), "operator"); err != nil {
		t.Fatalf("first: %v", err)
	}
	perms.setCall = nil
	if err := EnsureApplianceRoles(context.Background(), roles, perms, testPolicy(), "operator"); err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(perms.setCall) != 0 {
		t.Errorf("a boot with an unchanged catalog wrote %v", perms.setCall)
	}
}
