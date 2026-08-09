package services

import (
	"context"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// memPageRepo / memPermRepo2 are in-memory stores so these tests exercise the REAL service —
// including the delete-and-rewrite it performs — against real derivation. Only the disk is faked.
type memPageRepo struct {
	dbsql.IGenericRepo[entities.AccessRolePage]
	rows   []*entities.AccessRolePage
	nextID int64
}

func (m *memPageRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.AccessRolePage, uint64, error) {
	var out []*entities.AccessRolePage
	for _, r := range m.rows {
		keep := true
		for _, f := range filters {
			if f.FieldName == "RoleId" {
				if id, ok := f.Value.(int64); ok && r.RoleId != id {
					keep = false
				}
			}
		}
		if keep {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, uint64(len(out)), nil
}

func (m *memPageRepo) Create(_ context.Context, _ string, row entities.AccessRolePage) (uint64, error) {
	m.nextID++
	row.Id = m.nextID
	cp := row
	m.rows = append(m.rows, &cp)
	return uint64(m.nextID), nil
}

func (m *memPageRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, r := range m.rows {
		if uint64(r.Id) == id {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

type memPermRepo2 struct {
	dbsql.IGenericRepo[entities.AccessRolePermission]
	rows   []*entities.AccessRolePermission
	nextID int64
}

func (m *memPermRepo2) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.AccessRolePermission, uint64, error) {
	var out []*entities.AccessRolePermission
	for _, r := range m.rows {
		keep := true
		for _, f := range filters {
			if f.FieldName == "RoleId" {
				if id, ok := f.Value.(int64); ok && r.RoleId != id {
					keep = false
				}
			}
		}
		if keep {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, uint64(len(out)), nil
}

func (m *memPermRepo2) Create(_ context.Context, _ string, row entities.AccessRolePermission) (uint64, error) {
	m.nextID++
	row.Id = m.nextID
	cp := row
	m.rows = append(m.rows, &cp)
	return uint64(m.nextID), nil
}

func (m *memPermRepo2) UpdateById(_ context.Context, _ string, row entities.AccessRolePermission) (uint64, error) {
	for i, r := range m.rows {
		if r.Id == row.Id {
			cp := row
			m.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, nil
}

func (m *memPermRepo2) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, r := range m.rows {
		if uint64(r.Id) == id {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

// testCatalog is a two-page app with a carve-out, small enough to reason about exactly.
func testCatalog() PageCatalog {
	return PageCatalog{
		Baseline: []PathGrant{Read("/api/auth/session")},
		Pages: []Page{
			{Id: "alpha", Group: "g", Order: 1, Levels: []PageLevel{
				{Id: "view", Grants: []PathGrant{Read("/api/alpha"), Read("/api/shared")}},
				{Id: "use", Grants: []PathGrant{Write("/api/alpha/act")}},
			}},
			{Id: "admin-thing", Group: "g", Order: 2, AdminOnly: true, Levels: []PageLevel{
				{Id: "manage", Grants: []PathGrant{Full("/api/secret")}},
			}},
		},
		Carveouts: []string{"/api/shared/private"},
	}
}

func newPageService() (IAccessRolePageService, *memPageRepo, *memPermRepo2, IAccessPermissionService) {
	pageRepo, permRepo := &memPageRepo{}, &memPermRepo2{}
	perms := NewAccessPermissionServiceWithRepo(permRepo)
	return NewAccessRolePageServiceWithRepos(pageRepo, permRepo, perms), pageRepo, permRepo, perms
}

func pathVerbs(rows []*entities.AccessRolePermission) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		v := ""
		for _, b := range []bool{r.CanGet, r.CanPost, r.CanPut, r.CanDelete} {
			if b {
				v += "1"
			} else {
				v += "0"
			}
		}
		out[r.Path] = v
	}
	return out
}

func TestSetForRole_DerivesTheMatrixFromPages(t *testing.T) {
	svc, _, permRepo, _ := newPageService()
	ctx := context.Background()

	if err := svc.SetForRole(ctx, 7, testCatalog(), []PageGrant{{PageId: "alpha", Level: "use"}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	got := pathVerbs(permRepo.rows)
	for path, want := range map[string]string{
		"/api/auth/session":   "1000", // baseline
		"/api/alpha":          "1000",
		"/api/shared":         "1000",
		"/api/alpha/act":      "0100", // the "use" rung
		"/api/shared/private": "0000", // carve-out
	} {
		if got[path] != want {
			t.Errorf("%s: got %q want %q", path, got[path], want)
		}
	}
}

// THE property page-level access exists to create: removing a page removes the access. If a
// stale grant survived, the navigation would hide the page while the API kept serving it.
func TestSetForRole_RemovingAPageRemovesItsAccess(t *testing.T) {
	svc, pageRepo, permRepo, _ := newPageService()
	ctx := context.Background()

	if err := svc.SetForRole(ctx, 7, testCatalog(), []PageGrant{{PageId: "alpha", Level: "use"}}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := svc.SetForRole(ctx, 7, testCatalog(), nil); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if len(pageRepo.rows) != 0 {
		t.Errorf("page rows survived the revoke: %d left", len(pageRepo.rows))
	}
	got := pathVerbs(permRepo.rows)
	if got["/api/alpha"] != "" {
		t.Errorf("/api/alpha still granted %q after the page was removed", got["/api/alpha"])
	}
	if got["/api/auth/session"] != "1000" {
		t.Error("the baseline must survive: a role that cannot read its own session cannot sign in")
	}
}

// An administrator's hand-written Advanced row must survive a page edit. Silently destroying a
// deliberate exception is the behaviour that would make the whole feature untrusted.
func TestRederive_LeavesHandEditedRowsAlone(t *testing.T) {
	svc, _, permRepo, perms := newPageService()
	ctx := context.Background()

	// An admin typed this in under Advanced — note Managed is false.
	if _, err := perms.Set(ctx, entities.AccessRolePermission{RoleId: 7, Path: "/api/bespoke", CanPost: true}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.SetForRole(ctx, 7, testCatalog(), []PageGrant{{PageId: "alpha", Level: "view"}}); err != nil {
		t.Fatalf("set: %v", err)
	}

	if got := pathVerbs(permRepo.rows)["/api/bespoke"]; got != "0100" {
		t.Errorf("the hand-edited row was destroyed by a page edit: got %q", got)
	}
}

// Re-deriving must not accumulate duplicate rows for the same path.
func TestSetForRole_IsIdempotent(t *testing.T) {
	svc, _, permRepo, _ := newPageService()
	ctx := context.Background()
	grants := []PageGrant{{PageId: "alpha", Level: "use"}}

	for i := 0; i < 3; i++ {
		if err := svc.SetForRole(ctx, 7, testCatalog(), grants); err != nil {
			t.Fatalf("set %d: %v", i, err)
		}
	}
	seen := map[string]int{}
	for _, r := range permRepo.rows {
		seen[r.Path]++
	}
	for path, n := range seen {
		if n != 1 {
			t.Errorf("%s has %d rows after repeated saves", path, n)
		}
	}
}

// An admin-only page cannot be stored, so it cannot be granted by a crafted request either.
func TestSetForRole_RefusesAdminOnlyAndUnknownGrants(t *testing.T) {
	svc, pageRepo, permRepo, _ := newPageService()
	ctx := context.Background()

	err := svc.SetForRole(ctx, 7, testCatalog(), []PageGrant{
		{PageId: "admin-thing", Level: "manage"},
		{PageId: "nope", Level: "view"},
		{PageId: "alpha", Level: "not-a-level"},
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if len(pageRepo.rows) != 0 {
		t.Errorf("invalid grants were stored: %+v", pageRepo.rows)
	}
	if got := pathVerbs(permRepo.rows)["/api/secret"]; got != "" {
		t.Errorf("an admin-only page was granted: %q", got)
	}
}

// The upgrade path. An install predating page-level access has permission rows and no page rows;
// those rows came from the policy catalog, so adopting the preset must CLAIM them rather than
// layer a second overlapping set on top of them.
func TestAdoptPreset_ClaimsPrePageRowsAndDoesNotDuplicate(t *testing.T) {
	svc, pageRepo, permRepo, perms := newPageService()
	ctx := context.Background()

	// Seeded by the old policy path: unmanaged, because the column did not exist then.
	for _, row := range []entities.AccessRolePermission{
		{RoleId: 7, Path: "/api/alpha", CanGet: true},
		{RoleId: 7, Path: "/api/shared", CanGet: true},
	} {
		if _, err := perms.Set(ctx, row); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	preset := []PageGrant{{PageId: "alpha", Level: "view"}}
	if err := svc.AdoptPreset(ctx, 7, testCatalog(), preset); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	seen := map[string]int{}
	for _, r := range permRepo.rows {
		seen[r.Path]++
	}
	for path, n := range seen {
		if n != 1 {
			t.Errorf("%s has %d rows after adoption — the old row was not claimed", path, n)
		}
	}
	if len(pageRepo.rows) != 1 {
		t.Errorf("expected the preset's single page row, got %d", len(pageRepo.rows))
	}
}

// Adoption is one-shot: once an administrator has chosen pages, a later boot must never impose
// the preset back over their choices.
func TestAdoptPreset_NeverOverridesAnAdminsChoices(t *testing.T) {
	svc, _, permRepo, _ := newPageService()
	ctx := context.Background()

	if err := svc.SetForRole(ctx, 7, testCatalog(), nil); err != nil { // admin removed every page
		t.Fatalf("set: %v", err)
	}
	if err := svc.AdoptPreset(ctx, 7, testCatalog(), []PageGrant{{PageId: "alpha", Level: "use"}}); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if got := pathVerbs(permRepo.rows)["/api/alpha"]; got != "" {
		t.Errorf("a boot re-imposed the preset over the admin's empty page set: %q", got)
	}
}
