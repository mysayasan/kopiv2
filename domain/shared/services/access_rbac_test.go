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

	if err := perms.EnsureViewerDefaults(ctx, viewer); err != nil {
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
