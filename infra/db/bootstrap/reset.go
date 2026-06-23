package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"

	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Reset performs a destructive factory reset of the database described by opts: it
// drops the whole database (honouring the configured engine and name), then re-runs
// Ensure to recreate the schema and re-seed stock data from scratch.
//
// It requires opts.Bootstrap.AllowReset to be true, so a stock deployment cannot be
// wiped by accident. The caller MUST stop everything holding a connection or file
// handle to the database first (recorders, monitors, repos) and restart the app
// afterwards — the live connection pool is invalid once the database is dropped.
func Reset(ctx context.Context, opts Options) (*Status, error) {
	if !opts.Bootstrap.AllowReset {
		return nil, fmt.Errorf("reset is disabled: set bootstrap.allowReset = true to enable")
	}
	engine := normalizeDbEngine(opts.Config.Engine)
	if engine != "postgres" && engine != "mariadb" && engine != "sqlite" {
		return nil, fmt.Errorf("reset supports postgres, mariadb, and sqlite, got %q", engine)
	}

	if err := dropDatabase(ctx, opts.Config, engine); err != nil {
		return nil, fmt.Errorf("drop database %q: %w", opts.Config.DbName, err)
	}

	// Recreate and seed from a clean slate. Force the provisioning flags on so a
	// reset always rebuilds, even if the running config had auto-create disabled.
	resetOpts := opts
	resetOpts.Bootstrap.Enabled = true
	resetOpts.Bootstrap.AutoCreateDatabase = true
	resetOpts.Bootstrap.AutoCreateSchema = true
	resetOpts.Bootstrap.AutoMigrate = true
	resetOpts.Bootstrap.AutoSeed = true
	return Ensure(ctx, resetOpts)
}

// dropDatabase removes the configured database. For sqlite it deletes the database
// file and its WAL/SHM/journal sidecars; for postgres/mariadb it connects to the
// maintenance database and issues DROP DATABASE (terminating other sessions first
// on postgres so the drop isn't blocked).
func dropDatabase(ctx context.Context, cfg dbsql.DbConfigModel, engine string) error {
	if engine == "sqlite" {
		name := strings.TrimSpace(cfg.DbName)
		if name == "" || name == ":memory:" {
			return nil
		}
		for _, p := range []string{name, name + "-wal", name + "-shm", name + "-journal"} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}

	adminDB, err := openMaintenanceDB(cfg, engine)
	if err != nil {
		return err
	}
	defer adminDB.Close()

	ident := quoteIdent(cfg.DbName, engine)
	switch engine {
	case "postgres":
		// This is a deliberate, irreversible security wipe — it must not be blocked
		// by anything still holding the database. Terminate other sessions first
		// (best-effort; a session may vanish between the catalog read and the kill,
		// so ignore errors), then DROP ... WITH (FORCE) (PG13+), which itself boots
		// any stragglers. Fall back to a plain DROP for older servers that reject the
		// option.
		terminate := func() {
			_, _ = adminDB.ExecContext(ctx,
				`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`,
				cfg.DbName)
		}
		terminate()
		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", ident)); err != nil {
			terminate()
			if _, fallbackErr := adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", ident)); fallbackErr != nil {
				return fmt.Errorf("forced drop failed (%v); plain drop also failed: %w", err, fallbackErr)
			}
		}
	case "mariadb":
		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", ident)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported db engine %q", engine)
	}
	return nil
}
