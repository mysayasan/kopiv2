package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// fakeRoles is the minimal role store the local-user tests need. The local user service now
// derives IsAdmin from the ROLE (its IsSuperadmin flag) rather than from a bool on the user,
// so every test that builds one has to be able to resolve a role.
type fakeRoles struct {
	IAccessRoleService
	byId   map[int64]*entities.AccessRole
	byName map[string]*entities.AccessRole
}

func newFakeRoles() *fakeRoles {
	admin := &entities.AccessRole{Id: 1, Name: RoleAdmin, IsSuperadmin: true, Builtin: true}
	operator := &entities.AccessRole{Id: 2, Name: RoleOperator}
	viewer := &entities.AccessRole{Id: 3, Name: RoleViewer, Builtin: true}
	return &fakeRoles{
		byId:   map[int64]*entities.AccessRole{1: admin, 2: operator, 3: viewer},
		byName: map[string]*entities.AccessRole{RoleAdmin: admin, RoleOperator: operator, RoleViewer: viewer},
	}
}

func (f *fakeRoles) GetById(_ context.Context, id int64) (*entities.AccessRole, error) {
	return f.byId[id], nil
}

func (f *fakeRoles) GetByName(_ context.Context, name string) (*entities.AccessRole, error) {
	return f.byName[name], nil
}
