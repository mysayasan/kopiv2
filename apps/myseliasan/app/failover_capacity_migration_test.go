package app

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestMigrationsSurviveAMissingTable is the regression test for a boot that panicked with
//
//	migration 20260825-01-failover-capacity: add failover_plan.standby_max:
//	relation "failover_plan" does not exist (42P01)
//
// Migrations run BEFORE the auto-migrator and only on databases that are NOT fresh (a fresh
// one baselines them as applied). So every migration meets databases that predate the table
// it alters: the fleet upgrades from a version where failover_plan did not exist yet, the
// migration ALTERs a table nobody has created, and the app cannot start at all.
//
// The rule that fixes it — if the table is not there, do nothing and let the auto-migrator
// create it at the entity's current shape — is easy to forget in the next migration, so this
// asserts it for ALL of them against a database with no tables whatsoever.
func TestMigrationsSurviveAMissingTable(t *testing.T) {
	for _, m := range (&module{}).Migrations() {
		t.Run(m.ID, func(t *testing.T) {
			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "empty.db"))
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer db.Close()
			runMigration(t, db, m.Exec)
		})
	}
}

// A failover_plan table as W3-7 first shipped it, holding a plan an operator already made.
func openLegacyFailoverDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "failover.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE failover_plan (
		id INTEGER PRIMARY KEY, name TEXT, protected_node_id TEXT, standby_node_id TEXT,
		enabled BOOLEAN, state TEXT)`); err != nil {
		t.Fatalf("create failover_plan: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO failover_plan (id, name, protected_node_id, standby_node_id, enabled, state)
		VALUES (1, 'HQ cover', 'node-hq', 'node-spare', 1, 'staged')`); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	return db
}

func TestFailoverCapacityMigrationBackfillsExistingPlans(t *testing.T) {
	db := openLegacyFailoverDB(t)
	runMigration(t, db, findMigration(t, "20260825-01-failover-capacity"))

	var (
		max, own  int
		state     string
		checkedAt int64
	)
	// A NULL in any of these is the failure the backfill exists to prevent: the entity's
	// non-pointer int/string/int64 fields cannot scan one, so the whole plan list would fail.
	if err := db.QueryRow(`SELECT standby_max, standby_own, capacity_state, capacity_checked_at
		FROM failover_plan WHERE id = 1`).Scan(&max, &own, &state, &checkedAt); err != nil {
		t.Fatalf("scan capacity columns: %v", err)
	}
	if max != 0 || own != 0 || state != "" || checkedAt != 0 {
		t.Fatalf("existing plan = (%d, %d, %q, %d), want the zero capacity", max, own, state, checkedAt)
	}
}

func TestFailoverCapacityMigrationIsIdempotent(t *testing.T) {
	db := openLegacyFailoverDB(t)
	exec := findMigration(t, "20260825-01-failover-capacity")
	runMigration(t, db, exec)

	// A capacity answer that arrived after the first run must survive a second one.
	if _, err := db.Exec(`UPDATE failover_plan SET standby_max = 32, standby_own = 8, capacity_state = 'ok' WHERE id = 1`); err != nil {
		t.Fatalf("record capacity: %v", err)
	}
	runMigration(t, db, exec)

	var max int
	var state string
	if err := db.QueryRow(`SELECT standby_max, capacity_state FROM failover_plan WHERE id = 1`).Scan(&max, &state); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if max != 32 || state != "ok" {
		t.Fatalf("re-running the migration overwrote a real answer: (%d, %q)", max, state)
	}
}

// A managed_node table as it stood before the staged-rollout work added the reported-version
// columns, holding a node the fleet adopted back then.
func openLegacyManagedNodeDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "nodes.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE managed_node (
		id INTEGER PRIMARY KEY, node_id TEXT, name TEXT, base_url TEXT, status TEXT)`); err != nil {
		t.Fatalf("create managed_node: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO managed_node (id, node_id, name, base_url, status)
		VALUES (1, 'node-hq', 'HQ recorder', 'https://10.0.0.9:3000', 'online')`); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	return db
}

func TestManagedNodeVersionMigrationBackfillsExistingNodes(t *testing.T) {
	db := openLegacyManagedNodeDB(t)
	runMigration(t, db, findMigration(t, "20260901-01-managed-node-version-backfill"))

	var version string
	var seenAt int64
	// NULLs here are what took the whole node list down on postgres — "" and 0 say what the
	// column actually means: this node has never reported a version.
	if err := db.QueryRow(`SELECT version, version_seen_at FROM managed_node WHERE id = 1`).Scan(&version, &seenAt); err != nil {
		t.Fatalf("scan version columns: %v", err)
	}
	if version != "" || seenAt != 0 {
		t.Fatalf("existing node = (%q, %d), want the unreported zero", version, seenAt)
	}
}

func TestManagedNodeVersionMigrationIsIdempotent(t *testing.T) {
	db := openLegacyManagedNodeDB(t)
	exec := findMigration(t, "20260901-01-managed-node-version-backfill")
	runMigration(t, db, exec)

	// A hello that arrived after the first run must survive a second one.
	if _, err := db.Exec(`UPDATE managed_node SET version = 'v1.31.0', version_seen_at = 1788233221 WHERE id = 1`); err != nil {
		t.Fatalf("record version: %v", err)
	}
	runMigration(t, db, exec)

	var version string
	var seenAt int64
	if err := db.QueryRow(`SELECT version, version_seen_at FROM managed_node WHERE id = 1`).Scan(&version, &seenAt); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if version != "v1.31.0" || seenAt != 1788233221 {
		t.Fatalf("re-running the migration overwrote a reported version: (%q, %d)", version, seenAt)
	}
}
