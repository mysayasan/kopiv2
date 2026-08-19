package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/db/sql/sqlite"
)

// The fake repo in audit_retention_test.go is my model of the data layer, and the two things
// retention leans on hardest are exactly the things a hand-written fake cannot vouch for: a
// LessThan filter on CreatedAt selecting the right rows, and a filtered Delete removing
// those rows and only those rows. This runs the real purge against a real sqlite file.
func TestPurgeAgainstRealSqlite(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "data", "audit.db")
	opts := bootstrap.Options{
		AppName: "myidsan-audit-retention-test",
		Config:  dbsql.DbConfigModel{Engine: "sqlite", DbName: dbPath},
		Bootstrap: bootstrap.BootstrapConfig{
			Enabled:            true,
			AutoCreateDatabase: true,
			AutoCreateSchema:   true,
			AutoMigrate:        true,
		},
		Entities: []any{AuditLog{}},
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
	repo := dbsql.NewGenericRepo[AuditLog](crud)
	svc := NewService(repo, nil)

	now := time.Now().UTC()
	// Three rows well outside a 30-day window, two comfortably inside it, and one just
	// inside the boundary — the off-by-one that would quietly eat a recent event.
	seed := []struct {
		email string
		at    time.Time
	}{
		{"ancient1@example.test", now.AddDate(0, 0, -400)},
		{"ancient2@example.test", now.AddDate(0, 0, -365)},
		{"ancient3@example.test", now.AddDate(0, 0, -31)},
		{"boundary@example.test", now.AddDate(0, 0, -29)},
		{"recent1@example.test", now.AddDate(0, 0, -1)},
		{"recent2@example.test", now},
	}
	for _, s := range seed {
		if _, err := repo.Create(ctx, "", AuditLog{
			Action:     "login.success",
			ActorEmail: s.email,
			Outcome:    OutcomeSuccess,
			TargetType: "user",
			CreatedAt:  s.at.Unix(),
		}); err != nil {
			t.Fatalf("seed %s: %v", s.email, err)
		}
	}

	archiveDir := filepath.Join(t.TempDir(), "archive")
	res, err := svc.PurgeOlderThan(ctx, 30, archiveDir)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if res.Archived != 3 || res.Deleted != 3 {
		t.Fatalf("archived=%d deleted=%d want 3/3 — the CreatedAt cutoff did not select what it should", res.Archived, res.Deleted)
	}

	archived := readArchive(t, res.ArchivePath)
	if len(archived) != 3 {
		t.Fatalf("archive holds %d rows want 3", len(archived))
	}
	// Round-tripping through real sqlite column types must not blank the payload.
	if archived[0].ActorEmail != "ancient1@example.test" || archived[0].CreatedAt == 0 {
		t.Fatalf("archived row came back empty or reordered: %+v", archived[0])
	}

	// What survives: the three in-window rows plus the purge's record of itself.
	rows, total, err := repo.Get(ctx, "", 100, 0, nil, []sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}})
	if err != nil {
		t.Fatalf("post-purge read: %v", err)
	}
	if total != 4 {
		t.Fatalf("table holds %d rows want 4 (3 in-window + 1 purge record)", total)
	}
	survivors := map[string]bool{}
	var purgeRecords int
	for _, row := range rows {
		if row.Action == ActionAuditPurge {
			purgeRecords++
			continue
		}
		survivors[row.ActorEmail] = true
	}
	if purgeRecords != 1 {
		t.Fatalf("purge self-records = %d want 1", purgeRecords)
	}
	for _, want := range []string{"boundary@example.test", "recent1@example.test", "recent2@example.test"} {
		if !survivors[want] {
			t.Fatalf("%s was inside the retention window but was deleted", want)
		}
	}
	if len(survivors) != 3 {
		t.Fatalf("unexpected survivors: %v", survivors)
	}

	// A second run with nothing newly expired must be a clean no-op rather than
	// re-archiving or, worse, deleting the record it just wrote about itself.
	second, err := svc.PurgeOlderThan(ctx, 30, archiveDir)
	if err != nil {
		t.Fatalf("second PurgeOlderThan: %v", err)
	}
	if second.Archived != 0 || second.Deleted != 0 {
		t.Fatalf("second run was not a no-op: %+v", second)
	}
}
