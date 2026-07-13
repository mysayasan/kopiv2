package bootstrap

import (
	"context"
	"path/filepath"
	"testing"

	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// driftFixture declares Count as an int64.
type driftFixture struct {
	Id    int64  `pkey:"true" skipWhenInsert:"true"`
	Slug  string `validate:"required"`
	Count int64
}

func driftOptions(t *testing.T, dbPath string) Options {
	t.Helper()
	return Options{
		AppName: "drift-test",
		Config:  dbsql.DbConfigModel{Engine: "sqlite", DbName: dbPath},
		Bootstrap: BootstrapConfig{
			Enabled:            true,
			AutoCreateDatabase: true,
			AutoCreateSchema:   true,
			AutoMigrate:        true,
		},
		Entities: []any{driftFixture{}},
	}
}

// seedDriftDatabase creates the table with a DIFFERENT type for count than the entity
// declares, plus a bootstrap-state row so this reads as an upgrade rather than a fresh
// install.
func seedDriftDatabase(t *testing.T, dbPath string, createTable string) {
	t.Helper()
	ctx := context.Background()
	db := openSQLite(t, dbPath)

	if err := ensureBootstrapStateTable(ctx, db, "sqlite"); err != nil {
		t.Fatalf("state table: %v", err)
	}
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO bootstrap_schema_state (app_name, manifest_hash, manifest_json, updated_at) VALUES ('drift-test', 'legacy-hash', '{}', 1)`); err != nil {
		t.Fatalf("seed state: %v", err)
	}
}

// A changed column type used to be invisible: only column NAMES were ever compared, so the
// column kept its old SQL type and the row scanner failed at runtime, on a customer's box,
// with no clue why. It must now be reported.
func TestDrift_TypeChangeIsDetected(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "type.db")
	// count is TEXT in the database; the entity says int64.
	seedDriftDatabase(t, dbPath, `CREATE TABLE drift_fixture (id INTEGER PRIMARY KEY AUTOINCREMENT, slug TEXT, count TEXT)`)

	status, err := Ensure(ctx, driftOptions(t, dbPath))
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	found := false
	for _, d := range status.SchemaDrift {
		if d.Kind == DriftTypeChanged && d.Column == "count" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a changed column type was not detected: %+v", status.SchemaDrift)
	}
	// And it must be visible in the status message, not buried.
	if status.Message == "bootstrap complete" {
		t.Fatalf("drift was not surfaced in the status message: %q", status.Message)
	}
}

// A field removed or renamed in the entity leaves its column — with all its data — in the
// database. Silently, forever. Report it so somebody writes a drop or rename migration.
func TestDrift_ExtraColumnIsDetected(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "extra.db")
	seedDriftDatabase(t, dbPath, `CREATE TABLE drift_fixture (id INTEGER PRIMARY KEY AUTOINCREMENT, slug TEXT, count BIGINT, legacy_note TEXT)`)

	status, err := Ensure(ctx, driftOptions(t, dbPath))
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	found := false
	for _, d := range status.SchemaDrift {
		if d.Kind == DriftExtraColumn && d.Column == "legacy_note" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a column the entity no longer declares was not detected: %+v", status.SchemaDrift)
	}
}

// Drift must NOT be manufactured out of a schema that is simply correct — a false positive
// on every boot would train everyone to ignore the warning.
func TestDrift_MatchingSchemaReportsNone(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "clean.db")
	seedDriftDatabase(t, dbPath, `CREATE TABLE drift_fixture (id INTEGER PRIMARY KEY AUTOINCREMENT, slug TEXT, count BIGINT)`)

	status, err := Ensure(ctx, driftOptions(t, dbPath))
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(status.SchemaDrift) != 0 {
		t.Fatalf("a correct schema reported drift: %+v", status.SchemaDrift)
	}
}

// A column the entity declares but the database lacks is NOT drift — the additive
// auto-migrator adds it. Reporting it would be noise.
func TestDrift_MissingColumnIsNotDriftItIsAdded(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "missing.db")
	seedDriftDatabase(t, dbPath, `CREATE TABLE drift_fixture (id INTEGER PRIMARY KEY AUTOINCREMENT, slug TEXT)`)

	status, err := Ensure(ctx, driftOptions(t, dbPath))
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(status.SchemaDrift) != 0 {
		t.Fatalf("a column the auto-migrator adds was reported as drift: %+v", status.SchemaDrift)
	}

	db := openSQLite(t, dbPath)
	has := false
	for _, name := range columnNames(t, db, "drift_fixture") {
		if name == "count" {
			has = true
		}
	}
	if !has {
		t.Fatal("the auto-migrator did not add the missing column")
	}
}

// realisticFixture uses the Go types that DON'T survive a naive comparison: SQLite stores a
// BOOLEAN as INTEGER and a TIMESTAMPTZ as TEXT; MariaDB stores a BOOLEAN as TINYINT(1).
// Comparing the manifest's engine-neutral type against what is actually on disk would report
// drift on these every single boot — and a warning that always fires is a warning nobody
// reads. This is a regression test for exactly that false positive.
type realisticFixture struct {
	Id      int64 `pkey:"true" skipWhenInsert:"true"`
	Name    string
	Enabled bool
	Ratio   float64
	SeenAt  int64
}

func TestDrift_EngineTypeMappingsDoNotCryWolf(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "nowolf.db")

	db := openSQLite(t, dbPath)
	if err := ensureBootstrapStateTable(ctx, db, "sqlite"); err != nil {
		t.Fatalf("state table: %v", err)
	}
	// Exactly what the bootstrapper itself would create on SQLite: BOOLEAN -> INTEGER,
	// DOUBLE PRECISION -> REAL.
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE realistic_fixture (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, enabled INTEGER, ratio REAL, seen_at INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO bootstrap_schema_state (app_name, manifest_hash, manifest_json, updated_at) VALUES ('nowolf-test', 'legacy-hash', '{}', 1)`); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	status, err := Ensure(ctx, Options{
		AppName: "nowolf-test",
		Config:  dbsql.DbConfigModel{Engine: "sqlite", DbName: dbPath},
		Bootstrap: BootstrapConfig{
			Enabled:            true,
			AutoCreateDatabase: true,
			AutoCreateSchema:   true,
			AutoMigrate:        true,
		},
		Entities: []any{realisticFixture{}},
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(status.SchemaDrift) != 0 {
		t.Fatalf("engine type mappings were reported as drift — this would fire on every boot: %+v", status.SchemaDrift)
	}
}

// typeFamily is what keeps the drift check from crying wolf. Exact SQL type strings differ
// wildly across engines for the same Go type; families are what actually decide whether a
// row scan works.
func TestTypeFamily_NormalizesAcrossEngines(t *testing.T) {
	same := [][]string{
		{"BIGINT", "int8", "integer", "INT", "bigint", "tinyint(1)"},
		{"TEXT", "varchar(255)", "character varying", "CLOB", "text"},
		{"BOOLEAN", "bool", "boolean"},
		{"DOUBLE PRECISION", "real", "float8", "numeric(10,2)"},
		{"TIMESTAMPTZ", "timestamp with time zone", "datetime"},
	}
	for _, group := range same {
		want := typeFamily(group[0])
		for _, alias := range group[1:] {
			if got := typeFamily(alias); got != want {
				t.Fatalf("typeFamily(%q) = %q, want %q (same family as %q)", alias, got, want, group[0])
			}
		}
	}

	// And genuinely different families must NOT collapse together — that is the whole point.
	if typeFamily("TEXT") == typeFamily("BIGINT") {
		t.Fatal("text and int must be different families")
	}
	if typeFamily("BOOLEAN") == typeFamily("BIGINT") {
		t.Fatal("bool and int must be different families")
	}
}
