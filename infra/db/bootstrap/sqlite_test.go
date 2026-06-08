package bootstrap

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type localBootstrapEntity struct {
	Id      int64  `pkey:"true" skipWhenInsert:"true"`
	Code    string `validate:"required" ukey:"code"`
	Enabled bool
	Count   int64
}

func TestEnsureSQLiteCreatesDatabaseAndSchema(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "data", "bootstrap.db")
	opts := Options{
		AppName: "sqlite-bootstrap-test",
		Config: dbsql.DbConfigModel{
			Engine: "sqlite",
			DbName: dbPath,
		},
		Bootstrap: BootstrapConfig{
			Enabled:            true,
			AutoCreateDatabase: true,
			AutoCreateSchema:   true,
			AutoMigrate:        true,
		},
		Entities: []any{localBootstrapEntity{}},
	}

	status, err := Ensure(ctx, opts)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if !status.Ready || !status.DatabaseCreated || !status.SchemaCreated || !status.SchemaUpdated {
		t.Fatalf("Ensure() status = %+v", status)
	}

	status, err = Ensure(ctx, opts)
	if err != nil {
		t.Fatalf("Ensure() second run error = %v", err)
	}
	if !status.Ready || status.DatabaseCreated || status.SchemaCreated {
		t.Fatalf("Ensure() second status = %+v", status)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	var tableCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'local_bootstrap_entity'`).Scan(&tableCount); err != nil {
		t.Fatalf("table lookup error = %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("local_bootstrap_entity table count = %d", tableCount)
	}

	var indexCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'ux_local_bootstrap_entity_code'`).Scan(&indexCount); err != nil {
		t.Fatalf("index lookup error = %v", err)
	}
	if indexCount != 1 {
		t.Fatalf("unique index count = %d", indexCount)
	}

	var stateCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bootstrap_schema_state WHERE app_name = ?`, opts.AppName).Scan(&stateCount); err != nil {
		t.Fatalf("state lookup error = %v", err)
	}
	if stateCount != 1 {
		t.Fatalf("bootstrap state count = %d", stateCount)
	}
}
