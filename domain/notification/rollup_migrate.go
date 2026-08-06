package notification

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateRollupSourceColumn upgrades an existing notification_rollup table for
// the per-source dimension: adds the source column (backfilled to ''), and drops
// the old ux_notification_rollup_slot unique index so the auto-migrator can
// recreate it WITH source included. Without the drop, the stale index — which
// does not know about source — rejects the first two rows that share a slot but
// differ by source, and the rollup maintainer errors forever.
//
// Shared here because BOTH apps that fold rollups (mymatasan and myseliasan)
// must run it; each registers it as a one-shot bootstrap migration. Idempotent
// and a no-op on fresh databases (no table yet — the auto-migrator creates the
// complete shape).
func MigrateRollupSourceColumn(ctx context.Context, tx *sql.Tx, engine string) error {
	exists, hasSource, err := rollupTableState(ctx, tx, engine)
	if err != nil {
		return err
	}
	if !exists {
		return nil // fresh install: auto-migrator creates table+index with source
	}
	if !hasSource {
		colType := "VARCHAR(255)"
		if engine == "sqlite" {
			colType = "TEXT"
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE notification_rollup ADD COLUMN source "+colType); err != nil {
			return fmt.Errorf("add notification_rollup.source: %w", err)
		}
	}
	// Backfill regardless (an ADD COLUMN leaves existing rows NULL, and the
	// non-pointer string field cannot scan a NULL).
	if _, err := tx.ExecContext(ctx, "UPDATE notification_rollup SET source = '' WHERE source IS NULL"); err != nil {
		return fmt.Errorf("backfill notification_rollup.source: %w", err)
	}
	return dropRollupSlotIndex(ctx, tx, engine)
}

// rollupTableState reports whether notification_rollup exists and already has
// the source column.
func rollupTableState(ctx context.Context, tx *sql.Tx, engine string) (exists, hasSource bool, err error) {
	var query string
	var args []any
	switch engine {
	case "postgres":
		query = "SELECT column_name FROM information_schema.columns WHERE table_name = $1"
		args = []any{"notification_rollup"}
	case "mariadb":
		query = "SELECT column_name FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ?"
		args = []any{"notification_rollup"}
	case "sqlite":
		query = "SELECT name FROM pragma_table_info('notification_rollup')"
	default:
		return false, false, fmt.Errorf("unsupported db engine %q", engine)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return false, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return exists, hasSource, err
		}
		exists = true
		if name == "source" || name == "SOURCE" || name == "Source" {
			hasSource = true
		}
	}
	return exists, hasSource, rows.Err()
}

func dropRollupSlotIndex(ctx context.Context, tx *sql.Tx, engine string) error {
	const index = "ux_notification_rollup_slot"
	switch engine {
	case "postgres":
		// On Postgres the unique key may exist as a CONSTRAINT that owns the
		// index (DROP INDEX then fails with 2BP01, live-bench verbatim) or as a
		// bare unique index, depending on how the table was bootstrapped. Drop
		// the constraint first — that removes its index too — then any bare one.
		if _, err := tx.ExecContext(ctx, "ALTER TABLE notification_rollup DROP CONSTRAINT IF EXISTS "+index); err != nil {
			return fmt.Errorf("drop constraint %s: %w", index, err)
		}
		if _, err := tx.ExecContext(ctx, "DROP INDEX IF EXISTS "+index); err != nil {
			return fmt.Errorf("drop %s: %w", index, err)
		}
	case "sqlite":
		if _, err := tx.ExecContext(ctx, "DROP INDEX IF EXISTS "+index); err != nil {
			return fmt.Errorf("drop %s: %w", index, err)
		}
	case "mariadb":
		// MariaDB's DROP INDEX has no IF EXISTS on every supported version; probe first.
		var n int
		err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'notification_rollup' AND index_name = ?",
			index).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			if _, err := tx.ExecContext(ctx, "ALTER TABLE notification_rollup DROP INDEX "+index); err != nil {
				return fmt.Errorf("drop %s: %w", index, err)
			}
		}
	default:
		return fmt.Errorf("unsupported db engine %q", engine)
	}
	return nil
}
