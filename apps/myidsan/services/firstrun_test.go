package services

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Empty config/env password → a per-install password is GENERATED and reported,
// and a later boot must NOT rotate it (the operator just read it from the
// recovery file).
func TestEnsureStockSuperadmin_GeneratesAndDoesNotRotate(t *testing.T) {
	repo := newFakeUserLoginRepo()
	svc := NewUserLoginService(repo, nil, testPasswordPolicy())

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
	svc := NewUserLoginService(repo, nil, testPasswordPolicy())

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
	svc := NewUserLoginService(repo, nil, testPasswordPolicy())
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
