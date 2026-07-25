package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// findMigration returns the named migration from the app's list, so the test exercises the SAME
// code the app runs rather than a copy of it.
func findMigration(t *testing.T, id string) func(context.Context, *sql.Tx, string) error {
	t.Helper()
	for _, m := range (&module{}).Migrations() {
		if m.ID == id {
			return m.Exec
		}
	}
	t.Fatalf("migration %s not registered", id)
	return nil
}

func runMigration(t *testing.T, db *sql.DB, exec func(context.Context, *sql.Tx, string) error) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := exec(context.Background(), tx, "sqlite"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("migration: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// A site table that predates the kind column, holding a row an operator already created. This is
// the case that matters: the column is added underneath a live fleet, and every existing site has
// to come out the other side as a building.
func openLegacySiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE site (id INTEGER PRIMARY KEY, name TEXT, description TEXT, icon TEXT, ordinal INTEGER)`); err != nil {
		t.Fatalf("create site: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO site (id, name, description, icon, ordinal) VALUES (1, 'Head Office', '', '🏢', 0)`); err != nil {
		t.Fatalf("seed site: %v", err)
	}
	return db
}

func TestSiteKindMigrationBackfillsExistingSitesAsBuildings(t *testing.T) {
	db := openLegacySiteDB(t)
	runMigration(t, db, findMigration(t, "20260724-01-site-kind"))

	var kind string
	if err := db.QueryRow(`SELECT kind FROM site WHERE id = 1`).Scan(&kind); err != nil {
		// A NULL here is the exact failure the backfill exists to prevent: entities.Site.Kind is a
		// non-pointer string and cannot scan a NULL left by a defaultless ADD COLUMN.
		t.Fatalf("scan kind: %v", err)
	}
	if kind != "building" {
		t.Fatalf("existing site kind = %q, want \"building\"", kind)
	}
}

// A floor_plan table that predates has_plan_image, holding one row of each shape the backfill has
// to classify.
func openLegacyFloorDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "floors.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE floor_plan (
		id INTEGER PRIMARY KEY, site_id INTEGER, name TEXT, ordinal INTEGER,
		bg_path TEXT, design TEXT, content_type TEXT, width INTEGER, height INTEGER)`); err != nil {
		t.Fatalf("create floor_plan: %v", err)
	}
	rows := []string{
		// 1: a wizard blank area — PNG at the canvas defaults, no design, no background.
		`(1, 1, 'Ground floor', 0, '', '', 'image/png', 1600, 1000)`,
		// 2: an uploaded photo — real dimensions.
		`(2, 1, 'Level 2', 1, '', '', 'image/jpeg', 2480, 1754)`,
		// 3: an uploaded plan that was later annotated — has a pristine background.
		`(3, 1, 'Level 3', 2, '/plans/floor-3.bg.img', '{"walls":[]}', 'image/png', 1600, 1000)`,
		// 4: a blank area that was drawn on — canvas defaults, a design, but NO background, which is
		// what distinguishes it from an annotated upload (annotating an upload preserves bg_path).
		`(4, 1, 'Kitchen', 3, '', '{"walls":[[1,2]]}', 'image/png', 1600, 1000)`,
		// 5: NULL columns, as a defaultless ADD COLUMN would have left them on an older schema.
		`(5, 1, 'Carporch', 4, NULL, NULL, 'image/png', 1600, 1000)`,
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO floor_plan (id, site_id, name, ordinal, bg_path, design, content_type, width, height) VALUES ` + r); err != nil {
			t.Fatalf("seed floor %s: %v", r, err)
		}
	}
	return db
}

func TestFloorHasPlanImageMigrationClassifiesExistingFloors(t *testing.T) {
	db := openLegacyFloorDB(t)
	runMigration(t, db, findMigration(t, "20260724-02-floor-has-plan-image"))

	want := map[int]bool{
		1: false, // generated blank canvas — nothing to remove
		2: true,  // uploaded photo
		3: true,  // uploaded then annotated
		4: false, // blank canvas drawn on — drawing walls is not uploading a plan
		5: false, // NULL bg_path/design still reads as a blank canvas
	}
	for id, expect := range want {
		var got bool
		if err := db.QueryRow(`SELECT has_plan_image FROM floor_plan WHERE id = ?`, id).Scan(&got); err != nil {
			// A NULL here would mean the entity's non-pointer bool cannot scan the row at all.
			t.Fatalf("scan has_plan_image for floor %d: %v", id, err)
		}
		if got != expect {
			t.Fatalf("floor %d has_plan_image = %v, want %v", id, got, expect)
		}
	}
}

func TestFloorHasPlanImageMigrationIsIdempotent(t *testing.T) {
	db := openLegacyFloorDB(t)
	exec := findMigration(t, "20260724-02-floor-has-plan-image")
	runMigration(t, db, exec)

	// An operator uploads a plan onto the blank area after the first run. A second run must not
	// re-classify it back to blank on the strength of its still-default dimensions.
	if _, err := db.Exec(`UPDATE floor_plan SET has_plan_image = 1 WHERE id = 1`); err != nil {
		t.Fatalf("simulate upload: %v", err)
	}
	runMigration(t, db, exec)

	var got bool
	if err := db.QueryRow(`SELECT has_plan_image FROM floor_plan WHERE id = 1`).Scan(&got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !got {
		t.Fatal("re-running the migration must not undo an upload")
	}
}

// A node_placement table holding exactly what the exclusivity rule has to clean up before its
// unique index can exist: a camera pinned twice, and a pin on a floor that is gone.
func openDuplicatePlacementDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "placements.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE floor_plan (id INTEGER PRIMARY KEY, site_id INTEGER, name TEXT)`); err != nil {
		t.Fatalf("create floor_plan: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO floor_plan (id, site_id, name) VALUES (10, 1, 'Ground floor'), (11, 1, 'Level 2')`); err != nil {
		t.Fatalf("seed floors: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE node_placement (id INTEGER PRIMARY KEY, floor_id INTEGER, node_id TEXT, camera_id TEXT, x REAL, y REAL)`); err != nil {
		t.Fatalf("create node_placement: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO node_placement (id, floor_id, node_id, camera_id, x, y) VALUES
		(1, 10, 'node-a', '3', 10, 10),
		(2, 11, 'node-a', '3', 20, 20),
		(3, 10, 'node-a', '4', 30, 30),
		(4, 99, 'node-b', '9', 40, 40),
		(5, 10, 'node-b', '', 50, 50)`); err != nil {
		t.Fatalf("seed placements: %v", err)
	}
	return db
}

func TestPlacementUniqueMigrationDedupesAndEnforces(t *testing.T) {
	db := openDuplicatePlacementDB(t)
	runMigration(t, db, findMigration(t, "20260724-03-placement-unique-camera"))

	ids := map[int64]bool{}
	rows, err := db.Query(`SELECT id FROM node_placement ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids[id] = true
	}
	rows.Close()

	// 1 kept (oldest pin of node-a/3), 2 dropped as the duplicate, 3 and 5 untouched (distinct
	// cameras / the node's own marker), 4 dropped as an orphan pointing at a floor that is gone.
	for _, want := range []int64{1, 3, 5} {
		if !ids[want] {
			t.Fatalf("placement %d should have survived; got %v", want, ids)
		}
	}
	for _, gone := range []int64{2, 4} {
		if ids[gone] {
			t.Fatalf("placement %d should have been removed; got %v", gone, ids)
		}
	}

	// The index is the backstop: a second pin for the same camera must now be impossible even if
	// something bypassed the service check.
	if _, err := db.Exec(`INSERT INTO node_placement (id, floor_id, node_id, camera_id, x, y) VALUES (6, 11, 'node-a', '3', 60, 60)`); err == nil {
		t.Fatal("the unique index must reject a second pin for the same camera")
	}
	// A different camera on the same node is still fine.
	if _, err := db.Exec(`INSERT INTO node_placement (id, floor_id, node_id, camera_id, x, y) VALUES (7, 11, 'node-a', '5', 70, 70)`); err != nil {
		t.Fatalf("a different camera must still be placeable: %v", err)
	}
}

func TestPlacementUniqueMigrationIsIdempotent(t *testing.T) {
	db := openDuplicatePlacementDB(t)
	exec := findMigration(t, "20260724-03-placement-unique-camera")
	runMigration(t, db, exec)
	runMigration(t, db, exec) // re-running must not fail on the index it already created

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM node_placement`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("placement count after re-run = %d, want 3", n)
	}
}

func TestSiteKindMigrationIsIdempotent(t *testing.T) {
	db := openLegacySiteDB(t)
	exec := findMigration(t, "20260724-01-site-kind")
	runMigration(t, db, exec)

	// A site created as a park after the first run must not be rewritten into a building by a
	// second run (the backfill only touches NULL/'').
	if _, err := db.Exec(`INSERT INTO site (id, name, description, icon, ordinal, kind) VALUES (2, 'Central Park', '', '🌳', 0, 'outdoor')`); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	runMigration(t, db, exec)

	var kind string
	if err := db.QueryRow(`SELECT kind FROM site WHERE id = 2`).Scan(&kind); err != nil {
		t.Fatalf("scan kind: %v", err)
	}
	if kind != "outdoor" {
		t.Fatalf("park kind after re-run = %q, want \"outdoor\"", kind)
	}
}
