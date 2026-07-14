package services

import (
	"context"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// fakeRoles is the minimal role store the local-user tests need. The local user service now
// derives IsAdmin from the ROLE (its IsSuperadmin flag) rather than from a bool on the user,
// so every test that builds one has to be able to resolve a role.
type fakeRoles struct {
	sharedservices.IAccessRoleService
	byId   map[int64]*sharedentities.AccessRole
	byName map[string]*sharedentities.AccessRole
}

func newFakeRoles() *fakeRoles {
	admin := &sharedentities.AccessRole{Id: 1, Name: RoleAdmin, IsSuperadmin: true, Builtin: true}
	operator := &sharedentities.AccessRole{Id: 2, Name: RoleOperator}
	viewer := &sharedentities.AccessRole{Id: 3, Name: RoleViewer, Builtin: true}
	return &fakeRoles{
		byId:   map[int64]*sharedentities.AccessRole{1: admin, 2: operator, 3: viewer},
		byName: map[string]*sharedentities.AccessRole{RoleAdmin: admin, RoleOperator: operator, RoleViewer: viewer},
	}
}

func (f *fakeRoles) GetById(_ context.Context, id int64) (*sharedentities.AccessRole, error) {
	return f.byId[id], nil
}

func (f *fakeRoles) GetByName(_ context.Context, name string) (*sharedentities.AccessRole, error) {
	return f.byName[name], nil
}
