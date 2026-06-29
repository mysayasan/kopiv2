package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"golang.org/x/crypto/bcrypt"
)

// fakeAccessRoleRepo is an in-memory accessrbac role repo for wiring the user
// service in tests (the shared role logic itself is tested in domain/accessrbac).
type fakeAccessRoleRepo struct {
	dbsql.IGenericRepo[sharedentities.AccessRole]
	rows   []*sharedentities.AccessRole
	nextID int64
}

func (f *fakeAccessRoleRepo) GetByUnique(_ context.Context, _ string, field string, uids ...any) (*sharedentities.AccessRole, error) {
	for _, r := range f.rows {
		if (field == "name" && r.Name == uids[0]) || (field == "id" && r.Id == uids[0]) {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}
func (f *fakeAccessRoleRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*sharedentities.AccessRole, uint64, error) {
	return f.rows, uint64(len(f.rows)), nil
}
func (f *fakeAccessRoleRepo) Create(_ context.Context, _ string, m sharedentities.AccessRole) (uint64, error) {
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}

type fakeUserRepo struct {
	dbsql.IGenericRepo[entities.ControlUser]
	rows   []*entities.ControlUser
	nextID int64
}

func (f *fakeUserRepo) GetByUnique(_ context.Context, _ string, field string, uids ...any) (*entities.ControlUser, error) {
	for _, r := range f.rows {
		if field == "id" && r.Id == uids[0] {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}
func (f *fakeUserRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.ControlUser, uint64, error) {
	var out []*entities.ControlUser
	for _, r := range f.rows {
		if userMatches(r, filters) {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, uint64(len(out)), nil
}
func (f *fakeUserRepo) Create(_ context.Context, _ string, m entities.ControlUser) (uint64, error) {
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}
func (f *fakeUserRepo) UpdateById(_ context.Context, _ string, m entities.ControlUser) (uint64, error) {
	for _, r := range f.rows {
		if r.Id == m.Id {
			*r = m
			return 1, nil
		}
	}
	return 0, nil
}

func userMatches(u *entities.ControlUser, filters []sqldataenums.Filter) bool {
	for _, fl := range filters {
		switch fl.FieldName {
		case "Kind":
			if s, ok := fl.Value.(string); ok && u.Kind != s {
				return false
			}
		case "Username":
			if s, ok := fl.Value.(string); ok && u.Username != s {
				return false
			}
		case "Email":
			if s, ok := fl.Value.(string); ok && u.Email != s {
				return false
			}
		case "SsoUserId":
			if id, ok := fl.Value.(int64); ok && u.SsoUserId != id {
				return false
			}
		}
	}
	return true
}

func newTestRBAC() (sharedservices.IAccessRoleService, *controlUserService) {
	roles := sharedservices.NewAccessRoleServiceWithRepo(&fakeAccessRoleRepo{})
	users := newControlUserService(&fakeUserRepo{}, roles)
	return roles, users
}

func TestUpsertFederatedAssignsViewerThenUpdates(t *testing.T) {
	roles, users := newTestRBAC()
	ctx := context.Background()
	_ = roles.EnsureBuiltins(ctx)
	viewer, _ := roles.GetByName(ctx, sharedservices.RoleViewer)

	u1, err := users.UpsertFederated(ctx, 42, "a@x.io", "Alice")
	if err != nil {
		t.Fatalf("UpsertFederated: %v", err)
	}
	if u1.Kind != "federated" || u1.RoleId != viewer.Id {
		t.Fatalf("first login should be federated viewer: %+v", u1)
	}
	u2, err := users.UpsertFederated(ctx, 42, "a@x.io", "Alice Smith")
	if err != nil {
		t.Fatalf("UpsertFederated 2: %v", err)
	}
	if u2.Id != u1.Id || u2.Name != "Alice Smith" {
		t.Fatalf("re-login should update in place: %+v", u2)
	}
}

// TestUpsertFederatedDoesNotMergeByEmail guards the privilege-escalation fix: two
// distinct SSO identities that happen to share a (non-unique) email must remain
// separate accounts, so a new login can never inherit an existing — possibly
// privileged — record by email alone.
func TestUpsertFederatedDoesNotMergeByEmail(t *testing.T) {
	roles, users := newTestRBAC()
	ctx := context.Background()
	_ = roles.EnsureBuiltins(ctx)

	u1, err := users.UpsertFederated(ctx, 1, "admin", "First")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Simulate the first account having been elevated out-of-band.
	sa, _ := roles.GetByName(ctx, sharedservices.RoleSuperadmin)
	_ = users.SetRole(ctx, u1.Id, sa.Id)

	// A different SSO id with the SAME email must create a brand-new account, not
	// rebind to (and inherit superadmin from) the first.
	u2, err := users.UpsertFederated(ctx, 2, "admin", "Second")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if u2.Id == u1.Id {
		t.Fatal("distinct SSO ids sharing an email must not merge into one account")
	}
	viewer, _ := roles.GetByName(ctx, sharedservices.RoleViewer)
	if u2.RoleId != viewer.Id {
		t.Fatalf("new federated account must default to viewer, got roleId=%d", u2.RoleId)
	}
}

// TestUpsertFederatedRequiresStableId rejects a login with no usable SSO subject id,
// so a stream of id-less logins can't pile up indistinguishable accounts.
func TestUpsertFederatedRequiresStableId(t *testing.T) {
	roles, users := newTestRBAC()
	ctx := context.Background()
	_ = roles.EnsureBuiltins(ctx)
	if _, err := users.UpsertFederated(ctx, 0, "x@y.io", "NoId"); err == nil {
		t.Fatal("expected rejection of federated login without a stable user id")
	}
}

func TestUpsertFederatedRejectsDisabled(t *testing.T) {
	roles, users := newTestRBAC()
	ctx := context.Background()
	_ = roles.EnsureBuiltins(ctx)

	_, _ = users.UpsertFederated(ctx, 7, "b@x.io", "Bob")
	users.repo.(*fakeUserRepo).rows[0].Disabled = true
	if _, err := users.UpsertFederated(ctx, 7, "b@x.io", "Bob"); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled user: got %v want ErrUserDisabled", err)
	}
}

func TestAuthenticateLocalAndChangePassword(t *testing.T) {
	roles, users := newTestRBAC()
	ctx := context.Background()
	_ = roles.EnsureBuiltins(ctx)
	if err := users.EnsureStockSuperadmin(ctx, "admin", "initpass1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := users.AuthenticateLocal(ctx, "admin", "nope"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong pw: got %v want ErrInvalidCredentials", err)
	}
	u, err := users.AuthenticateLocal(ctx, "admin", "initpass1")
	if err != nil || u == nil || !u.MustChangePassword {
		t.Fatalf("auth: %v / %+v", err, u)
	}
	if err := users.ChangePassword(ctx, u.Id, "wrong", "brandnewpass"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("change wrong current: got %v", err)
	}
	if err := users.ChangePassword(ctx, u.Id, "initpass1", "short"); err == nil {
		t.Fatal("expected short-password rejection")
	}
	if err := users.ChangePassword(ctx, u.Id, "initpass1", "brandnewpass"); err != nil {
		t.Fatalf("change: %v", err)
	}
	u2, err := users.AuthenticateLocal(ctx, "admin", "brandnewpass")
	if err != nil || u2.MustChangePassword {
		t.Fatalf("post-change auth: %v / mustChange=%v", err, u2.MustChangePassword)
	}
}

func TestRetireStock(t *testing.T) {
	roles, users := newTestRBAC()
	ctx := context.Background()
	_ = roles.EnsureBuiltins(ctx)
	_ = users.EnsureStockSuperadmin(ctx, "admin", "initpass1")
	_, _ = users.UpsertFederated(ctx, 1, "real@x.io", "Real")

	n, err := users.RetireStock(ctx)
	if err != nil || n != 1 {
		t.Fatalf("RetireStock: %v / retired %d", err, n)
	}
	stock, _ := users.getByUsername(ctx, "admin")
	if stock == nil || !stock.Disabled {
		t.Fatalf("stock not disabled: %+v", stock)
	}
	if _, err := users.AuthenticateLocal(ctx, "admin", "initpass1"); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled stock auth: got %v want ErrUserDisabled", err)
	}
}

func TestEnsureStockSuperadminSeedsLocalMustChange(t *testing.T) {
	roles, users := newTestRBAC()
	ctx := context.Background()
	_ = roles.EnsureBuiltins(ctx)
	sa, _ := roles.GetByName(ctx, sharedservices.RoleSuperadmin)

	if err := users.EnsureStockSuperadmin(ctx, "root", "s3cret"); err != nil {
		t.Fatalf("EnsureStockSuperadmin: %v", err)
	}
	if err := users.EnsureStockSuperadmin(ctx, "root", "s3cret"); err != nil {
		t.Fatalf("EnsureStockSuperadmin 2: %v", err)
	}
	stock, err := users.getByUsername(ctx, "root")
	if err != nil || stock == nil {
		t.Fatalf("stock not seeded: %v / %v", err, stock)
	}
	if stock.Kind != "local" || !stock.IsStock || !stock.MustChangePassword || stock.RoleId != sa.Id {
		t.Fatalf("stock superadmin wrong: %+v", stock)
	}
	if bcrypt.CompareHashAndPassword([]byte(stock.PasswordHash), []byte("s3cret")) != nil {
		t.Fatal("stock password hash does not verify")
	}
}
