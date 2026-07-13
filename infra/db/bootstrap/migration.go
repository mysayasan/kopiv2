package bootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
)

// migrationTable records which migrations have been applied, per app.
const migrationTable = "schema_migration"

// Migration is one ordered, once-only schema change.
//
// It exists because the auto-migrator can only ADD COLUMN. That is enough while a schema
// only grows, and has no answer the first time it doesn't:
//
//   - a RENAME adds the new column and silently leaves the old one holding all the data
//   - a DROP leaves the column forever; a legacy NOT NULL one eventually breaks inserts
//   - a TYPE CHANGE is not even DETECTED — only column names were ever compared
//
// On a field appliance you cannot reach, there was no supported path for any of those.
// A migration is that path.
//
// Additive changes still need NO migration: add a field to the entity and the
// auto-migrator picks it up. Write one only for what additive cannot express.
type Migration struct {
	// ID is the immutable, sortable identity — "20260714-01-rename-camera-host".
	// Migrations run in ID order. NEVER change an ID once released: it is the key that
	// records the migration as applied.
	ID string
	// Name is human-readable text for the log.
	Name string
	// Statements are the SQL to run, keyed by engine. The "" key applies to every engine;
	// an engine-specific key ("sqlite" / "postgres" / "mariadb") replaces it for that
	// engine. The engines genuinely differ here — SQLite could not DROP COLUMN before
	// 3.35, and none of them agree on how to change a column's type — so a structural
	// migration usually has to say what it means per engine rather than pretend they are
	// the same database.
	Statements map[string][]string
	// Exec, when set, runs INSTEAD of Statements — for a change SQL alone cannot express
	// (reading rows, transforming them, writing them back).
	Exec func(ctx context.Context, tx *sql.Tx, engine string) error
}

// statementsFor returns the statements for an engine: the engine-specific set if present,
// otherwise the engine-neutral one.
func (m Migration) statementsFor(engine string) []string {
	if stmts, ok := m.Statements[engine]; ok {
		return stmts
	}
	return m.Statements[""]
}

// checksum fingerprints what the migration will actually do, so an already-applied
// migration that is later EDITED can be caught. Two databases that both claim to have
// applied "20260714-01" must have had the same thing done to them; if the code says
// otherwise, the schema on disk is not what the developer thinks it is.
//
// A migration with an Exec function cannot be fingerprinted (a Go closure has no stable
// hash), so it is recorded with an empty checksum and skipped by the tamper check.
func (m Migration) checksum(engine string) string {
	if m.Exec != nil {
		return ""
	}
	h := sha256.New()
	for _, stmt := range m.statementsFor(engine) {
		h.Write([]byte(strings.TrimSpace(stmt)))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// validateMigrations rejects a migration set that cannot be applied deterministically.
// Better to fail at startup than to apply migrations in an order that depends on map
// iteration or to silently skip a duplicate.
func validateMigrations(migrations []Migration) error {
	seen := map[string]bool{}
	for _, m := range migrations {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			return fmt.Errorf("migration with empty ID (name %q)", m.Name)
		}
		if seen[id] {
			return fmt.Errorf("duplicate migration ID %q", id)
		}
		seen[id] = true
		if m.Exec == nil && len(m.Statements) == 0 {
			return fmt.Errorf("migration %q has neither Statements nor Exec", id)
		}
	}
	return nil
}

// sortMigrations orders by ID. IDs are date-prefixed, so lexical order is chronological.
func sortMigrations(migrations []Migration) []Migration {
	out := make([]Migration, len(migrations))
	copy(out, migrations)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func ensureMigrationTable(ctx context.Context, db *sql.DB, engine string) error {
	var stmt string
	switch engine {
	case "postgres":
		stmt = fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	app_name TEXT NOT NULL,
	id TEXT NOT NULL,
	name TEXT NOT NULL,
	checksum TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (app_name, id)
)
`, quoteIdent(migrationTable, engine))
	case "mariadb":
		stmt = fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	app_name VARCHAR(190) NOT NULL,
	id VARCHAR(190) NOT NULL,
	name TEXT NOT NULL,
	checksum VARCHAR(64) NOT NULL,
	applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (app_name, id)
)
`, quoteIdent(migrationTable, engine))
	case "sqlite":
		stmt = fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	app_name TEXT NOT NULL,
	id TEXT NOT NULL,
	name TEXT NOT NULL,
	checksum TEXT NOT NULL,
	applied_at INTEGER NOT NULL,
	PRIMARY KEY (app_name, id)
)
`, quoteIdent(migrationTable, engine))
	default:
		return fmt.Errorf("unsupported db engine %q", engine)
	}

	_, err := db.ExecContext(ctx, stmt)
	return err
}

// appliedMigrations returns the recorded id -> checksum for this app.
func appliedMigrations(ctx context.Context, db *sql.DB, engine string, appName string) (map[string]string, error) {
	query := fmt.Sprintf(`SELECT id, checksum FROM %s WHERE app_name = ?`, quoteIdent(migrationTable, engine))
	if engine == "postgres" {
		query = fmt.Sprintf(`SELECT id, checksum FROM %s WHERE app_name = $1`, quoteIdent(migrationTable, engine))
	}

	rows, err := db.QueryContext(ctx, query, appName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := map[string]string{}
	for rows.Next() {
		var id, checksum string
		if err := rows.Scan(&id, &checksum); err != nil {
			return nil, err
		}
		applied[id] = checksum
	}
	return applied, rows.Err()
}

// recordMigration marks a migration applied. tx may be nil, in which case it is written
// directly (MariaDB auto-commits DDL, so a transaction there is a comforting fiction).
func recordMigration(ctx context.Context, exec sqlExecutor, engine string, appName string, m Migration, now int64) error {
	query := fmt.Sprintf(`INSERT INTO %s (app_name, id, name, checksum, applied_at) VALUES (?, ?, ?, ?, ?)`, quoteIdent(migrationTable, engine))
	args := []any{appName, m.ID, m.Name, m.checksum(engine), now}
	switch engine {
	case "postgres":
		query = fmt.Sprintf(`INSERT INTO %s (app_name, id, name, checksum) VALUES ($1, $2, $3, $4)`, quoteIdent(migrationTable, engine))
		args = args[:4]
	case "mariadb":
		query = fmt.Sprintf(`INSERT INTO %s (app_name, id, name, checksum) VALUES (?, ?, ?, ?)`, quoteIdent(migrationTable, engine))
		args = args[:4]
	}
	_, err := exec.ExecContext(ctx, query, args...)
	return err
}

// sqlExecutor is the bit of *sql.DB and *sql.Tx we need, so a migration can be recorded
// inside its own transaction where the engine supports one.
type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// baselineMigrations records every migration as applied WITHOUT running it.
//
// This is what a fresh install does. The schema was just created directly from the current
// entities, so it is already at the latest shape: replaying a rename against a column that
// was never named the old thing would fail, and replaying a drop against a column that was
// never created would too. The migrations describe how to get an OLD database to the
// current shape. A new one is already there.
func baselineMigrations(ctx context.Context, db *sql.DB, engine string, appName string, migrations []Migration, now int64) error {
	for _, m := range migrations {
		if err := recordMigration(ctx, db, engine, appName, m, now); err != nil {
			return fmt.Errorf("baseline migration %s: %w", m.ID, err)
		}
	}
	if len(migrations) > 0 {
		log.Printf("bootstrap %s: fresh database — baselined %d migration(s) as already applied", appName, len(migrations))
	}
	return nil
}

// runMigrations applies every pending migration in ID order.
//
// It runs BEFORE the additive auto-migrator, which is the only order that works: a rename
// must happen while the column still has its old name. If auto-migrate went first it would
// see the entity's NEW field, ADD it as an empty column, and then the rename would fail
// because the target already exists — and the data would still be stranded in the old one.
//
// The full pipeline is therefore:
//
//	migrations (structural)  ->  auto-migrate (additive)  ->  seeders (data, idempotent)
func runMigrations(ctx context.Context, db *sql.DB, engine string, appName string, migrations []Migration, now int64) ([]string, error) {
	applied, err := appliedMigrations(ctx, db, engine, appName)
	if err != nil {
		return nil, err
	}

	ran := []string{}
	for _, m := range migrations {
		if recorded, ok := applied[m.ID]; ok {
			// Already applied. Verify it has not been edited since — two databases that both
			// claim "20260714-01" must have had the same thing done to them.
			if want := m.checksum(engine); want != "" && recorded != "" && recorded != want {
				return nil, fmt.Errorf("migration %s was modified after it was applied (checksum %s, expected %s); "+
					"never edit a released migration — add a new one", m.ID, recorded, want)
			}
			continue
		}

		if err := applyMigration(ctx, db, engine, appName, m, now); err != nil {
			return nil, fmt.Errorf("migration %s (%s): %w", m.ID, m.Name, err)
		}
		log.Printf("bootstrap %s: applied migration %s (%s)", appName, m.ID, m.Name)
		ran = append(ran, m.ID)
	}
	return ran, nil
}

// applyMigration runs one migration and records it.
//
// Where the engine supports transactional DDL (SQLite, Postgres) the statements and the
// record land together, so a failure leaves nothing half-done. MariaDB implicitly commits
// each DDL statement, so a migration there CANNOT be atomic — write MariaDB statements
// defensively (IF EXISTS / IF NOT EXISTS), because a failure part-way through leaves the
// earlier statements applied and the migration unrecorded, so it will be retried.
func applyMigration(ctx context.Context, db *sql.DB, engine string, appName string, m Migration, now int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if m.Exec != nil {
		if err := m.Exec(ctx, tx, engine); err != nil {
			return err
		}
	} else {
		stmts := m.statementsFor(engine)
		if len(stmts) == 0 {
			// Nothing to do on this engine. That is legitimate — a SQLite-only fix, say —
			// but it still gets recorded, so it is not retried on every boot.
			log.Printf("bootstrap %s: migration %s has no statements for engine %s — recording as applied", appName, m.ID, engine)
		}
		for _, stmt := range stmts {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("statement %q: %w", strings.TrimSpace(stmt), err)
			}
		}
	}

	if err := recordMigration(ctx, tx, engine, appName, m, now); err != nil {
		return err
	}
	return tx.Commit()
}
