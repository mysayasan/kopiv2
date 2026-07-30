package services

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/db/sql/sqlite"
)

// CountActive filters on a BOOLEAN column, and the query builder renders bool filter values
// as quoted literals ('true'/'false'). Whether that matches what the engine actually stored
// is not something a hand-written fake can answer — a fake compares Go values, so it agrees
// with whatever I assumed. If the engine stores booleans as 0/1, the filter silently matches
// nothing and the gauge reads a permanent zero, which looks exactly like "no sessions".
//
// So this runs the real count against a real sqlite file.
func TestCountActiveAgainstRealSqlite(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "data", "sessions.db")
	opts := bootstrap.Options{
		AppName: "myidsan-session-count-test",
		Config:  dbsql.DbConfigModel{Engine: "sqlite", DbName: dbPath},
		Bootstrap: bootstrap.BootstrapConfig{
			Enabled:            true,
			AutoCreateDatabase: true,
			AutoCreateSchema:   true,
			AutoMigrate:        true,
		},
		Entities: []any{sharedentities.UserSession{}},
	}
	if _, err := bootstrap.Ensure(ctx, opts); err != nil {
		t.Fatalf("bootstrap.Ensure: %v", err)
	}

	crud, err := sqlite.NewDbCrud(opts.Config)
	if err != nil {
		t.Fatalf("NewDbCrud: %v", err)
	}
	if closer, ok := crud.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	repo := dbsql.NewGenericRepo[sharedentities.UserSession](crud)
	svc := NewSessionService(repo, cache.NewMemoryStore(time.Minute, time.Minute), nil)

	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		if _, err := repo.Create(ctx, "", sharedentities.UserSession{
			UserLoginId: int64(i + 1),
			SessionId:   fmt.Sprintf("live-%d", i),
			IsActive:    true,
			CreatedAt:   now,
		}); err != nil {
			t.Fatalf("seed live session: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := repo.Create(ctx, "", sharedentities.UserSession{
			UserLoginId: int64(100 + i),
			SessionId:   fmt.Sprintf("dead-%d", i),
			IsActive:    false,
			RevokedAt:   now,
			CreatedAt:   now,
		}); err != nil {
			t.Fatalf("seed revoked session: %v", err)
		}
	}

	got, err := svc.CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive: %v", err)
	}
	if got != 3 {
		t.Fatalf("CountActive = %d want 3 — a zero here would mean the boolean filter does not match what the engine stored, and the gauge would read a permanent zero that looks like 'no sessions'", got)
	}

	// Revoking one must move the count, or the gauge cannot confirm a revocation took
	// effect — which is one of the two things it exists for.
	if _, err := svc.Revoke(ctx, "live-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, err = svc.CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive after revoke: %v", err)
	}
	if got != 2 {
		t.Fatalf("CountActive = %d want 2 after revoking one session", got)
	}
}
