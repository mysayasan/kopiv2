package services

import (
	"context"
	"testing"
)

// seedActiveAdmin creates an active, non-must-change admin with a known password
// and returns the service (backed by the counting fake) plus the user id.
func seedActiveAdmin(t *testing.T, repo *fakeLocalUserRepo, password string) (ILocalUserService, int64) {
	t.Helper()
	svc := NewLocalUserService(repo)
	user, err := svc.Create(context.Background(), CreateLocalUserRequest{
		Username: "admin", Password: password, DisplayName: "Admin",
		IsAdmin: true, IsActive: true, MustChangePassword: false,
	})
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	return svc, user.Id
}

// TestAuthenticateCachesVerification is the regression test for the load-test
// finding: the SPA replays Basic auth on every request, so a repeated correct
// credential must NOT re-run bcrypt + the LastLoginAt write every time. After
// the first verification, subsequent identical calls are served from cache with
// no further repo traffic.
func TestAuthenticateCachesVerification(t *testing.T) {
	repo := &fakeLocalUserRepo{}
	svc, _ := seedActiveAdmin(t, repo, "correct-horse")

	// First auth: cold. Hits the repo (lookup) and writes LastLoginAt.
	if _, err := svc.Authenticate(context.Background(), "admin", "correct-horse"); err != nil {
		t.Fatalf("first auth: %v", err)
	}
	lookups, writes := repo.getByUniqueCount, repo.updateCount
	if lookups == 0 || writes == 0 {
		t.Fatalf("cold auth should hit repo: lookups=%d writes=%d", lookups, writes)
	}

	// Next 20 identical auths: all cache hits — no new repo traffic at all.
	for i := 0; i < 20; i++ {
		if _, err := svc.Authenticate(context.Background(), "admin", "correct-horse"); err != nil {
			t.Fatalf("cached auth %d: %v", i, err)
		}
	}
	if repo.getByUniqueCount != lookups {
		t.Errorf("cache miss on repeated credential: lookups %d -> %d", lookups, repo.getByUniqueCount)
	}
	if repo.updateCount != writes {
		t.Errorf("LastLoginAt written on cache hit: writes %d -> %d", writes, repo.updateCount)
	}
}

// TestAuthenticateDoesNotCacheWrongPassword: a wrong password must always take
// the expensive path (so the cache can't cheapen credential guessing) and must
// never be stored — a later correct password still works, a repeated wrong one
// still fails.
func TestAuthenticateDoesNotCacheWrongPassword(t *testing.T) {
	repo := &fakeLocalUserRepo{}
	svc, _ := seedActiveAdmin(t, repo, "correct-horse")

	for i := 0; i < 3; i++ {
		if _, err := svc.Authenticate(context.Background(), "admin", "nope"); err == nil {
			t.Fatalf("wrong password unexpectedly accepted (attempt %d)", i)
		}
	}
	// Every wrong attempt must reach the repo (bcrypt), i.e. none were cached.
	if repo.getByUniqueCount < 3 {
		t.Errorf("wrong passwords were cached: only %d lookups for 3 attempts", repo.getByUniqueCount)
	}
	if _, err := svc.Authenticate(context.Background(), "admin", "correct-horse"); err != nil {
		t.Fatalf("correct password after wrong attempts: %v", err)
	}
}

// TestChangePasswordInvalidatesAuthCache: after a password change the OLD
// credential must stop authenticating immediately, even though it was cached.
func TestChangePasswordInvalidatesAuthCache(t *testing.T) {
	repo := &fakeLocalUserRepo{}
	svc, id := seedActiveAdmin(t, repo, "old-password")

	// Warm the cache with the old password.
	if _, err := svc.Authenticate(context.Background(), "admin", "old-password"); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	if _, err := svc.ChangePassword(context.Background(), id, "old-password", "new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	// The old (cached) credential must now be rejected.
	if _, err := svc.Authenticate(context.Background(), "admin", "old-password"); err == nil {
		t.Fatal("old password still authenticates after change — cache not invalidated")
	}
	// The new credential works.
	if _, err := svc.Authenticate(context.Background(), "admin", "new-password"); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
}
