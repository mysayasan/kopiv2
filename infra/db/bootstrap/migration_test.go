package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// migrationFixture is the CURRENT shape of the entity. The tests below simulate a database
// that was created from an OLDER shape and must be brought forward.
type migrationFixture struct {
	Id    int64  `pkey:"true" skipWhenInsert:"true"`
	Slug  string `validate:"required"`
	Count int64
}

func sqliteOptions(t *testing.T, dbPath string, migrations []Migration) Options {
	t.Helper()
	return Options{
		AppName: "migration-test",
		Config:  dbsql.DbConfigModel{Engine: "sqlite", DbName: dbPath},
		Bootstrap: BootstrapConfig{
			Enabled:            true,
			AutoCreateDatabase: true,
			AutoCreateSchema:   true,
			AutoMigrate:        true,
		},
		Entities:   []any{migrationFixture{}},
		Migrations: migrations,
	}
}

// openSQLite opens the database for assertions. The handle is closed at the end of the
// test — on Windows sqlite cannot delete a file that is still open, which a factory-reset
// test would otherwise trip over.
func openSQLite(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := openTargetDB(dbsql.DbConfigModel{Engine: "sqlite", DbName: dbPath}, "sqlite")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// withSQLite opens the database, runs fn, and closes it immediately — for setup that must
// not leave a handle open (see openSQLite).
func withSQLite(t *testing.T, dbPath string, fn func(db *sql.DB)) {
	t.Helper()
	db, err := openTargetDB(dbsql.DbConfigModel{Engine: "sqlite", DbName: dbPath}, "sqlite")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	fn(db)
}

// seedLegacyDatabase builds a database as an OLD version of the app would have left it:
// the table has the old column name, real data in it, and a bootstrap-state row (so the
// bootstrapper knows this is an upgrade, not a fresh install).
func seedLegacyDatabase(t *testing.T, dbPath string) {
	t.Helper()
	ctx := context.Background()
	withSQLite(t, dbPath, func(db *sql.DB) {
		if err := ensureBootstrapStateTable(ctx, db, "sqlite"); err != nil {
			t.Fatalf("state table: %v", err)
		}
		// The old shape: "code", not "slug".
		if _, err := db.ExecContext(ctx, `CREATE TABLE migration_fixture (id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT, count BIGINT)`); err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO migration_fixture (code, count) VALUES ('alpha', 7), ('beta', 9)`); err != nil {
			t.Fatalf("seed rows: %v", err)
		}
		// A previously-recorded manifest is what marks the database as NOT fresh.
		if _, err := db.ExecContext(ctx,
			`INSERT INTO bootstrap_schema_state (app_name, manifest_hash, manifest_json, updated_at) VALUES ('migration-test', 'legacy-hash', '{}', 1)`); err != nil {
			t.Fatalf("seed state: %v", err)
		}
	})
}

func renameCodeToSlug() Migration {
	return Migration{
		ID:   "20260714-01-rename-code-to-slug",
		Name: "rename migration_fixture.code to slug",
		Statements: map[string][]string{
			"":         {`ALTER TABLE migration_fixture RENAME COLUMN code TO slug`},
			"mariadb":  {`ALTER TABLE migration_fixture CHANGE code slug TEXT`},
			"postgres": {`ALTER TABLE migration_fixture RENAME COLUMN code TO slug`},
		},
	}
}

func columnNames(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var cid, notNull, pk int
		var name, dataType string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &def, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, name)
	}
	return names
}

// THE headline. Before this, a rename was impossible: the auto-migrator would ADD an empty
// "slug" column and leave every row's data stranded in "code" — silently, with no error.
// A migration must carry the data across.
func TestMigration_RenamePreservesData(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rename.db")
	seedLegacyDatabase(t, dbPath)

	status, err := Ensure(ctx, sqliteOptions(t, dbPath, []Migration{renameCodeToSlug()}))
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(status.MigrationsApplied) != 1 || status.MigrationsApplied[0] != "20260714-01-rename-code-to-slug" {
		t.Fatalf("migration was not applied: %+v", status.MigrationsApplied)
	}

	db := openSQLite(t, dbPath)

	// The data must have MOVED, not been abandoned in the old column.
	var slug string
	var count int64
	if err := db.QueryRowContext(ctx, `SELECT slug, count FROM migration_fixture WHERE count = 7`).Scan(&slug, &count); err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if slug != "alpha" {
		t.Fatalf("slug = %q, want %q — the rename did not carry the data", slug, "alpha")
	}

	// And the old column must be gone — otherwise the auto-migrator ran first and simply
	// added an empty duplicate.
	for _, name := range columnNames(t, db, "migration_fixture") {
		if name == "code" {
			t.Fatal("old column 'code' still exists — auto-migrate ran before the migration")
		}
	}
}

// A fresh database is ALREADY at the current shape (auto-create builds it from the current
// entities), so replaying a rename against a column that never had the old name would just
// fail. Fresh installs baseline: record the migrations, run none of them.
func TestMigration_FreshDatabaseIsBaselinedNotReplayed(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fresh.db")

	// No legacy table, no state row: a genuinely fresh install. The rename migration would
	// FAIL here (there is no "code" column to rename) if it were executed.
	status, err := Ensure(ctx, sqliteOptions(t, dbPath, []Migration{renameCodeToSlug()}))
	if err != nil {
		t.Fatalf("a fresh database must baseline, not replay: %v", err)
	}
	if len(status.MigrationsApplied) != 0 {
		t.Fatalf("fresh database ran migrations instead of baselining: %+v", status.MigrationsApplied)
	}

	// It must still be RECORDED, or it would be retried (and fail) on the next boot.
	db := openSQLite(t, dbPath)
	applied, err := appliedMigrations(ctx, db, "sqlite", "migration-test")
	if err != nil {
		t.Fatalf("appliedMigrations: %v", err)
	}
	if _, ok := applied["20260714-01-rename-code-to-slug"]; !ok {
		t.Fatal("fresh database did not baseline the migration — it would be retried and fail on next boot")
	}
}

// Migrations are once-only. A second boot must not re-run them.
func TestMigration_AppliedMigrationIsNotRerun(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "once.db")
	seedLegacyDatabase(t, dbPath)

	if _, err := Ensure(ctx, sqliteOptions(t, dbPath, []Migration{renameCodeToSlug()})); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	// The second run would fail outright if it re-ran the rename ("code" no longer exists).
	status, err := Ensure(ctx, sqliteOptions(t, dbPath, []Migration{renameCodeToSlug()}))
	if err != nil {
		t.Fatalf("second Ensure re-ran the migration: %v", err)
	}
	if len(status.MigrationsApplied) != 0 {
		t.Fatalf("migration was re-applied: %+v", status.MigrationsApplied)
	}
}

// Two databases that both claim to have applied "20260714-01" must have had the SAME thing
// done to them. Editing a released migration means the schema on disk is not what the
// developer thinks it is — fail loudly rather than carry on.
func TestMigration_EditingAnAppliedMigrationIsRejected(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tamper.db")
	seedLegacyDatabase(t, dbPath)

	if _, err := Ensure(ctx, sqliteOptions(t, dbPath, []Migration{renameCodeToSlug()})); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}

	// Same ID, different SQL — someone edited a released migration.
	edited := renameCodeToSlug()
	edited.Statements[""] = []string{`ALTER TABLE migration_fixture RENAME COLUMN count TO total`}

	_, err := Ensure(ctx, sqliteOptions(t, dbPath, []Migration{edited}))
	if err == nil {
		t.Fatal("editing an already-applied migration must be rejected")
	}
	if !strings.Contains(err.Error(), "modified after it was applied") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Migrations run in ID order, not declaration order — IDs are date-prefixed, so lexical
// order is chronological. Getting this wrong applies a rename before the table it renames.
func TestMigration_RunInIDOrderNotDeclarationOrder(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "order.db")
	seedLegacyDatabase(t, dbPath)

	second := Migration{
		ID:         "20260714-02-add-note",
		Name:       "add note",
		Statements: map[string][]string{"": {`ALTER TABLE migration_fixture ADD COLUMN note TEXT`}},
	}
	// Declared out of order on purpose: 02 before 01.
	status, err := Ensure(ctx, sqliteOptions(t, dbPath, []Migration{second, renameCodeToSlug()}))
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	want := []string{"20260714-01-rename-code-to-slug", "20260714-02-add-note"}
	if len(status.MigrationsApplied) != 2 ||
		status.MigrationsApplied[0] != want[0] || status.MigrationsApplied[1] != want[1] {
		t.Fatalf("applied in %v, want %v", status.MigrationsApplied, want)
	}
}

// A migration set that cannot be applied deterministically must be rejected at startup, not
// silently mis-applied.
func TestMigration_InvalidSetsAreRejected(t *testing.T) {
	cases := []struct {
		name string
		set  []Migration
	}{
		{"empty ID", []Migration{{Name: "x", Statements: map[string][]string{"": {"SELECT 1"}}}}},
		{"duplicate ID", []Migration{
			{ID: "a", Statements: map[string][]string{"": {"SELECT 1"}}},
			{ID: "a", Statements: map[string][]string{"": {"SELECT 2"}}},
		}},
		{"no statements and no exec", []Migration{{ID: "a", Name: "empty"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateMigrations(tc.set); err == nil {
				t.Fatal("expected the migration set to be rejected")
			}
		})
	}
}

// A migration that SQL cannot express (read rows, transform, write back) runs as Go.
func TestMigration_ExecRunsArbitraryGo(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "exec.db")
	seedLegacyDatabase(t, dbPath)

	transform := Migration{
		ID:   "20260714-00-double-counts",
		Name: "double every count",
		Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
			_, err := tx.ExecContext(ctx, `UPDATE migration_fixture SET count = count * 2`)
			return err
		},
	}

	if _, err := Ensure(ctx, sqliteOptions(t, dbPath, []Migration{transform, renameCodeToSlug()})); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	db := openSQLite(t, dbPath)
	var count int64
	if err := db.QueryRowContext(ctx, `SELECT count FROM migration_fixture WHERE slug = 'alpha'`).Scan(&count); err != nil {
		t.Fatalf("read: %v", err)
	}
	if count != 14 {
		t.Fatalf("count = %d, want 14 — the Exec migration did not run", count)
	}
}

// A factory reset drops and rebuilds the database. The rebuilt one is FRESH, so it must
// baseline the migrations — and the NEXT normal boot must not then replay them.
//
// This is the trap: the rebuild saves a manifest (so the next boot reads as "not fresh"),
// and if the reset had not baselined, every migration would be replayed against a
// brand-new schema and fail. A factory reset would leave the app unable to start.
func TestMigration_ResetBaselinesSoTheNextBootDoesNotReplay(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "reset.db")
	seedLegacyDatabase(t, dbPath)

	opts := sqliteOptions(t, dbPath, []Migration{renameCodeToSlug()})
	opts.Bootstrap.AllowReset = true

	// Normal boot: the migration runs.
	if _, err := Ensure(ctx, opts); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}

	// Factory reset: drop everything and rebuild from the current entities.
	if _, err := Reset(ctx, opts); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	// The next boot must NOT replay the rename — there is no "code" column to rename in the
	// rebuilt schema, so it would fail outright.
	status, err := Ensure(ctx, opts)
	if err != nil {
		t.Fatalf("boot after reset replayed the migration: %v", err)
	}
	if len(status.MigrationsApplied) != 0 {
		t.Fatalf("boot after reset re-applied migrations: %+v", status.MigrationsApplied)
	}
}

// A failed migration must leave NOTHING behind on an engine with transactional DDL: not the
// half-applied schema change, and not a record claiming it succeeded.
func TestMigration_FailureIsAtomicAndNotRecorded(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fail.db")
	seedLegacyDatabase(t, dbPath)

	broken := Migration{
		ID:   "20260714-01-broken",
		Name: "adds a column then fails",
		Statements: map[string][]string{"": {
			`ALTER TABLE migration_fixture ADD COLUMN half_done TEXT`,
			`THIS IS NOT SQL`,
		}},
	}

	if _, err := Ensure(ctx, sqliteOptions(t, dbPath, []Migration{broken})); err == nil {
		t.Fatal("a broken migration must fail the bootstrap")
	}

	db := openSQLite(t, dbPath)
	for _, name := range columnNames(t, db, "migration_fixture") {
		if name == "half_done" {
			t.Fatal("the failed migration left a half-applied schema change behind")
		}
	}
	applied, err := appliedMigrations(ctx, db, "sqlite", "migration-test")
	if err != nil {
		t.Fatalf("appliedMigrations: %v", err)
	}
	if _, ok := applied["20260714-01-broken"]; ok {
		t.Fatal("a failed migration was recorded as applied")
	}
}
