package services

import (
	"context"
	"testing"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeSessionRepo is an in-memory IGenericRepo supporting the filters the session service
// actually issues (equality on UserLoginId and SessionId).
type fakeSessionRepo struct {
	dbsql.IGenericRepo[sharedentities.UserSession]
	rows   []*sharedentities.UserSession
	nextID int64
}

func (f *fakeSessionRepo) Get(_ context.Context, _ string, limit uint64, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*sharedentities.UserSession, uint64, error) {
	matched := make([]*sharedentities.UserSession, 0, len(f.rows))
	for _, row := range f.rows {
		keep := true
		for _, filter := range filters {
			switch filter.FieldName {
			case "UserLoginId":
				if row.UserLoginId != filter.Value.(int64) {
					keep = false
				}
			case "SessionId":
				if row.SessionId != filter.Value.(string) {
					keep = false
				}
			case "IsActive":
				// Honoured because CountActive filters on it. A fake that silently
				// ignores a filter reports every row as matching, which makes a
				// count assertion pass for the wrong reason or fail for a reason
				// that has nothing to do with the code under test.
				want, ok := filter.Value.(bool)
				if ok && row.IsActive != want {
					keep = false
				}
			}
		}
		if keep {
			cp := *row
			matched = append(matched, &cp)
		}
	}
	total := uint64(len(matched))
	if offset > total {
		offset = total
	}
	matched = matched[offset:]
	if limit > 0 && uint64(len(matched)) > limit {
		matched = matched[:limit]
	}
	return matched, total, nil
}

func (f *fakeSessionRepo) Create(_ context.Context, _ string, model sharedentities.UserSession) (uint64, error) {
	f.nextID++
	model.Id = f.nextID
	cp := model
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}

func (f *fakeSessionRepo) UpdateById(_ context.Context, _ string, model sharedentities.UserSession) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, nil
}

// newSessionHarness builds the service with a NIL cache. That exercises the row-only
// path deliberately: with no cache the service must fall back to the row's own view rather
// than declaring every session dead. The cache-reconciliation branch (a row whose cache
// entry has vanished) is covered by the live end-to-end run, where a real store exists.
func newSessionHarness(t *testing.T) (*sessionService, *fakeSessionRepo) {
	t.Helper()
	repo := &fakeSessionRepo{}
	return &sessionService{repo: repo}, repo
}

// A user's sessions must all come back. This is the regression the plan called out: the
// generic GetByForeign helper returns a single child row, so a per-user listing built on it
// would show one session no matter how many exist — and "sign out everywhere" would then
// appear to do nothing.
func TestListForUserReturnsEverySession(t *testing.T) {
	svc, repo := newSessionHarness(t)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		svc.Record(ctx, sharedentities.UserSession{
			SessionId:   string(rune('a'+i)) + "-session",
			UserLoginId: 7,
			ExpiresAt:   time.Now().Add(time.Hour).Unix(),
		})
	}
	// A session belonging to someone else must not leak into the list.
	svc.Record(ctx, sharedentities.UserSession{SessionId: "other", UserLoginId: 99})

	views, err := svc.ListForUser(ctx, 7, "")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(views) != 4 {
		t.Fatalf("got %d sessions, want 4 (all of user 7's)", len(views))
	}
	for _, v := range views {
		if v.UserId != 7 {
			t.Errorf("listing leaked a session belonging to user %d", v.UserId)
		}
	}
	_ = repo
}

// The current session must be identifiable, so the UI can label it and warn before
// someone signs themselves out.
func TestListForUserMarksTheCurrentSession(t *testing.T) {
	svc, _ := newSessionHarness(t)
	ctx := context.Background()
	svc.Record(ctx, sharedentities.UserSession{SessionId: "mine", UserLoginId: 3})
	svc.Record(ctx, sharedentities.UserSession{SessionId: "theirs", UserLoginId: 3})

	views, err := svc.ListForUser(ctx, 3, "mine")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	var current int
	for _, v := range views {
		if v.Current {
			current++
			if v.SessionId != "mine" {
				t.Errorf("wrong session marked current: %s", v.SessionId)
			}
		}
	}
	if current != 1 {
		t.Fatalf("expected exactly 1 current session, got %d", current)
	}
}

// A revoked or expired row must never be reported active — otherwise a dead session keeps
// appearing in a device list as though it were still usable.
func TestListForUserReportsRevokedAndExpiredAsInactive(t *testing.T) {
	svc, repo := newSessionHarness(t)
	ctx := context.Background()
	now := time.Now().Unix()

	svc.Record(ctx, sharedentities.UserSession{SessionId: "live", UserLoginId: 5, ExpiresAt: now + 3600})
	svc.Record(ctx, sharedentities.UserSession{SessionId: "expired", UserLoginId: 5, ExpiresAt: now - 10})
	svc.Record(ctx, sharedentities.UserSession{SessionId: "revoked", UserLoginId: 5, ExpiresAt: now + 3600})
	for _, row := range repo.rows {
		if row.SessionId == "revoked" {
			row.RevokedAt = now
			row.IsActive = false
		}
	}

	views, err := svc.ListForUser(ctx, 5, "")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	state := map[string]bool{}
	for _, v := range views {
		state[v.SessionId] = v.Active
	}
	if !state["live"] {
		t.Error("an unexpired, unrevoked session was reported inactive")
	}
	if state["expired"] {
		t.Error("an expired session was reported active")
	}
	if state["revoked"] {
		t.Error("a revoked session was reported active")
	}
}

func TestRevokeMarksTheRowAndIsIdempotent(t *testing.T) {
	svc, repo := newSessionHarness(t)
	ctx := context.Background()
	svc.Record(ctx, sharedentities.UserSession{SessionId: "doomed", UserLoginId: 11, ExpiresAt: time.Now().Add(time.Hour).Unix()})

	found, err := svc.Revoke(ctx, "doomed")
	if err != nil || !found {
		t.Fatalf("Revoke: found=%v err=%v", found, err)
	}
	if repo.rows[0].RevokedAt == 0 || repo.rows[0].IsActive {
		t.Fatal("row was not marked revoked")
	}

	// Revoking an unknown id must not error — an operator clicking twice, or racing
	// against an expiry, is not a failure.
	if found, err := svc.Revoke(ctx, "never-existed"); err != nil || found {
		t.Fatalf("revoking an unknown session: found=%v err=%v", found, err)
	}
}

// "Sign out everywhere else" must spare the caller's own session, or the person asking to
// evict their other devices would evict themselves too.
func TestRevokeAllForUserSparesTheExceptedSession(t *testing.T) {
	svc, _ := newSessionHarness(t)
	ctx := context.Background()
	future := time.Now().Add(time.Hour).Unix()
	for _, id := range []string{"keep", "drop-1", "drop-2"} {
		svc.Record(ctx, sharedentities.UserSession{SessionId: id, UserLoginId: 21, ExpiresAt: future})
	}

	count, err := svc.RevokeAllForUser(ctx, 21, "keep")
	if err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}
	if count != 2 {
		t.Fatalf("revoked %d sessions, want 2", count)
	}

	views, _ := svc.ListForUser(ctx, 21, "keep")
	for _, v := range views {
		if v.SessionId == "keep" && !v.Active {
			t.Error("the excepted session was revoked — the caller signed themselves out")
		}
		if v.SessionId != "keep" && v.Active {
			t.Errorf("session %s survived a revoke-all", v.SessionId)
		}
	}
}

// An administrator ending someone else's sessions passes no exception, so every one goes.
func TestRevokeAllForUserWithNoExceptionEndsEverything(t *testing.T) {
	svc, _ := newSessionHarness(t)
	ctx := context.Background()
	future := time.Now().Add(time.Hour).Unix()
	for _, id := range []string{"a", "b", "c"} {
		svc.Record(ctx, sharedentities.UserSession{SessionId: id, UserLoginId: 42, ExpiresAt: future})
	}

	count, err := svc.RevokeAllForUser(ctx, 42, "")
	if err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}
	if count != 3 {
		t.Fatalf("revoked %d, want 3", count)
	}
	views, _ := svc.ListForUser(ctx, 42, "")
	for _, v := range views {
		if v.Active {
			t.Errorf("session %s survived an administrative revoke-all", v.SessionId)
		}
	}
}
