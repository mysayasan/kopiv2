package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"golang.org/x/crypto/bcrypt"
)

// fakeLocalUserRepo is a minimal in-memory IGenericRepo covering just the methods
// EnsureDefaultAdmin / Create touch.
type fakeLocalUserRepo struct {
	dbsql.IGenericRepo[entities.LocalUser]
	rows   []*entities.LocalUser
	nextID uint64
}

func (f *fakeLocalUserRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.LocalUser, uint64, error) {
	return f.rows, uint64(len(f.rows)), nil
}

func (f *fakeLocalUserRepo) GetByUnique(_ context.Context, _ string, _ string, uids ...any) (*entities.LocalUser, error) {
	username, _ := uids[0].(string)
	for _, r := range f.rows {
		if r.Username == username {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakeLocalUserRepo) Create(_ context.Context, _ string, model entities.LocalUser) (uint64, error) {
	f.nextID++
	model.Id = int64(f.nextID)
	cp := model
	f.rows = append(f.rows, &cp)
	return f.nextID, nil
}

func (f *fakeLocalUserRepo) UpdateById(_ context.Context, _ string, model entities.LocalUser) (uint64, error) {
	for _, r := range f.rows {
		if r.Id == model.Id {
			*r = model
			return 1, nil
		}
	}
	return 0, nil
}

// TestEnsureDefaultAdminGeneratesWhenNoPassword covers the Linux/Docker/portable
// path: with no config or env password, a fresh install seeds a per-install
// generated password (must-change) and reports it so the caller can reveal it.
func TestEnsureDefaultAdminGeneratesWhenNoPassword(t *testing.T) {
	t.Setenv("LOCAL_ADMIN_PASSWORD", "")
	repo := &fakeLocalUserRepo{}
	svc := NewLocalUserService(repo)

	res, err := svc.EnsureDefaultAdmin(context.Background(), "admin", "")
	if err != nil {
		t.Fatalf("EnsureDefaultAdmin: %v", err)
	}
	if !res.Seeded || !res.Generated {
		t.Fatalf("want seeded+generated, got %#v", res)
	}
	if res.Username != "admin" {
		t.Fatalf("username = %q, want admin", res.Username)
	}
	if len(res.Password) < 12 {
		t.Fatalf("generated password too short: %q", res.Password)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("expected one seeded row, got %d", len(repo.rows))
	}
	row := repo.rows[0]
	if !row.MustChangePassword {
		t.Fatal("seeded admin must be flagged must-change")
	}
	// The reported password is the real credential (its bcrypt hash verifies).
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(res.Password)); err != nil {
		t.Fatalf("reported password does not match stored hash: %v", err)
	}
}

// TestEnsureDefaultAdminUsesConfigPassword covers the explicit-credential path: a
// configured password is used verbatim and reported as not generated (the operator
// already knows it), so the caller won't echo it or write a recovery file.
func TestEnsureDefaultAdminUsesConfigPassword(t *testing.T) {
	t.Setenv("LOCAL_ADMIN_PASSWORD", "")
	repo := &fakeLocalUserRepo{}
	svc := NewLocalUserService(repo)

	res, err := svc.EnsureDefaultAdmin(context.Background(), "admin", "configured-secret")
	if err != nil {
		t.Fatalf("EnsureDefaultAdmin: %v", err)
	}
	if !res.Seeded || res.Generated {
		t.Fatalf("want seeded and not generated, got %#v", res)
	}
	if res.Password != "configured-secret" {
		t.Fatalf("password = %q, want the configured value", res.Password)
	}
}

// TestEnsureDefaultAdminSkipsWhenUsersExist confirms an existing install is not
// re-seeded and reports nothing to reveal.
func TestEnsureDefaultAdminSkipsWhenUsersExist(t *testing.T) {
	repo := &fakeLocalUserRepo{rows: []*entities.LocalUser{{Id: 1, Username: "admin", PasswordHash: "x", MustChangePassword: true}}}
	svc := NewLocalUserService(repo)

	res, err := svc.EnsureDefaultAdmin(context.Background(), "admin", "")
	if err != nil {
		t.Fatalf("EnsureDefaultAdmin: %v", err)
	}
	if res.Seeded {
		t.Fatalf("existing install must not re-seed, got %#v", res)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("row count changed: %d", len(repo.rows))
	}
}

// TestResetAdminUsesEnvOverride covers the installer "reset admin login" path: an
// existing admin's password is force-reset to LOCAL_ADMIN_PASSWORD (the installer's
// generated value), flagged must-change, without creating a new row.
func TestResetAdminUsesEnvOverride(t *testing.T) {
	t.Setenv("LOCAL_ADMIN_PASSWORD", "InstallerGenerated9")
	oldHash, _ := hashLocalPassword("whatever-old")
	repo := &fakeLocalUserRepo{
		nextID: 1,
		rows:   []*entities.LocalUser{{Id: 1, Username: "admin", PasswordHash: oldHash, IsAdmin: true, IsActive: true, MustChangePassword: false}},
	}
	svc := NewLocalUserService(repo)

	res, err := svc.ResetAdmin(context.Background(), "admin", "")
	if err != nil {
		t.Fatalf("ResetAdmin: %v", err)
	}
	if !res.Seeded || res.Generated {
		t.Fatalf("want reset with env-supplied (not generated) password, got %#v", res)
	}
	if res.Password != "InstallerGenerated9" {
		t.Fatalf("password = %q, want the env override", res.Password)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("reset must not add a row, got %d", len(repo.rows))
	}
	row := repo.rows[0]
	if !row.MustChangePassword {
		t.Fatal("reset admin must be flagged must-change")
	}
	if bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("InstallerGenerated9")) != nil {
		t.Fatal("stored hash does not match the reset password")
	}
	if bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("whatever-old")) == nil {
		t.Fatal("old password should no longer verify after reset")
	}
}

// TestResetAdminGeneratesWhenNoPassword confirms a reset with no config/env value
// generates a fresh password and reports it as generated (so the caller reveals it).
func TestResetAdminGeneratesWhenNoPassword(t *testing.T) {
	t.Setenv("LOCAL_ADMIN_PASSWORD", "")
	oldHash, _ := hashLocalPassword("old-secret")
	repo := &fakeLocalUserRepo{
		nextID: 1,
		rows:   []*entities.LocalUser{{Id: 1, Username: "admin", PasswordHash: oldHash, IsAdmin: true, IsActive: true}},
	}
	svc := NewLocalUserService(repo)

	res, err := svc.ResetAdmin(context.Background(), "admin", "")
	if err != nil {
		t.Fatalf("ResetAdmin: %v", err)
	}
	if !res.Seeded || !res.Generated || len(res.Password) < 12 {
		t.Fatalf("want a generated reset password, got %#v", res)
	}
	if bcrypt.CompareHashAndPassword([]byte(repo.rows[0].PasswordHash), []byte(res.Password)) != nil {
		t.Fatal("reported reset password does not match stored hash")
	}
}

// TestResetAdminSeedsWhenNoUsers confirms a reset on an empty table falls back to
// seeding a fresh admin rather than failing.
func TestResetAdminSeedsWhenNoUsers(t *testing.T) {
	t.Setenv("LOCAL_ADMIN_PASSWORD", "")
	repo := &fakeLocalUserRepo{}
	svc := NewLocalUserService(repo)

	res, err := svc.ResetAdmin(context.Background(), "admin", "")
	if err != nil {
		t.Fatalf("ResetAdmin: %v", err)
	}
	if !res.Seeded || len(repo.rows) != 1 {
		t.Fatalf("empty table reset should seed one admin, got res=%#v rows=%d", res, len(repo.rows))
	}
}
