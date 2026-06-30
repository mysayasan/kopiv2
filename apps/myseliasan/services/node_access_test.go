package services

import (
	"context"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	shentities "github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeGrantsRepo is an in-memory NodeAccessGrant repo that honors the RoleId/NodeId/
// Id equality filters the access service issues.
type fakeGrantsRepo struct {
	dbsql.IGenericRepo[entities.NodeAccessGrant]
	rows   []*entities.NodeAccessGrant
	nextID int64
}

func (f *fakeGrantsRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.NodeAccessGrant, uint64, error) {
	var out []*entities.NodeAccessGrant
	for _, r := range f.rows {
		if grantMatches(r, filters) {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, uint64(len(out)), nil
}
func (f *fakeGrantsRepo) Create(_ context.Context, _ string, m entities.NodeAccessGrant) (uint64, error) {
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}
func (f *fakeGrantsRepo) UpdateById(_ context.Context, _ string, m entities.NodeAccessGrant) (uint64, error) {
	for _, r := range f.rows {
		if r.Id == m.Id {
			*r = m
			return 1, nil
		}
	}
	return 0, nil
}
func (f *fakeGrantsRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, r := range f.rows {
		if uint64(r.Id) == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func grantMatches(g *entities.NodeAccessGrant, filters []sqldataenums.Filter) bool {
	for _, fl := range filters {
		switch fl.FieldName {
		case "RoleId":
			if rid, ok := fl.Value.(int64); ok && g.RoleId != rid {
				return false
			}
		case "NodeId":
			if nid, ok := fl.Value.(string); ok && g.NodeId != nid {
				return false
			}
		case "Id":
			if id, ok := fl.Value.(int64); ok && g.Id != id {
				return false
			}
		}
	}
	return true
}

func newTestAccess() (*nodeAccessService, *fakeNodesRepo, *fakeGrantsRepo) {
	nodes := &fakeNodesRepo{}
	grants := &fakeGrantsRepo{}
	return newNodeAccessService(grants, nodes, nil), nodes, grants
}

// fakeRoles is a minimal IAccessRoleService for tests: only GetById is meaningful,
// returning a superadmin role for ids in the super set.
type fakeRoles struct{ super map[int64]bool }

func (f *fakeRoles) EnsureBuiltins(ctx context.Context) error { return nil }
func (f *fakeRoles) GetByName(ctx context.Context, name string) (*shentities.AccessRole, error) {
	return nil, nil
}
func (f *fakeRoles) GetById(ctx context.Context, id int64) (*shentities.AccessRole, error) {
	return &shentities.AccessRole{Id: id, IsSuperadmin: f.super[id]}, nil
}
func (f *fakeRoles) List(ctx context.Context) ([]*shentities.AccessRole, error) { return nil, nil }
func (f *fakeRoles) Create(ctx context.Context, name, description string) (*shentities.AccessRole, error) {
	return nil, nil
}
func (f *fakeRoles) Update(ctx context.Context, id int64, name, description string) error { return nil }
func (f *fakeRoles) Delete(ctx context.Context, id int64) error                           { return nil }

func TestNodeAccessResolveMatrix(t *testing.T) {
	svc, nodes, grants := newTestAccess()
	ctx := context.Background()

	// node owned by role 10.
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: "n1", OwnerRoleId: 10})
	// role 20 has read-only, role 30 has read+write, role 40 has nothing.
	grants.rows = append(grants.rows,
		&entities.NodeAccessGrant{Id: 1, RoleId: 20, NodeId: "n1", CanRead: true},
		&entities.NodeAccessGrant{Id: 2, RoleId: 30, NodeId: "n1", CanRead: true, CanWrite: true},
	)

	cases := []struct {
		name       string
		roleId     int64
		wantRole   string
		wantRead   bool
		wantWrite  bool
	}{
		{"owner role → admin", 10, "admin", true, true},
		{"read-only grant → viewer", 20, "viewer", true, false},
		{"read+write grant → admin", 30, "admin", true, true},
		{"no grant, not owner → denied", 40, "", false, false},
		{"unknown role → denied", 99, "", false, false},
	}
	for _, c := range cases {
		acc, err := svc.Resolve(ctx, c.roleId, "n1")
		if err != nil {
			t.Fatalf("%s: Resolve error: %v", c.name, err)
		}
		if acc.CanRead != c.wantRead || acc.CanWrite != c.wantWrite || acc.Role() != c.wantRole {
			t.Fatalf("%s: got read=%v write=%v role=%q, want read=%v write=%v role=%q",
				c.name, acc.CanRead, acc.CanWrite, acc.Role(), c.wantRead, c.wantWrite, c.wantRole)
		}
	}
}

func TestNodeAccessSuperadminFullAccess(t *testing.T) {
	nodes := &fakeNodesRepo{}
	grants := &fakeGrantsRepo{}
	// role 1 is superadmin; role 2 is not.
	svc := newNodeAccessService(grants, nodes, &fakeRoles{super: map[int64]bool{1: true}})
	ctx := context.Background()

	// A node owned by some other role, with no grants for role 1 or 2.
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: "n1", OwnerRoleId: 99})

	// Superadmin role → full access despite no ownership/grant.
	acc, err := svc.Resolve(ctx, 1, "n1")
	if err != nil {
		t.Fatalf("Resolve superadmin: %v", err)
	}
	if !acc.CanRead || !acc.CanWrite || acc.Role() != "admin" {
		t.Fatalf("superadmin: got read=%v write=%v role=%q, want full admin", acc.CanRead, acc.CanWrite, acc.Role())
	}

	// Non-superadmin, non-owner, no grant → denied.
	if acc, _ := svc.Resolve(ctx, 2, "n1"); acc.Role() != "" {
		t.Fatalf("non-superadmin: expected denied, got role=%q", acc.Role())
	}
}

func TestNodeAccessSetWriteImpliesRead(t *testing.T) {
	svc, _, _ := newTestAccess()
	ctx := context.Background()

	// canWrite without canRead must be normalized to readable.
	g, err := svc.Set(ctx, entities.NodeAccessGrant{RoleId: 5, NodeId: "n2", CanRead: false, CanWrite: true})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !g.CanRead {
		t.Fatal("write grant should imply read")
	}
	// Updating the same (role,node) upserts rather than duplicating.
	g2, _ := svc.Set(ctx, entities.NodeAccessGrant{RoleId: 5, NodeId: "n2", CanRead: true, CanWrite: false})
	if g2.Id != g.Id {
		t.Fatalf("expected upsert (same id), got %d then %d", g.Id, g2.Id)
	}
	acc, _ := svc.Resolve(ctx, 5, "n2")
	if acc.CanWrite || !acc.CanRead {
		t.Fatalf("after downgrade: got read=%v write=%v, want read=true write=false", acc.CanRead, acc.CanWrite)
	}
}

func TestNodeAccessDeleteById(t *testing.T) {
	svc, _, _ := newTestAccess()
	ctx := context.Background()
	g, _ := svc.Set(ctx, entities.NodeAccessGrant{RoleId: 7, NodeId: "n3", CanRead: true})

	got, err := svc.GrantById(ctx, g.Id)
	if err != nil || got == nil || got.NodeId != "n3" {
		t.Fatalf("GrantById: %v / %+v", err, got)
	}
	if err := svc.Delete(ctx, g.Id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := svc.GrantById(ctx, g.Id); got != nil {
		t.Fatal("grant should be gone after delete")
	}
	// Resolve now denies role 7.
	if acc, _ := svc.Resolve(ctx, 7, "n3"); acc.Role() != "" {
		t.Fatalf("expected denied after delete, got role %q", acc.Role())
	}
}
