package bootstrap

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

func TestResetSQLiteDropsAndRebuilds(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "data", "bootstrap.db")
	opts := Options{
		AppName: "sqlite-reset-test",
		Config: dbsql.DbConfigModel{
			Engine: "sqlite",
			DbName: dbPath,
		},
		Bootstrap: BootstrapConfig{
			Enabled:            true,
			AutoCreateDatabase: true,
			AutoCreateSchema:   true,
			AutoMigrate:        true,
			AllowReset:         true,
		},
		Entities: []any{localBootstrapEntity{}},
	}

	if _, err := Ensure(ctx, opts); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	// Write a row so we can prove the reset wiped user data.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO local_bootstrap_entity (code, enabled, count) VALUES ('keep-me', 1, 7)`); err != nil {
		t.Fatalf("insert error = %v", err)
	}
	db.Close()

	status, err := Reset(ctx, opts)
	if err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if !status.Ready || !status.DatabaseCreated || !status.SchemaCreated {
		t.Fatalf("Reset() status = %+v", status)
	}

	// The schema exists again, but the row is gone — a true wipe + rebuild.
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() after reset error = %v", err)
	}
	defer db.Close()

	var rows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM local_bootstrap_entity`).Scan(&rows); err != nil {
		t.Fatalf("count after reset error = %v", err)
	}
	if rows != 0 {
		t.Fatalf("expected 0 rows after reset, got %d", rows)
	}
}

func TestResetDisabledWithoutAllowReset(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "data", "bootstrap.db")
	opts := Options{
		AppName: "sqlite-reset-guard-test",
		Config:  dbsql.DbConfigModel{Engine: "sqlite", DbName: dbPath},
		Bootstrap: BootstrapConfig{
			Enabled: true, AutoCreateDatabase: true, AutoCreateSchema: true, AutoMigrate: true,
			AllowReset: false,
		},
		Entities: []any{localBootstrapEntity{}},
	}
	if _, err := Ensure(ctx, opts); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if _, err := Reset(ctx, opts); err == nil {
		t.Fatal("Reset() should fail when AllowReset is false")
	}
	// The database file must still exist (reset was refused, not partially applied).
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("db file should still exist after refused reset: %v", err)
	}
}
