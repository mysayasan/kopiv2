package services

import (
	"context"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"golang.org/x/crypto/bcrypt"
)

// Empty config/env password → a per-install password is GENERATED and reported,
// and a later boot must NOT rotate it (the operator just read it from the
// recovery file).
func TestEnsureStockSuperadmin_GeneratesAndDoesNotRotate(t *testing.T) {
	repo := newFakeUserLoginRepo()
	svc := NewUserLoginService(repo, nil)

	seed, err := svc.EnsureStockSuperadmin(context.Background(), "admin", "", 7)
	if err != nil {
		t.Fatalf("EnsureStockSuperadmin: %v", err)
	}
	if !seed.Seeded || !seed.Generated || len(seed.Password) < 12 {
		t.Fatalf("unexpected seed: %+v", seed)
	}
	firstHash := repo.usersByEmail["admin"].Userpwd
	if bcrypt.CompareHashAndPassword([]byte(firstHash), []byte(seed.Password)) != nil {
		t.Fatal("stored hash does not match the generated password")
	}

	again, err := svc.EnsureStockSuperadmin(context.Background(), "admin", "", 7)
	if err != nil {
		t.Fatalf("second boot: %v", err)
	}
	if again.Seeded {
		t.Fatal("second boot must not report Seeded")
	}
	if repo.usersByEmail["admin"].Userpwd != firstHash {
		t.Fatal("generated password was rotated on the second boot")
	}
}

// A config-supplied password refreshes the untouched stock account (existing
// behavior) and is reported as not generated / not seeded on later boots.
func TestEnsureStockSuperadmin_ConfigPassword(t *testing.T) {
	repo := newFakeUserLoginRepo()
	svc := NewUserLoginService(repo, nil)

	seed, err := svc.EnsureStockSuperadmin(context.Background(), "admin", "cfg-pass-1", 7)
	if err != nil || !seed.Seeded || seed.Generated || seed.Password != "cfg-pass-1" {
		t.Fatalf("seed = %+v err = %v", seed, err)
	}
	// Config password change while still untouched (must-change) → refreshed in.
	if _, err := svc.EnsureStockSuperadmin(context.Background(), "admin", "cfg-pass-2", 7); err != nil {
		t.Fatalf("refresh boot: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(repo.usersByEmail["admin"].Userpwd), []byte("cfg-pass-2")) != nil {
		t.Fatal("config password refresh did not apply")
	}
}

// The reset path force-restores access even after handoff: password reset,
// must-change re-flagged, account reactivated, role re-pinned.
func TestResetStockSuperadmin_ForcesRecovery(t *testing.T) {
	repo := newFakeUserLoginRepo()
	svc := NewUserLoginService(repo, nil)
	if _, err := svc.EnsureStockSuperadmin(context.Background(), "admin", "cfg-pass-1", 7); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Simulate handoff: operator took over the password, then account deactivated.
	taken := repo.usersByEmail["admin"]
	taken.MustChangePassword = false
	taken.IsActive = false
	taken.UserRoleId = 3

	seed, err := svc.ResetStockSuperadmin(context.Background(), "admin", "", 7)
	if err != nil {
		t.Fatalf("ResetStockSuperadmin: %v", err)
	}
	if !seed.Seeded || !seed.Generated {
		t.Fatalf("unexpected reset seed: %+v", seed)
	}
	after := repo.usersByEmail["admin"]
	if !after.MustChangePassword || !after.IsActive || after.UserRoleId != 7 {
		t.Fatalf("reset did not restore the account: %+v", after)
	}
}

// fakeRuntimeSettingRepo backs the setup-state service with an in-memory row.
type fakeRuntimeSettingRepo struct {
	dbsql.IGenericRepo[entities.RuntimeSetting]
	row *entities.RuntimeSetting
}

func (f *fakeRuntimeSettingRepo) GetByUnique(_ context.Context, _ string, keyGroup string, uids ...any) (*entities.RuntimeSetting, error) {
	if f.row == nil {
		return nil, errNoResult{}
	}
	copy := *f.row
	return &copy, nil
}

func (f *fakeRuntimeSettingRepo) Create(_ context.Context, _ string, model entities.RuntimeSetting) (uint64, error) {
	model.Id = 1
	f.row = &model
	return 1, nil
}

func (f *fakeRuntimeSettingRepo) UpdateById(_ context.Context, _ string, model entities.RuntimeSetting) (uint64, error) {
	f.row = &model
	return 1, nil
}

type errNoResult struct{}

func (errNoResult) Error() string { return "select by unique failed: no result found" }

func TestSetupState_CompleteRoundTrip(t *testing.T) {
	repo := &fakeRuntimeSettingRepo{}
	svc := NewSetupStateService(repo)

	state, err := svc.Get(context.Background())
	if err != nil || state.Completed {
		t.Fatalf("fresh state = %+v err = %v, want not completed", state, err)
	}

	done, err := svc.Complete(context.Background())
	if err != nil || !done.Completed || done.CompletedAt == 0 {
		t.Fatalf("complete = %+v err = %v", done, err)
	}
	if repo.row == nil || !strings.Contains(repo.row.Value, `"completed":true`) {
		t.Fatalf("row not persisted: %+v", repo.row)
	}

	state, err = svc.Get(context.Background())
	if err != nil || !state.Completed {
		t.Fatalf("reload = %+v err = %v, want completed", state, err)
	}

	// Completing again just refreshes the row (idempotent).
	if _, err := svc.Complete(context.Background()); err != nil {
		t.Fatalf("re-complete: %v", err)
	}
}

var _ = sqldataenums.Equal // keep the import stable alongside sibling test fakes
