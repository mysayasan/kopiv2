package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/apis"
	appentities "github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	"github.com/mysayasan/kopiv2/domain/notification"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/control"
	"github.com/mysayasan/kopiv2/infra/coordination"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/eventbus"
	"github.com/mysayasan/kopiv2/infra/mediarelay"
	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/telemetry"
	"github.com/mysayasan/kopiv2/infra/versioning"
)

type module struct {
	// Set during RegisterAppRoutes so the readiness probe can report fleet-listener
	// health. apphost uses one module instance for the process lifetime, so these are
	// safe to read from ReadinessStatus after routes are registered.
	controlServer  *services.ControlServer
	mediaListening *atomic.Bool
}

func New() apphost.App {
	return &module{}
}

// ReadinessStatus reports fleet-listener health as ADVISORY fields on /api/ready. Per
// the apphost contract these never flip the process's ok/HTTP status (that stays gated
// on db + cache) — they give operators/monitoring visibility that a listener has died
// even while the process itself is otherwise healthy.
func (m *module) ReadinessStatus(ctx context.Context) map[string]string {
	status := map[string]string{}
	if m.controlServer != nil {
		status["controlChannel"] = upDown(m.controlServer.IsListening())
		status["connectedNodes"] = strconv.Itoa(m.controlServer.ConnectedCount())
	}
	if m.mediaListening != nil {
		status["mediaRelay"] = upDown(m.mediaListening.Load())
	}
	return status
}

func upDown(up bool) string {
	if up {
		return "up"
	}
	return "down"
}

// periodic runs fn once immediately, then every interval, until ctx is cancelled.
//
// It is supervised: a panic inside fn restarts the loop with backoff instead of killing
// the process. That matters more than it looks — these loops are the retention purges,
// and a dead purge loop is invisible. Nothing re-creates it, so the disk simply fills
// until every write fails, the database included. (Same contract as mymatasan's helper.)
func periodic(ctx context.Context, name string, interval time.Duration, fn func(context.Context)) {
	safego.Supervise(ctx, name, func(ctx context.Context) {
		fn(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fn(ctx)
			case <-ctx.Done():
				return
			}
		}
	})
}

// leaderOnly wraps a task so it runs only on the instance holding the background-work
// lease.
//
// Everything it is applied to below is work that must happen once for the DEPLOYMENT,
// not once per process: retention purges, the notification rollup, the daily digest,
// heartbeat reconciliation. Standalone, the single instance always holds the lease, so
// wrapping changes nothing — which is the point, because the guard has to be safe
// enough to apply everywhere without a second behaviour to reason about.
//
// The check is INSIDE the task rather than around its registration: leadership moves
// while the process runs, and a loop that decided at startup would either never start
// working when it is promoted, or never stop when it is demoted.
func leaderOnly(leader *coordination.Leader, fn func(context.Context)) func(context.Context) {
	return func(ctx context.Context) {
		if !leader.IsLeader() {
			return
		}
		fn(ctx)
	}
}

// leaderTicker runs fn on an interval, but only while this instance holds the lease.
// The ticker keeps running for followers so a promoted instance picks the work up
// without a restart.
func leaderTicker(ctx context.Context, leader *coordination.Leader, interval time.Duration, fn func(context.Context)) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if leader.IsLeader() {
					fn(ctx)
				}
			}
		}
	}()
}

// purgeInterval turns the configured purge cadence (hours) into a duration,
// defaulting to every 6 hours when unset.
func purgeInterval(hours int) time.Duration {
	if d := time.Duration(hours) * time.Hour; d > 0 {
		return d
	}
	return 6 * time.Hour
}

func (m *module) Name() string {
	return "myseliasan"
}

func (m *module) BaseDir() string {
	return filepath.Join("apps", "myseliasan")
}

func (m *module) SharedAPIs() apphost.SharedAPIConfig {
	cfg := apphost.DefaultSharedAPIConfig()
	cfg.ApiLog = false
	cfg.AppRegistry = false
	cfg.ApiEndpoint = false
	cfg.FileStorage = false
	cfg.CacheService = false
	cfg.RuntimeLog = false
	return cfg
}

// Migrations runs BEFORE the auto-migrator and independently of the autoMigrate config flag.
// The single entry here guarantees the fleet-map columns exist on managed_node even in a
// deployment that has autoMigrate turned off — without them, node adoption's INSERT fails
// with a 500 (the node pairs, then the record can't be saved). Idempotent: it adds only the
// columns that are missing, so it is a no-op where the auto-migrator already added them.
func (m *module) Migrations() []bootstrap.Migration {
	return []bootstrap.Migration{
		{
			ID:   "20260718-01-managed-node-geo",
			Name: "add lat/lon/map_placed to managed_node (fleet map)",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				return ensureManagedNodeGeoColumns(ctx, tx, engine)
			},
		},
		{
			// A SEPARATE migration (new ID) so it runs even on databases where 01 already
			// applied before the backfill logic existed: an ADD COLUMN without a default left
			// existing rows NULL, and the non-pointer float64/bool entity fields cannot scan a
			// NULL. Editing 01 would not re-run it; 02 always runs once. Idempotent.
			ID:   "20260718-02-managed-node-geo-backfill",
			Name: "backfill NULL lat/lon/map_placed on managed_node to zero",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				return backfillManagedNodeGeoNulls(ctx, tx, engine)
			},
		},
		{
			ID:   "20260719-01-placement-fov",
			Name: "add heading/fov to node_placement (floor-plan camera coverage arcs)",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				return ensurePlacementFovColumns(ctx, tx, engine)
			},
		},
		{
			ID:   "20260719-02-floor-design",
			Name: "add design (drawn-plan vector shapes) to floor_plan",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				return ensureFloorDesignColumn(ctx, tx, engine)
			},
		},
		{
			ID:   "20260719-03-floor-bgpath",
			Name: "add bg_path (pristine background image) to floor_plan",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				existing, err := tableColumns(ctx, tx, engine, "floor_plan")
				if err != nil {
					return err
				}
				if !existing["bg_path"] {
					if _, err := tx.ExecContext(ctx, "ALTER TABLE floor_plan ADD COLUMN bg_path TEXT"); err != nil {
						return fmt.Errorf("add floor_plan.bg_path: %w", err)
					}
				}
				if _, err := tx.ExecContext(ctx, "UPDATE floor_plan SET bg_path = '' WHERE bg_path IS NULL"); err != nil {
					return fmt.Errorf("backfill floor_plan.bg_path NULLs: %w", err)
				}
				return nil
			},
		},
		{
			// Digital-twin buildings: a site now has a geographic position, so it can be a marker
			// on the geo map (a building is where cameras physically live, independent of the node
			// that records them). Without this an EXISTING site table has no lat/lon/map_placed, so
			// both INSERT (create site) and List fail — the site never appears. Mirrors the
			// managed_node geo migration (add + backfill NULLs; non-pointer float64/bool can't scan
			// a NULL left by a defaultless ADD COLUMN). Idempotent.
			ID:   "20260720-01-site-geo",
			Name: "add lat/lon/map_placed to site (fleet map buildings)",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				return ensureSiteGeoColumns(ctx, tx, engine)
			},
		},
		{
			// A building can carry a chosen glyph (emoji) shown on the geo map. Same NULL-safety as
			// the other string columns: ADD COLUMN then backfill NULLs to '' (the entity's Icon
			// string cannot scan a NULL). Idempotent.
			ID:   "20260720-02-site-icon",
			Name: "add icon (building glyph) to site",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				existing, err := tableColumns(ctx, tx, engine, "site")
				if err != nil {
					return err
				}
				colType := "TEXT"
				if engine == "mariadb" {
					colType = "VARCHAR(32)"
				}
				if !existing["icon"] {
					if _, err := tx.ExecContext(ctx, "ALTER TABLE site ADD COLUMN icon "+colType); err != nil {
						return fmt.Errorf("add site.icon: %w", err)
					}
				}
				if _, err := tx.ExecContext(ctx, "UPDATE site SET icon = '' WHERE icon IS NULL"); err != nil {
					return fmt.Errorf("backfill site.icon NULLs: %w", err)
				}
				return nil
			},
		},
		{
			// The building an appliance resides in (building-first map). Same NULL-safety: ADD COLUMN
			// then backfill NULLs to 0 (the entity's SiteId int64 is non-pointer and cannot scan a
			// NULL). Idempotent.
			ID:   "20260720-03-node-site",
			Name: "add site_id (resides-in building) to managed_node",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				existing, err := tableColumns(ctx, tx, engine, "managed_node")
				if err != nil {
					return err
				}
				colType := "BIGINT"
				if engine == "sqlite" {
					colType = "INTEGER"
				}
				if !existing["site_id"] {
					if _, err := tx.ExecContext(ctx, "ALTER TABLE managed_node ADD COLUMN site_id "+colType); err != nil {
						return fmt.Errorf("add managed_node.site_id: %w", err)
					}
				}
				if _, err := tx.ExecContext(ctx, "UPDATE managed_node SET site_id = 0 WHERE site_id IS NULL"); err != nil {
					return fmt.Errorf("backfill managed_node.site_id NULLs: %w", err)
				}
				return nil
			},
		},
		{
			// The certificate auto-renew gate. Same NULL-safety as site_id: a bare ADD COLUMN
			// leaves existing rows NULL and the entity's AutoRenew bool is non-pointer, so it
			// cannot scan a NULL — ADD COLUMN then backfill NULLs to false. This only seeds the
			// column's zero value; the separate one-time BackfillAutoRenew (services) then flips
			// already-ENROLLED nodes to true so an existing fleet is not surprise-expired.
			// Idempotent.
			ID:   "20260720-04-node-auto-renew",
			Name: "add auto_renew (cert renewal gate) to managed_node",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				existing, err := tableColumns(ctx, tx, engine, "managed_node")
				if err != nil {
					return err
				}
				if !existing["auto_renew"] {
					if _, err := tx.ExecContext(ctx, "ALTER TABLE managed_node ADD COLUMN auto_renew "+geoColumnType("BOOLEAN", engine)); err != nil {
						return fmt.Errorf("add managed_node.auto_renew: %w", err)
					}
				}
				falseLit := "0"
				if engine == "postgres" {
					falseLit = "false"
				}
				if _, err := tx.ExecContext(ctx, "UPDATE managed_node SET auto_renew = "+falseLit+" WHERE auto_renew IS NULL"); err != nil {
					return fmt.Errorf("backfill managed_node.auto_renew NULLs: %w", err)
				}
				return nil
			},
		},
		{
			// 3D floor plans: a floor carries a painted grid (walls/floor cells) plus real-world
			// scale, wall height and stacking elevation. Same NULL-safety as design/fov — grid is a
			// string ('' default), the three float64 fields backfill to 0. Idempotent.
			ID:   "20260723-01-floor-3d",
			Name: "add grid/scale/wall_height/elevation to floor_plan (3D view)",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				return ensureFloor3DColumns(ctx, tx, engine)
			},
		},
		{
			// 3D camera placement: a placement gains mount height (metres above floor) and downward
			// pitch (degrees) so its coverage cone stands correctly in 3D. Same NULL-safety as the
			// heading/fov columns (non-pointer float64 cannot scan a NULL). Idempotent.
			ID:   "20260723-02-placement-mount",
			Name: "add mount_height/pitch to node_placement (3D camera cones)",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				return ensurePlacementMountColumns(ctx, tx, engine)
			},
		},
		{
			// Not every monitored place is a building — a park has one open ground surface, a
			// traffic-light junction has none. `kind` says which, and the marker/editor/3D all read
			// it. Backfilled to 'building' rather than '' so an existing fleet reads the same in the
			// database as it does in the UI (entities.NormalizeSiteKind treats both as building).
			// Same NULL-safety as site.icon: the entity's Kind string cannot scan a NULL left by a
			// defaultless ADD COLUMN. Idempotent.
			ID:   "20260724-01-site-kind",
			Name: "add kind (building/outdoor/point) to site",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				existing, err := tableColumns(ctx, tx, engine, "site")
				if err != nil {
					return err
				}
				colType := "TEXT"
				if engine == "mariadb" {
					colType = "VARCHAR(32)"
				}
				if !existing["kind"] {
					if _, err := tx.ExecContext(ctx, "ALTER TABLE site ADD COLUMN kind "+colType); err != nil {
						return fmt.Errorf("add site.kind: %w", err)
					}
				}
				if _, err := tx.ExecContext(ctx, "UPDATE site SET kind = 'building' WHERE kind IS NULL OR kind = ''"); err != nil {
					return fmt.Errorf("backfill site.kind: %w", err)
				}
				return nil
			},
		},
		{
			// Does this floor's picture come from an operator upload, or is it the generated blank
			// canvas? Both are stored the same way, so the editor could not tell and offered
			// "Remove plan" on areas that had no plan to remove.
			//
			// Existing rows carry no record of which they are, so they are classified by shape: a
			// generated blank is a PNG at the canvas defaults with no stored background (see
			// AddBlankFloor — the wizard and the editor never pass their own dimensions). A drawn
			// design does NOT count as a plan: annotating an uploaded picture always preserves it
			// as bg_path first (ReplaceFloorImage), so a design with no background means the walls
			// were drawn on a blank canvas.
			//
			// Everything else is assumed to be a real plan, so this can only ever hide the button,
			// never a plan. The one misjudged case — an upload that happens to be a PNG at exactly
			// the blank canvas size and was never annotated — is recovered by uploading it again.
			ID:   "20260724-02-floor-has-plan-image",
			Name: "add has_plan_image (uploaded vs blank canvas) to floor_plan",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				existing, err := tableColumns(ctx, tx, engine, "floor_plan")
				if err != nil {
					return err
				}
				if !existing["has_plan_image"] {
					if _, err := tx.ExecContext(ctx, "ALTER TABLE floor_plan ADD COLUMN has_plan_image "+geoColumnType("BOOLEAN", engine)); err != nil {
						return fmt.Errorf("add floor_plan.has_plan_image: %w", err)
					}
				}
				trueLit, falseLit := "1", "0"
				if engine == "postgres" {
					trueLit, falseLit = "true", "false"
				}
				blank := "(COALESCE(bg_path, '') = '' AND content_type = 'image/png' AND width = 1600 AND height = 1000)"
				if _, err := tx.ExecContext(ctx, "UPDATE floor_plan SET has_plan_image = "+falseLit+" WHERE has_plan_image IS NULL AND "+blank); err != nil {
					return fmt.Errorf("backfill floor_plan.has_plan_image (blank): %w", err)
				}
				if _, err := tx.ExecContext(ctx, "UPDATE floor_plan SET has_plan_image = "+trueLit+" WHERE has_plan_image IS NULL"); err != nil {
					return fmt.Errorf("backfill floor_plan.has_plan_image NULLs: %w", err)
				}
				return nil
			},
		},
		{
			// Placement is EXCLUSIVE: a camera is in one physical place, so it holds one pin. The
			// service refuses a second placement with a message naming where the first one is; this
			// unique index is the backstop that keeps two concurrent requests from both winning.
			//
			// It must run BEFORE the auto-migrator, which creates the same index off the entity's
			// ukey tag and would fail on a database that already contains duplicates. So duplicates
			// are resolved first — the OLDEST pin of each camera is kept (it is the one the
			// operator has been looking at; later ones were the accident) and the rest deleted.
			// Pins on a floor that no longer exists go too: they render nowhere, so keeping one
			// would silently block its camera from ever being placed again.
			//
			// Same index name the auto-migrator derives (ux_<table>_<ukey group>), so whichever
			// runs second is a no-op.
			ID:   "20260724-03-placement-unique-camera",
			Name: "one pin per camera: dedupe node_placement and add the unique index",
			Exec: func(ctx context.Context, tx *sql.Tx, engine string) error {
				cols, err := tableColumns(ctx, tx, engine, "node_placement")
				if err != nil {
					return err
				}
				if len(cols) == 0 {
					return nil // fresh install: the auto-migrator creates the table with the index
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM node_placement WHERE floor_id NOT IN (SELECT id FROM floor_plan)`); err != nil {
					return fmt.Errorf("drop orphaned placements: %w", err)
				}
				if _, err := tx.ExecContext(ctx, `
DELETE FROM node_placement
WHERE id NOT IN (
	SELECT MIN(id) FROM node_placement GROUP BY node_id, camera_id
)`); err != nil {
					return fmt.Errorf("dedupe node_placement: %w", err)
				}
				create := "CREATE UNIQUE INDEX IF NOT EXISTS ux_node_placement_camera ON node_placement (node_id, camera_id)"
				if engine == "mariadb" {
					// MariaDB lacks IF NOT EXISTS for CREATE INDEX on the versions we support; a
					// duplicate-name error here means the index is already there.
					if _, err := tx.ExecContext(ctx, "CREATE UNIQUE INDEX ux_node_placement_camera ON node_placement (node_id, camera_id)"); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate key name") {
						return fmt.Errorf("add ux_node_placement_camera: %w", err)
					}
					return nil
				}
				if _, err := tx.ExecContext(ctx, create); err != nil {
					return fmt.Errorf("add ux_node_placement_camera: %w", err)
				}
				return nil
			},
		},
		{
			// The rollup gained a per-source dimension (per-node baselines); existing
			// tables need the source column added and the old slot unique index dropped
			// so the auto-migrator recreates it including source. Shared with mymatasan.
			ID:   "20260806-01-notification-rollup-source",
			Name: "add source to notification_rollup and rebuild the slot index",
			Exec: notification.MigrateRollupSourceColumn,
		},
	}
}

// ensureFloor3DColumns adds the 3D layout columns to floor_plan if absent and backfills NULLs
// (grid to ”, the float columns to 0 — the entity's non-pointer fields cannot scan a NULL left
// by a defaultless ADD COLUMN). Mirrors ensureFloorDesignColumn + ensurePlacementFovColumns.
// Idempotent.
func ensureFloor3DColumns(ctx context.Context, tx *sql.Tx, engine string) error {
	existing, err := tableColumns(ctx, tx, engine, "floor_plan")
	if err != nil {
		return err
	}
	textType := "TEXT"
	if engine == "mariadb" {
		textType = "LONGTEXT"
	}
	if !existing["grid"] {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE floor_plan ADD COLUMN grid "+textType); err != nil {
			return fmt.Errorf("add floor_plan.grid: %w", err)
		}
	}
	for _, name := range []string{"scale", "wall_height", "elevation"} {
		if existing[name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE floor_plan ADD COLUMN "+name+" "+geoColumnType("DOUBLE PRECISION", engine)); err != nil {
			return fmt.Errorf("add floor_plan.%s: %w", name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE floor_plan SET grid = '' WHERE grid IS NULL"); err != nil {
		return fmt.Errorf("backfill floor_plan.grid NULLs: %w", err)
	}
	for _, name := range []string{"scale", "wall_height", "elevation"} {
		if _, err := tx.ExecContext(ctx, "UPDATE floor_plan SET "+name+" = 0 WHERE "+name+" IS NULL"); err != nil {
			return fmt.Errorf("backfill floor_plan.%s NULLs: %w", name, err)
		}
	}
	return nil
}

// ensurePlacementMountColumns adds mount_height/pitch to node_placement if absent and backfills
// NULLs to zero (non-pointer float64 cannot scan a NULL). Mirrors ensurePlacementFovColumns.
// Idempotent.
func ensurePlacementMountColumns(ctx context.Context, tx *sql.Tx, engine string) error {
	existing, err := tableColumns(ctx, tx, engine, "node_placement")
	if err != nil {
		return err
	}
	for _, name := range []string{"mount_height", "pitch"} {
		if existing[name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE node_placement ADD COLUMN "+name+" "+geoColumnType("DOUBLE PRECISION", engine)); err != nil {
			return fmt.Errorf("add node_placement.%s: %w", name, err)
		}
	}
	for _, name := range []string{"mount_height", "pitch"} {
		if _, err := tx.ExecContext(ctx, "UPDATE node_placement SET "+name+" = 0 WHERE "+name+" IS NULL"); err != nil {
			return fmt.Errorf("backfill node_placement.%s NULLs: %w", name, err)
		}
	}
	return nil
}

// ensureSiteGeoColumns adds the geographic columns to site if absent (same per-engine types the
// auto-migrator generates, so no schema-drift warning) and backfills any NULLs to zero/false.
// Idempotent — a no-op once the columns exist and are non-NULL.
func ensureSiteGeoColumns(ctx context.Context, tx *sql.Tx, engine string) error {
	existing, err := tableColumns(ctx, tx, engine, "site")
	if err != nil {
		return err
	}
	adds := []struct{ name, base string }{
		{"lat", "DOUBLE PRECISION"},
		{"lon", "DOUBLE PRECISION"},
		{"map_placed", "BOOLEAN"},
	}
	for _, c := range adds {
		if existing[c.name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE site ADD COLUMN "+c.name+" "+geoColumnType(c.base, engine)); err != nil {
			return fmt.Errorf("add site.%s: %w", c.name, err)
		}
	}
	falseLit := "0"
	if engine == "postgres" {
		falseLit = "false"
	}
	stmts := []string{
		"UPDATE site SET lat = 0 WHERE lat IS NULL",
		"UPDATE site SET lon = 0 WHERE lon IS NULL",
		"UPDATE site SET map_placed = " + falseLit + " WHERE map_placed IS NULL",
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("backfill site geo NULLs: %w", err)
		}
	}
	return nil
}

// ensureFloorDesignColumn adds the design column (drawn-plan vector JSON) to floor_plan if absent
// and backfills NULLs to ” (the entity's Design string cannot scan a NULL). Idempotent.
func ensureFloorDesignColumn(ctx context.Context, tx *sql.Tx, engine string) error {
	existing, err := tableColumns(ctx, tx, engine, "floor_plan")
	if err != nil {
		return err
	}
	colType := "TEXT"
	if engine == "mariadb" {
		colType = "LONGTEXT"
	}
	if !existing["design"] {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE floor_plan ADD COLUMN design "+colType); err != nil {
			return fmt.Errorf("add floor_plan.design: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE floor_plan SET design = '' WHERE design IS NULL"); err != nil {
		return fmt.Errorf("backfill floor_plan.design NULLs: %w", err)
	}
	return nil
}

// ensurePlacementFovColumns adds the coverage-arc columns to node_placement if absent and
// backfills any NULLs to zero (an ADD COLUMN without a default leaves existing rows NULL, and the
// entity's Heading/Fov float64 fields cannot scan a NULL). Idempotent.
func ensurePlacementFovColumns(ctx context.Context, tx *sql.Tx, engine string) error {
	existing, err := tableColumns(ctx, tx, engine, "node_placement")
	if err != nil {
		return err
	}
	for _, name := range []string{"heading", "fov"} {
		if existing[name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE node_placement ADD COLUMN "+name+" "+geoColumnType("DOUBLE PRECISION", engine)); err != nil {
			return fmt.Errorf("add node_placement.%s: %w", name, err)
		}
	}
	for _, name := range []string{"heading", "fov"} {
		if _, err := tx.ExecContext(ctx, "UPDATE node_placement SET "+name+" = 0 WHERE "+name+" IS NULL"); err != nil {
			return fmt.Errorf("backfill node_placement.%s NULLs: %w", name, err)
		}
	}
	return nil
}

// ensureManagedNodeGeoColumns adds the geographic columns to managed_node if absent, using the
// SAME per-engine types the auto-migrator generates (float64 and bool normalized per engine),
// so a column added here is byte-identical to one the auto-migrator would add and never
// triggers a schema-drift warning.
func ensureManagedNodeGeoColumns(ctx context.Context, tx *sql.Tx, engine string) error {
	existing, err := tableColumns(ctx, tx, engine, "managed_node")
	if err != nil {
		return err
	}
	adds := []struct{ name, base string }{
		{"lat", "DOUBLE PRECISION"},
		{"lon", "DOUBLE PRECISION"},
		{"map_placed", "BOOLEAN"},
	}
	for _, c := range adds {
		if existing[c.name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE managed_node ADD COLUMN "+c.name+" "+geoColumnType(c.base, engine)); err != nil {
			return fmt.Errorf("add managed_node.%s: %w", c.name, err)
		}
	}
	return backfillManagedNodeGeoNulls(ctx, tx, engine)
}

// backfillManagedNodeGeoNulls sets any NULL lat/lon/map_placed to their zero value. An ADD
// COLUMN without a default leaves EXISTING rows NULL, and the entity's Lat/Lon (float64) and
// MapPlaced (bool) are non-pointer, so the row scanner fails on a NULL with "converting NULL
// to float64 is unsupported" — breaking List AND adoption's read-back. Engine-aware:
// map_placed is BOOLEAN on postgres, so its zero value is `false`, not `0` (a boolean-vs-
// integer type error) — sqlite/mariadb store it as an integer where 0 is correct. Idempotent.
func backfillManagedNodeGeoNulls(ctx context.Context, tx *sql.Tx, engine string) error {
	falseLit := "0"
	if engine == "postgres" {
		falseLit = "false"
	}
	stmts := []string{
		"UPDATE managed_node SET lat = 0 WHERE lat IS NULL",
		"UPDATE managed_node SET lon = 0 WHERE lon IS NULL",
		"UPDATE managed_node SET map_placed = " + falseLit + " WHERE map_placed IS NULL",
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("backfill managed_node geo NULLs: %w", err)
		}
	}
	return nil
}

// geoColumnType maps a base SQL type to the concrete per-engine type the auto-migrator uses
// (mirrors infra/db/bootstrap normalizeSQLType), so the explicit migration and the
// auto-migrator produce the same column definition.
func geoColumnType(base, engine string) string {
	switch engine {
	case "sqlite":
		switch base {
		case "DOUBLE PRECISION":
			return "REAL"
		case "BOOLEAN":
			return "INTEGER"
		}
	case "mariadb":
		switch base {
		case "DOUBLE PRECISION":
			return "DOUBLE"
		case "BOOLEAN":
			return "TINYINT(1)"
		}
	}
	return base // postgres, or an already-concrete type
}

// tableColumns returns the lower-cased column names of a table for the given engine.
func tableColumns(ctx context.Context, tx *sql.Tx, engine, table string) (map[string]bool, error) {
	var query string
	var args []any
	switch engine {
	case "postgres":
		query = "SELECT column_name FROM information_schema.columns WHERE table_name = $1"
		args = []any{table}
	case "mariadb":
		query = "SELECT column_name FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ?"
		args = []any{table}
	case "sqlite":
		query = "SELECT name FROM pragma_table_info('" + table + "')"
	default:
		return nil, fmt.Errorf("unsupported db engine %q", engine)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[strings.ToLower(name)] = true
	}
	return out, rows.Err()
}

func (m *module) Entities() []any {
	return []any{
		sharedentities.ApiEndpoint{},
		sharedentities.ApiLog{},
		sharedentities.UserSession{},
		sharedentities.Notification{},
		// Hourly rollup of the notification feed: the substrate the heatmap/baseline
		// analytics and the AI digest's anomaly findings read. Folded by the
		// RollupMaintainer started in RegisterAppRoutes.
		sharedentities.NotificationRollup{},
		appentities.ManagedNode{},
		appentities.ControlSetting{},
		// Shared key-value row the rest of the suite already carries; the first-run
		// wizard's completion flag lives here. ControlSetting is deliberately left
		// alone — it holds the fleet key and is the node-adoption path's table.
		sharedentities.RuntimeSetting{},
		appentities.NodeAccessGrant{},
		appentities.ControlUser{},
		appentities.AuditLog{},
		sharedentities.AccessRole{},
		sharedentities.AccessRolePermission{},
		// Cross-domain correlation: the reason the suite has a fourth app.
		appentities.FleetRule{},
		appentities.FleetRuleClause{},
		// Fleet configuration policy: what the estate's node settings OUGHT to be, so
		// drift from it can be reported instead of discovered.
		appentities.FleetPolicy{},
		appentities.FleetPolicyItem{},
		// Fleet map: sites + uploaded floor plans (indoor view); node/camera placements.
		appentities.Site{},
		appentities.FloorPlan{},
		appentities.NodePlacement{},
		// Dedup ledger for node-relayed notifications (reconnect replay idempotency).
		appentities.RelayedNotif{},
		// Fleet AI agent: stored digests (structured findings + optional LLM narrative).
		appentities.AgentDigest{},
		// Node state history + monitoring coverage: what the fleet's uptime WAS, so an
		// SLA can be reported over a past window rather than only observed in the present.
		appentities.NodeStateEvent{},
		appentities.FleetMonitorGap{},
		// Critical-clip archive: the fleet's own copy of the footage that matters, so an
		// appliance that is stolen or burned does not take the evidence with it.
		appentities.ArchivedClip{},
	}
}

func (m *module) Seeders(seedStatements []string) []bootstrap.Seeder {
	type endpointSeed struct {
		Title       string
		Description string
		Path        string
		AccessTier  apiaccessenums.AccessTier
	}

	endpoints := []endpointSeed{
		{Title: "API Health", Description: "api namespace health", Path: "/api/health", AccessTier: apiaccessenums.Public},
		{Title: "Runtime Version", Description: "runtime version access", Path: "/api/version", AccessTier: apiaccessenums.Public},
		{Title: "Auth", Description: "relying-app auth start, callback, and logout", Path: "/api/auth", AccessTier: apiaccessenums.Public},
		{Title: "Session", Description: "current relying-app session metadata", Path: "/api/session", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Notifications", Description: "control-plane unified feed of node-pushed events", Path: "/api/notifications", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Nodes", Description: "mymatasan node discovery, adoption, and management", Path: "/api/nodes", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Node Access", Description: "per-node read/write access grants (owner-role managed)", Path: "/api/nodes/access", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Node Self-Drop", Description: "node-initiated unpair notice (fleet-key authenticated)", Path: "/api/nodes/self-dropped", AccessTier: apiaccessenums.Public},
		{Title: "Audit", Description: "append-only audit trail of sensitive actions (superadmin-gated)", Path: "/api/audit", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Basemap", Description: "offline vector basemap archive for the fleet map", Path: "/api/basemap", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Sites", Description: "sites and uploaded floor plans for the indoor map", Path: "/api/sites", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Floors", Description: "floor-plan images and node/camera placements", Path: "/api/floors", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Placements", Description: "list, reposition and remove node/camera markers on floor plans", Path: "/api/placements", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Node Floorplan", Description: "floor plans holding a node camera markers (geo-map drill-down)", Path: "/api/node-floorplan", AccessTier: apiaccessenums.AuthOnly},
		{Title: "AI Agent", Description: "fleet digest, ask-the-fleet chat, and LLM sidecar management", Path: "/api/agent", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Settings", Description: "in-app editor for the safe subset of config.json (superadmin-gated)", Path: "/api/settings", AccessTier: apiaccessenums.AuthOnly},
		{Title: "System", Description: "process restart to apply settings changes (superadmin-gated)", Path: "/api/system", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Setup Wizard", Description: "first-run setup state and completion", Path: "/api/setup", AccessTier: apiaccessenums.AuthOnly},
		{Title: "User Manual", Description: "the built-in manual; public so help works on the sign-in screen and in the first-run wizard", Path: "/api/manual", AccessTier: apiaccessenums.Public},
	}

	statements := make([]string, 0, len(endpoints)*2)
	for _, endpoint := range endpoints {
		statements = append(statements,
			fmt.Sprintf(`INSERT INTO api_endpoint (title, description, app_code, host, path, access_tier, is_active, created_by, created_at, updated_by, updated_at)
SELECT '%s', '%s', 'myseliasan', '*', '%s', %d, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM api_endpoint WHERE app_code = 'myseliasan' AND host = '*' AND path = '%s');`, endpoint.Title, endpoint.Description, endpoint.Path, endpoint.AccessTier, endpoint.Path),
			fmt.Sprintf(`UPDATE api_endpoint SET app_code = 'myseliasan', access_tier = %d WHERE host = '*' AND path = '%s' AND ((access_tier IS NULL OR access_tier <> %d) OR app_code IS NULL OR app_code = '');`, endpoint.AccessTier, endpoint.Path, endpoint.AccessTier),
		)
	}

	seeders := []bootstrap.Seeder{
		bootstrap.NewSQLSeeder("myseliasan-endpoints", statements),
	}
	if len(seedStatements) > 0 {
		seeders = append(seeders, bootstrap.NewSQLSeeder("config", seedStatements))
	}
	return seeders
}

func (m *module) RegisterAppRoutes(api *mux.Router, deps apphost.Dependencies) (apphost.ShutdownFunc, error) {
	// Roles, the permission matrix, and the authorization middleware come from
	// apphost's shared accessrbac core (deps.Access*); apphost already seeded the
	// built-in roles + viewer defaults. myseliasan supplies its own user layer (the
	// resolver) and the stock-superadmin bootstrap. The middleware enforces
	// disabled/must-change + the matrix on myseliasan's OWN endpoints (not the node
	// tunnel, which is axis-2).
	roleService := deps.AccessRoles
	userService := services.NewControlUserService(deps.Db, roleService)
	// A RESET_ADMIN marker in the data dir (dropped by the Windows installer's "reset
	// the admin login" option, or by hand) is the lock-out recovery path. Consume it
	// BEFORE seeding, and delete it first so a later restart never re-runs the reset.
	seed, err := consumeAdminResetMarker(deps, userService)
	if err != nil {
		return nil, fmt.Errorf("reset stock superadmin: %w", err)
	}
	if seed == nil {
		established, serr := userService.EnsureStockSuperadmin(context.Background(), deps.Config.LocalAuth.Username, deps.Config.LocalAuth.Password)
		if serr != nil {
			return nil, fmt.Errorf("seed stock superadmin: %w", serr)
		}
		seed = &established
	}
	if seed.Seeded {
		announceFirstRunAdmin(deps, *seed)
	}
	controlSession := deps.Access
	controlSession.SetResolver(userService)

	// Immutable audit trail for sensitive actions (adopt/release/wipe, RBAC changes,
	// fleet-key rotation). Append-only: handlers only Record; the read API is
	// superadmin-gated. Distinct from api_log (HTTP access log with retention).
	auditService := services.NewAuditService(deps.Db, func(format string, args ...any) {
		deps.Logger.Warnf("myseliasan.audit", format, args...)
	})

	apis.NewAuthApi(api, deps.Config, deps.Auth, deps.Cache, userService)
	apis.NewSessionApi(api, *deps.Auth, userService, roleService, deps.AccessPerms)
	apis.NewRbacAdminApi(api, *deps.Auth, controlSession, roleService, userService, auditService)
	apis.NewAuditApi(api, *deps.Auth, controlSession, auditService)

	// Offline vector basemap for the fleet map: a directory of .pmtiles region archives
	// under the data dir (absent = map renders without cartography). Normally an intranet
	// install never reaches a CDN; but if MYSELIASAN_BASEMAP_SOURCE is set to a remote
	// pmtiles URL, an operator can DOWNLOAD a new region on demand (extracted with the
	// pmtiles tool, MYSELIASAN_PMTILES_BIN or "pmtiles" on PATH) — the one online action.
	apis.NewBasemapApi(api, *deps.Auth, controlSession,
		apis.ResolveBasemapDir(deps.DataDir, ""),
		os.Getenv("MYSELIASAN_BASEMAP_SOURCE"),
		os.Getenv("MYSELIASAN_PMTILES_BIN"))

	// Node management: discover, adopt, and release mymatasan nodes over the
	// fleet-key-authenticated pairing protocol. ParentBaseURL is recorded on each
	// node so it can call back (enroll / release / self-drop). The control plane is
	// also the fleet CA that issues node certs for the mTLS management channel.
	parentID := deps.Config.SSO.ClientID
	if parentID == "" {
		parentID = "myseliasan"
	}
	p := deps.Config.Pairing
	mtlsPort := p.MTLSPort
	if mtlsPort <= 0 {
		mtlsPort = 49532
	}
	// ParentBaseURL is the address each node records for callbacks (enroll / release /
	// self-drop) AND the host it dials for the persistent control channel. The default,
	// sso.redirectBaseUrl, is correct only when node and parent share a host; a node on
	// its own machine needs the parent's LAN-reachable URL, so pairing.parentBaseUrl
	// overrides it (must not be localhost in that case).
	parentBaseURL := strings.TrimSpace(p.ParentBaseURL)
	if parentBaseURL == "" {
		parentBaseURL = deps.Config.SSO.RedirectBaseURL
	}
	hbInterval := time.Duration(p.HeartbeatIntervalSeconds) * time.Second
	if hbInterval <= 0 {
		hbInterval = 60 * time.Second
	}

	// Encryption at rest for FLEET SECRETS. The fleet mTLS CA private key and the fleet
	// PSK live in the control-plane database; without this, anyone who can read the DB
	// owns the whole fleet's trust. When enabled (default), those secret settings are
	// AES-256-GCM encrypted (infra/atrest) with a master key stored OUTSIDE the DB.
	// Reads transparently pass through legacy plaintext, so enabling it needs no
	// migration; public certs and the revocation list stay plaintext.
	secretCipher, secretKeyStore, secErr := openFleetSecretCipher(deps)
	if secErr != nil {
		return nil, secErr
	}

	registry := services.NewNodeRegistry(deps.Db, services.NodeRegistryConfig{
		MulticastAddr:     p.MulticastAddr,
		ParentID:          parentID,
		ParentName:        parentID,
		ParentBaseURL:     parentBaseURL,
		MTLSPort:          mtlsPort,
		CertTTL:           time.Duration(p.CertTTLHours) * time.Hour,
		HeartbeatInterval: hbInterval,
		// Warn when a still-valid node cert is within its renewal window of expiring:
		// at that point automatic re-enrollment is overdue, so the operator should know.
		CertWarnBefore: time.Duration(p.RenewBeforeHours) * time.Hour,
		SecretCipher:   secretCipher,
	})
	// One-time: bless nodes already in the fleet with auto-renew so upgrading to the
	// operator-gated renewal model doesn't silently expire a fleet that was renewing
	// automatically. New adoptions start with auto-renew off (a dead-man's switch).
	if err := registry.BackfillAutoRenew(context.Background()); err != nil {
		deps.Logger.Warnf("myseliasan.nodes", "auto-renew backfill failed: %v", err)
	}
	// Node state history: the heartbeat's liveness transitions, written down. The
	// registry reports every observation here (nil-safe, best-effort) and the
	// availability service reads it back. Injected rather than constructed inside the
	// registry so the registry's own tests stay repo-only.
	stateHistory := services.NewNodeStateHistory(deps.Db)
	registry.SetStateHistory(stateHistory)

	nodesApi := apis.NewNodesApi(api, *deps.Auth, controlSession, registry, auditService,
		func(f string, a ...any) { deps.Logger.Warnf("myseliasan.nodes", f, a...) })

	// Sites + floor plans (indoor map). Plan images are encrypted at rest with the same
	// fleet cipher that protects the CA key/PSK, stored under <dataDir>/floorplans.
	planDir := apphost.ResolveWritablePath(deps.DataDir, "floorplans")
	siteService := services.NewSiteService(deps.Db, secretCipher, planDir)
	apis.NewSitesApi(api, *deps.Auth, controlSession, siteService)

	// Availability (SLA) reporting over the recorded history. Mounted on the nodes API
	// so it inherits the same page grant as the fleet list it is about, rather than
	// inventing a second permission for the same data in a different shape.
	availabilityService := services.NewNodeAvailabilityService(registry, siteService, stateHistory)
	nodesApi.SetAvailability(availabilityService)

	bgCtx, stopBackground := context.WithCancel(context.Background())

	// Shared key-value runtime settings (first-run wizard flag, rollup cursor, digest
	// schedule watermark). Built here because the notification rollup needs it; the
	// setup-wizard API below reuses the same repo.
	runtimeSettingRepo := dbsql.NewGenericRepo[sharedentities.RuntimeSetting](deps.Db)

	// Unified notification feed for the control plane: events nodes push up their
	// control channels (alerts, health, system, going-offline) land here so an
	// operator sees fleet activity in one place. Reuses the shared notification
	// engine (persist + log + live SSE).
	notificationRepo := dbsql.NewGenericRepo[sharedentities.Notification](deps.Db)
	rollupRepo := dbsql.NewGenericRepo[sharedentities.NotificationRollup](deps.Db)
	notificationService := notification.NewService(notificationRepo,
		notification.Options{Logger: deps.Logger, Metrics: deps.Metrics}).
		WithRollups(rollupRepo)
	apis.NewNotificationApi(api, *deps.Auth, controlSession, notificationService)

	// Fold the feed into hourly rollups (the heatmap/baseline substrate). The first
	// sweep backfills every historical row past the persisted cursor, so fleets that
	// upgraded onto this build get their history scored, not just new events.
	// Leader-gated: the sweep is a read-cursor → page → increment → write-cursor cycle
	// with no locking, so two instances sweeping the same database fold the same page
	// twice and every bucket is quietly overcounted. These buckets ARE the heatmap, the
	// baselines and the anomaly detection, so the corruption would only surface as
	// numbers somebody eventually stopped trusting.
	rollupMaintainer := notification.NewRollupMaintainer(notificationRepo, rollupRepo,
		services.NewRollupCursor(runtimeSettingRepo), 0, 0).
		WithGate(deps.Leader.IsLeader)
	rollupMaintainer.Start(bgCtx)

	// Retention purge for the feed. Without it the control plane's notifications table
	// grows unbounded (every node event lands here forever). Retention is config-driven
	// (notification.retentionDays; 0 keeps everything) and the loop is supervised — a
	// dead purge loop is invisible until the disk fills.
	periodic(bgCtx, "myseliasan.purge.notifications", purgeInterval(deps.Config.Notification.PurgeIntervalHours), leaderOnly(deps.Leader, func(ctx context.Context) {
		days := deps.Config.Notification.RetentionDays
		if days <= 0 {
			return
		}
		if deleted, err := notificationService.PurgeOlderThanDays(ctx, days, deps.Config.Notification.PurgeReadOnly); err != nil {
			deps.Logger.Warnf("myseliasan.notification", "notification purge failed: %v", err)
		} else if deleted > 0 {
			deps.Metrics.Add(services.MetricNotificationsPurgedTotal, nil, float64(deleted))
			deps.Logger.Infof("myseliasan.notification", "purged %d expired notifications", deleted)
		}
	}))

	// On-demand printable PDF reports (fleet health, site/asset inventory, security &
	// access, incident detail). Rendered pure-Go (domain/report) so generation needs no
	// headless browser — the control plane runs air-gapped. The security report is
	// superadmin-gated inside the API; every generation is written to the audit trail.
	// Fleet AI agent, part 1: the LLM runtime and the digest service. Built before
	// the reports so their executive-summary section can use the same narrator +
	// optional model. (The chat service and /api/agent routes follow the control
	// server below — chat needs its connectivity oracle; these do not.)
	llmDir := apphost.ResolveWritablePath(deps.DataDir, "llm")
	agentCfg := deps.Config.Agent
	llmSidecar := services.NewLLMSidecar(services.SidecarConfig{
		Enabled:    strings.EqualFold(strings.TrimSpace(agentCfg.LLM.Mode), "sidecar"),
		Port:       agentCfg.LLM.Sidecar.Port,
		CtxSize:    agentCfg.LLM.Sidecar.CtxSize,
		Threads:    agentCfg.LLM.Sidecar.Threads,
		BinaryPath: agentCfg.LLM.Sidecar.BinaryPath,
		ModelPath:  agentCfg.LLM.Sidecar.ModelPath,
	}, llmDir, func(f string, a ...any) { deps.Logger.Infof("myseliasan.agent", f, a...) })
	llmSidecar.SetOnRestart(func() { deps.Metrics.Inc(services.MetricAgentSidecarRestartsTotal, nil) })
	llmSidecar.Start(bgCtx)
	llmManager := services.NewLLMManager(agentCfg.LLM, llmSidecar)
	llmInstaller := services.NewLLMInstaller(llmDir, llmSidecar,
		func() bool { return agentCfg.AllowDownloads == nil || *agentCfg.AllowDownloads },
		func(f string, a ...any) { deps.Logger.Warnf("myseliasan.agent", f, a...) })
	llmInstaller.SetOnResult(func(artifact, method string, ok bool) {
		outcome := "ok"
		if !ok {
			outcome = "failed"
		}
		deps.Metrics.Inc(services.MetricAgentInstallTotal,
			telemetry.Labels{"artifact": artifact, "method": method, "outcome": outcome})
	})
	digestService := services.NewDigestService(deps.Db, notificationService, registry,
		auditService, llmManager, func() config.AgentConfigModel { return deps.Config.Agent },
		deps.Metrics, func(f string, a ...any) { deps.Logger.Infof("myseliasan.agent", f, a...) })

	reportService := services.NewReportService(registry, siteService, notificationService,
		auditService, userService, roleService, deps.AccessPerms, digestService, availabilityService)
	apis.NewReportsApi(api, *deps.Auth, controlSession, reportService, auditService)

	// In-app editor for the safe subset of config.json (localAuth, SSO, pairing, security,
	// storage, logging) plus a process restart. Superadmin-gated. Because the shared host
	// reads these infra blocks only at boot, an edit is written back to config.json and takes
	// effect on the next restart — see services/settings_materialize.go for why the DB alone
	// can't apply them. The first-run defaults snapshot is encrypted with the same fleet cipher.
	settingsService := services.NewSettingsService(deps.Config, deps.ConfigPath, deps.Db, secretCipher,
		func(f string, a ...any) { deps.Logger.Warnf("myseliasan.settings", f, a...) })
	apis.NewSettingsApi(api, *deps.Auth, controlSession, settingsService, auditService, []string{deps.DataDir, deps.HomeDir})
	// Factory reset. Wipes the control plane back to first-run: the database (which holds
	// the fleet, its users, sites and every runtime setting) is dropped and reseeded, the
	// uploaded floor plans and cached basemaps are erased, and the fleet secret key is
	// crypto-erased so the encrypted CA key and PSK it protected are unrecoverable.
	//
	// ADOPTED NODES ARE NOT TOLD. Dropping the fleet here does not reach out to them, so
	// every node keeps running with a certificate this control plane no longer recognises
	// and has to be re-adopted. That is the honest behaviour for a local wipe -- a reset
	// that tried to notify nodes would hang on unreachable ones -- but it is why the
	// confirmation names the app and the UI spells the consequence out.
	//
	// Hidden unless bootstrap.allowReset is true, which myseliasan ships false.
	systemResetService := sharedservices.NewSystemResetService(sharedservices.SystemResetConfig{
		ConfirmPhrase: m.Name(),
		CollectDataPaths: func(context.Context) []string {
			return []string{
				planDir,
				apis.ResolveBasemapDir(deps.DataDir, ""),
				strings.TrimSpace(deps.Config.FileStorage.Path),
				// The AI sidecar's binaries and model files (<dataDir>/llm) — a
				// factory-reset control plane must not keep a 1 GB model around.
				apphost.ResolveWritablePath(deps.DataDir, "llm"),
			}
		},
		BootstrapOpts: sharedservices.ResetBootstrapOptions(
			m.Name(), deps.Config, m.Entities(), m.Migrations(), m.Seeders(deps.Config.Bootstrap.SeedStatements)),
		Restarter: deps.Restarter,
		KeyStore:  secretKeyStore,
		StopServices: func() {
			// Stop the pollers and the node-facing listeners before the wipe so nothing
			// writes to the database or re-enrols a node while it is being dropped.
			stopBackground()
		},
		CloseDatabase: func() error {
			if c, ok := deps.Db.(io.Closer); ok {
				return c.Close()
			}
			return nil
		},
		Logf: func(f string, a ...any) { deps.Logger.Infof("myseliasan.reset", f, a...) },
	})
	// Shed load with a clean 503 while the reset runs: it closes the database pool before
	// restarting, so every DB-backed request would otherwise return a raw 500 and the
	// SPA's overlay would look like a crash. The reset's own routes stay reachable.
	api.Use(sharedapis.NewResetGate(systemResetService))

	apis.NewSystemApi(api, *deps.Auth, controlSession, deps.Restarter, systemResetService)

	// First-run setup wizard completion flag (shared runtime-setting row, the same
	// contract mymatasan and myidsan use).
	setupStateService := sharedservices.NewSetupStateService(runtimeSettingRepo)
	apis.NewSetupApi(api, *deps.Auth, controlSession, setupStateService)

	// Backup & Restore. This is the ONLY way the fleet certificate authority leaves this
	// machine: the CA private key lives in a control_setting row, and every node's trust
	// chain hangs off it, so losing the database without a copy of this file means
	// physically re-adopting every node with a fresh claim code.
	//
	// It is built here rather than beside the other settings routes because it needs
	// setupStateService (a restored instance is already configured and must not be sent
	// back through the first-run wizard) and planDir + secretCipher (floor plan images are
	// encrypted on disk with the same key as the CA, and are unsealed on export / re-sealed
	// on restore so a bundle can move between hosts with different at-rest keys).
	backupVersion := ""
	if manifest, err := versioning.LoadDefault(); err == nil {
		if info, err := manifest.InfoForApp(m.Name()); err == nil {
			backupVersion = info.AppVersion
		}
	}
	backupService := services.NewBackupService(deps.Db, secretCipher, planDir, setupStateService, backupVersion)
	apis.NewBackupApi(api, *deps.Auth, controlSession, backupService, auditService)
	// The built-in manual. No auth middleware, deliberately — it has to be readable from the
	// sign-in screen and the first-run wizard. See apis.NewManualApi.
	apis.NewManualApi(api)

	// Deployment mode + cluster-readiness checklist. myseliasan is one of the two apps
	// in the suite that can genuinely run behind a load balancer (it is stateless over
	// its database); the checklist reports what this instance still needs, including the
	// parts only an operator can do. The env is rebuilt per request rather than captured
	// so the settings editor changing cache.provider is reflected without a restart.
	deploymentModeService := sharedservices.NewDeploymentModeService(runtimeSettingRepo)
	apis.NewDeploymentApi(api, *deps.Auth, controlSession, deploymentModeService,
		func() sharedservices.DeploymentEnv {
			return sharedservices.DeploymentEnv{
				DbEngine:           deps.Config.Db.Engine,
				CacheProvider:      deps.Config.Cache.Provider,
				LockProvider:       deps.Config.Transaction.LockProvider,
				JwtSecret:          deps.Config.Jwt.Secret,
				JwtSecretGenerated: deps.JwtSecretGenerated,
				MaxOpenConns:       deps.Config.Db.Pool.MaxOpenConns,
				// The fleet CA key and PSK are sealed with this key. Two instances holding
				// different ones look healthy until one reads the other's rows, at which
				// point the fleet's trust is simply gone — so the fingerprint is surfaced
				// for an operator to compare between instances.
				AtRestEnabled:     secretKeyStore.Enabled(),
				AtRestFingerprint: secretKeyStore.Fingerprint(),
				CachePing:         sharedservices.PingFunc(deps.Cache),
				LockPing:          sharedservices.PingFunc(deps.Locker),
				ExtraChecks:       llmSidecarDeploymentCheck(deps),
			}
		})

	// Control channel server: a dedicated fleet-mTLS listener accepting the
	// persistent, node-dialed bi-directional channel. Connection presence bumps a
	// node online (stronger liveness than the heartbeat poll, which still runs as a
	// fallback). It tunnels parent→node commands and ingests node→parent events.
	// The cross-domain correlator. THIS is the reason the fourth app exists.
	//
	//	motion on Camera 3 (a mymatasan node)
	//	AND a door contact opening (a myiotsan node)
	//	AND no badge swipe (a myiotsan node)
	//	-> intrusion
	//
	// No node can see that. mymatasan cannot see your door sensors; myiotsan cannot see your
	// cameras. Only the control plane, which already receives every node's events in one feed, is
	// in a position to notice the conjunction — and the conjunction is where the signal is. A
	// camera's motion alert at 03:00 is a moth; a door contact at 03:00 is a cleaner; the two
	// together with no badge swipe is an intrusion.
	correlator := services.NewCorrelator(deps.Db, notificationService,
		// The node's kind is resolved from the ADOPTED NODE'S RECORD — the authoritative answer to
		// "is this a camera or a sensor hub?", set from the claim-code-gated adopt reply. Trusting
		// a kind carried in the event body would let a node claim to be something it is not, and a
		// rule scoped to "a camera" could then be satisfied by a door sensor.
		func(ctx context.Context, nodeId string) string {
			nodes, err := registry.List(ctx)
			if err != nil {
				return ""
			}
			for _, n := range nodes {
				if n.NodeId == nodeId {
					if n.Kind == "" {
						return "camera" // every node adopted before the field existed is a camera
					}
					return n.Kind
				}
			}
			return ""
		},
		func(f string, a ...any) { deps.Logger.Infof("myseliasan.correlate", f, a...) })
	correlator.SetMetrics(deps.Metrics)
	// Recurrence context on fired rules ("also fired 3 times this week") —
	// deterministic feed queries under the correlator's hard enrich timeout.
	correlator.SetEnricher(services.NewFleetRuleEnricher(notificationService))
	// The digest's suggested-rule detector skips patterns an existing rule covers.
	digestService.SetRuleChecker(correlator.HasRuleFor)
	if err := correlator.Reload(context.Background()); err != nil {
		stopBackground()
		return nil, fmt.Errorf("load fleet rules: %w", err)
	}
	apis.NewFleetRulesApi(api, *deps.Auth, controlSession, correlator)

	// observeIfLeader feeds the correlator only on the instance that will actually fire.
	//
	// Arming and firing have to live on the SAME instance. Observing everywhere while
	// sweeping only on the leader looked harmless — every instance would keep warm state —
	// but the sweep is also what CLEARS an armed rule once its grace window passes. A
	// follower therefore accumulated armed rules that were never swept, and the moment it
	// was promoted it fired a backlog of correlations whose evidence was long gone.
	//
	// A promoted instance now starts with an empty armed set and rebuilds it from live
	// events, so the worst case is a correlation spanning the exact moment of a leadership
	// change being missed — far better than a burst of stale alerts.
	observeIfLeader := func(nodeID, kind string, body []byte) {
		if !deps.Leader.IsLeader() {
			return
		}
		observeForCorrelation(context.Background(), correlator, nodeID, kind, body)
	}

	// The sweep is what makes an ABSENCE decidable: nothing arrives to tell you the badge was
	// never swiped, so the passage of time has to.
	//
	// Leader-gated so an armed rule fires ONCE rather than once per instance — a fleet rule
	// firing raises an alert and can actuate, so duplicates are not cosmetic.
	//
	// KNOWN GAP, not fixed by this guard: each instance only ever sees events from the nodes
	// whose control channels terminate on it, and the armed set is in process memory. A rule
	// whose clauses span nodes attached to DIFFERENT instances therefore never arms at all —
	// it goes quiet rather than firing twice, which is the more dangerous direction. Closing
	// it needs every ingested node event on a shared bus (Phase 3); until then a multi-instance
	// deployment should be treated as unable to correlate across instance boundaries.
	leaderTicker(bgCtx, deps.Leader, time.Second, func(ctx context.Context) {
		correlator.Sweep(ctx)
	})

	// Dedup ledger for node-relayed notifications: keys each ingested node event by the node's
	// stable engine id so the reconnect replay (below) never re-publishes one already delivered
	// live or by an earlier replay.
	relayDedup := services.NewRelayDedup(deps.Db)

	// Cross-instance event fan-out. A node's events reach only the instance holding its
	// control channel; every other instance is serving browsers (whose bell would never
	// move) and running the same fleet rules (which would never see the other half of a
	// condition). The bus carries each event to all of them. With the in-process provider —
	// the single-instance default — publisher and subscriber are the same process, so this
	// wiring runs unchanged and costs nothing.
	nodeEventBus, busProvider, busErr := eventbus.New(eventbus.Config{
		Provider:       deps.Config.Cluster.EventBusProvider(deps.Config.Cache.Provider),
		KeyPrefix:      deps.Config.Cache.KeyPrefix,
		AppName:        m.Name(),
		RedisAddress:   deps.Config.Cache.Redis.Address,
		RedisPassword:  deps.Config.Cache.Redis.Password,
		RedisDB:        deps.Config.Cache.Redis.DB,
		RedisUseTLS:    deps.Config.Cache.Redis.UseTLS,
		ConnectTimeout: time.Duration(deps.Config.Cache.Redis.ConnectTimeoutMs) * time.Millisecond,
		CommandTimeout: time.Duration(deps.Config.Cache.Redis.OperationTimeoutMs) * time.Millisecond,
	})
	if busErr != nil {
		stopBackground()
		return nil, fmt.Errorf("event bus: %w", busErr)
	}
	if nodeEventBus.Distributed() {
		if err := nodeEventBus.Ping(context.Background()); err != nil {
			stopBackground()
			return nil, fmt.Errorf("event bus provider %s not reachable: %w", busProvider, err)
		}
		deps.Logger.Infof("myseliasan.cluster", "node-event bus provider=%s — the live feed and fleet rules span every instance", busProvider)
	}

	// instanceID marks who published an event, so an instance can ignore the echo of its
	// own message. Without it every event would be handled twice at its origin: once
	// locally and once off the bus.
	instanceID := services.NewInstanceID(deps.Config.Cluster.AdvertiseURL)

	busLog := func(f string, a ...any) { deps.Logger.Warnf("myseliasan.cluster", f, a...) }

	// Every notification this instance publishes — a node's, a node-lost alert the heartbeat
	// raised, an anomaly, the morning digest — is relayed to the other instances' live feeds.
	// Registered as a hub channel because the hub already invokes every channel on every
	// publish, which is exactly the set that should be relayed.
	notificationService.Register(services.NewNotificationRelayChannel(nodeEventBus, instanceID, busLog))
	services.SubscribeNotifications(bgCtx, nodeEventBus, instanceID,
		func(n notification.Notification) {
			// Stream only, never persist: the origin already wrote the row, and writing it
			// again would duplicate the feed and double-count the rollups.
			notificationService.RelayToStream(context.Background(), n)
		}, busLog)

	// Declared here and assigned once the control server exists (it needs the tunnel to
	// fetch with, and the tunnel needs the event handler below). The closure reads the
	// variable when a node event arrives, and no node can connect until the control
	// listener starts further down — so there is no window where this is nil in anger.
	var clipArchive services.IClipArchiveService

	onNodeEvent := func(nodeID, kind string, body []byte) {
		ingestNodeEvent(notificationService, relayDedup, clipArchive, nodeID, kind, body)
		// Feed the same event to the correlator. It is deliberately fed the NODE event rather
		// than the control plane's own re-published notification: correlating on our own output
		// would let a fleet rule's alert satisfy another fleet rule's clause, and two rules could
		// then trigger each other forever.
		observeIfLeader(nodeID, kind, body)
		// And to the other instances' correlators, so a rule whose conditions span nodes
		// attached to different instances can finally see them together. Notifications travel
		// separately, on their own topic, via the relay channel registered above.
		services.PublishNodeEvent(context.Background(), nodeEventBus, instanceID, nodeID, kind, body, busLog)
	}

	// The receiving half for correlation: raw events from OTHER instances.
	services.SubscribeNodeEvents(bgCtx, nodeEventBus, instanceID,
		func(ev services.NodeEventMessage) {
			observeIfLeader(ev.NodeID, ev.Kind, ev.Body)
		}, busLog)

	// Rule edits land on whichever instance the operator's browser reached, while the LEADER
	// is the instance that actually fires rules. Without this a new rule could sit unused,
	// and a disabled one keep firing, until something restarted — with nothing wrong on the
	// screen where the edit was made.
	correlator.SetOnRulesChanged(func() {
		services.PublishRulesChanged(context.Background(), nodeEventBus, instanceID, busLog)
	})
	services.SubscribeRulesChanged(bgCtx, nodeEventBus, instanceID, func() {
		if err := correlator.Reload(context.Background()); err != nil {
			deps.Logger.Warnf("myseliasan.cluster", "reloading fleet rules after another instance's edit failed: %v", err)
		}
	}, busLog)
	controlServer := services.NewControlServer(registry, p.ControlPort, onNodeEvent,
		func(format string, args ...any) { deps.Logger.Infof("myseliasan.control", format, args...) })
	// Replay-on-reconnect: the live push (above) drops any notification a node publishes while
	// its control channel is down, and nothing backfills it — so the control plane's feed could
	// undercount a busy node. When a node (re)connects, pull the notifications it created within
	// the replay window and ingest the ones we're missing (relayDedup makes this idempotent).
	// Which instance holds each node's control channel. Backed by the shared cache, so with
	// the single-instance default it is an in-process map (this instance owns everything it
	// is connected to, and there is nobody to forward to) and behind a load balancer it is
	// the deployment-wide answer every instance reads.
	clusterCfg := deps.Config.Cluster
	nodeOwners := services.NewNodeOwnerRegistry(deps.Cache, clusterCfg.AdvertiseURL,
		time.Duration(clusterCfg.OwnershipTTLSeconds)*time.Second,
		func(f string, a ...any) { deps.Logger.Warnf("myseliasan.cluster", f, a...) })
	nodeOwners.StartRenewal(bgCtx)

	// One client for every instance-to-instance hop — node commands, recording playback and
	// camera negotiation all present the same derived credential to the same peers.
	var clusterPeer *services.PeerClient
	if nodeOwners.Enabled() {
		clusterPeer = services.NewPeerClient(deps.Config.Jwt.Secret,
			time.Duration(clusterCfg.ForwardTimeoutSeconds)*time.Second, clusterCfg.InsecureSkipVerify)
		deps.Logger.Infof("myseliasan.cluster", "instance-to-instance node forwarding enabled; this instance advertises %s", clusterCfg.AdvertiseURL)
	}

	// Critical-clip archive: the fleet's own copy of the footage a rule was flagged to
	// keep. Built here because it needs the control tunnel to pull clips over and the
	// server's presence oracle to know when a node is even reachable; the ingest path
	// above enqueues into it.
	clipDir := apphost.ResolveWritablePath(deps.DataDir, "clips")
	clipArchive = services.NewClipArchiveService(deps.Db, controlServer, registry,
		controlServer.IsConnected, secretCipher, clipDir,
		func(n notification.Notification) { notificationService.Publish(context.Background(), n) },
		func(f string, a ...any) { deps.Logger.Infof("myseliasan.clips", f, a...) })
	apis.NewClipsApi(api, *deps.Auth, controlSession, clipArchive, auditService)

	controlServer.SetOnConnect(func(nodeID string) {
		// Claimed before the replay: from this moment the node's commands can be served
		// here, and the other instances need to know that as early as possible.
		nodeOwners.Claim(context.Background(), nodeID)
		replayNodeNotifications(controlServer, notificationService, relayDedup, clipArchive, nodeID,
			func(f string, a ...any) { deps.Logger.Infof("myseliasan.notif-replay", f, a...) })
	})
	// Withdraw the claim as soon as the channel drops, so another instance can take the node
	// over immediately instead of waiting out the ownership lease.
	controlServer.SetOnDisconnect(func(nodeID string) {
		nodeOwners.Release(context.Background(), nodeID)
	})
	// Keep the dedup ledger bounded: prune markers older than twice the replay window — a
	// windowed pull can never re-offer them, so they are dead weight. Leader-gated: it is a
	// whole-table cleanup, and N instances deleting the same rows only multiplies the work.
	leaderTicker(bgCtx, deps.Leader, time.Hour, func(ctx context.Context) {
		cutoff := time.Now().Add(-2 * notifReplayWindow).Unix()
		if _, err := relayDedup.Prune(ctx, cutoff); err != nil {
			deps.Logger.Warnf("myseliasan.notif-replay", "prune dedup ledger: %v", err)
		}
	})
	// The persistent node-dialed control channel is the authoritative liveness signal:
	// a node holding a live connection is online even when the parent cannot reach its
	// mTLS port directly. Wire its presence into the heartbeat reconciler so the mTLS
	// poll becomes a fallback that can no longer flap a control-connected node offline.
	m.controlServer = controlServer
	// Presence is asked of the OWNER REGISTRY, not of this instance's connection map.
	//
	// That is the whole point of the registry. Asking locally means the heartbeat sees only
	// its own nodes, falls back to the mTLS probe for everyone else, and — where that probe
	// cannot reach them — marks perfectly healthy nodes attached to another instance "lost"
	// and raises an operator alert for each one. Standalone the two answers are identical,
	// because everything this instance is connected to is everything there is.
	registry.SetControlPresence(func(nodeID string) bool {
		return nodeOwners.ConnectedAnywhere(context.Background(), nodeID)
	})
	// Surface nodes the control channel refuses (stranded: valid cert, no record) so an
	// operator can see and block them. Wired now that the control server exists.
	nodesApi.SetRejectTracker(controlServer)
	// Proactive fleet-health alerting: the heartbeat reconciler detects a node dropping
	// to "lost", recovering, or a certificate nearing expiry and hands each transition
	// to this sink, which surfaces it in the unified notification feed (so a
	// crashed/partitioned node no longer fails silently). Set before the heartbeat loop.
	services.DescribeMyseliasanMetrics(deps.Metrics)
	registry.SetFleetEventSink(func(e services.FleetEvent) {
		publishFleetEvent(notificationService, e)
		// Count the transition. A node dropping off looks, in the UI, identical to one an
		// operator released on purpose; a certificate creeping toward expiry has no UI symptom at
		// all. The counter is the only place a burst of either becomes visible.
		if deps.Metrics != nil {
			deps.Metrics.Inc(services.MetricFleetEventsTotal, telemetry.Labels{"kind": fleetEventKind(e.Kind)})
		}
	})
	go controlServer.Run(bgCtx)

	// Fleet-health gauges: how much of the adopted fleet is actually reachable right now, and
	// whether the control channel is even serving. Sampled off the control server so the accept
	// path stays free of a metrics lock.
	services.RunFleetMetricsSampler(bgCtx, deps.Metrics, controlServer, func(ctx context.Context) int {
		nodes, err := registry.List(ctx)
		if err != nil {
			return 0
		}
		return len(nodes)
	}, 10*time.Second)

	// Heartbeat reconciliation: every interval, reconcile each adopted node's liveness —
	// control-channel presence first, then the mTLS poll as a fallback — converging the
	// registry after self-drops and bounded by a grace window so brief reconnects don't
	// show offline.
	//
	// Leader-gated so there is a SINGLE writer of node status. Unguarded, every instance
	// reconciles the same rows from its own partial view and they overwrite each other,
	// so a node's status flaps and the lost/recovered transitions — which raise operator
	// alerts — fire once per instance.
	//
	// KNOWN GAP, not fixed by this guard: control-channel presence is an in-process map,
	// so the leader sees only the nodes whose channels terminate on ITSELF. Nodes attached
	// to another instance fall through to the mTLS probe, and where that cannot reach them
	// (NAT, firewall) the leader will eventually mark a healthy node "lost" and alert on it.
	// The guard makes that deterministic rather than flapping; it does not make it correct.
	// Correctness needs the node-owner registry (Phase 2), which turns presence into a
	// deployment-wide lookup. Until then, treat a multi-instance fleet's liveness as
	// reliable only for nodes attached to the leader.
	leaderTicker(bgCtx, deps.Leader, hbInterval, func(ctx context.Context) {
		registry.Heartbeat(ctx)
	})

	// Critical-clip archive worker. Leader-gated for the same reason the heartbeat is:
	// two instances working the same queue would pull the same clip twice, doubling the
	// tunnel traffic and racing on the file. A minute is brisk enough that a flagged
	// alert is off the appliance within a couple of minutes of its clip being cut, and
	// slow enough that a fleet with nothing to archive costs one indexed query a minute.
	leaderTicker(bgCtx, deps.Leader, time.Minute, func(ctx context.Context) {
		clipArchive.RunOnce(ctx)
	})
	// Retention. Hourly is ample for a 90-day window, and keeping it off the fetch path
	// means a slow purge can never delay getting evidence off an appliance.
	leaderTicker(bgCtx, deps.Leader, time.Hour, func(ctx context.Context) {
		if n, err := clipArchive.Purge(ctx, time.Now().Unix()); err != nil {
			deps.Logger.Warnf("myseliasan.clips", "clip retention sweep failed: %v", err)
		} else if n > 0 {
			deps.Logger.Infof("myseliasan.clips", "clip retention removed the media of %d archived clip(s)", n)
		}
	})

	// Fleet AI agent, part 2: the chat service (needs the control server's
	// connectivity oracle) and the /api/agent surface. The LLM runtime and digest
	// service were built earlier, before the reports that reuse them; the invariant
	// stands — the LLM is never in a critical path, and with mode "off" (default)
	// or the model down the digest still generates from the narrator.
	// The manual retriever is the chat's second grounding source and the docs endpoint's only
	// one. It reads embedded text and indexes lazily, so it costs nothing until a question is
	// asked and works with the LLM off.
	docsService := services.NewDocsService()
	chatService := services.NewChatService(notificationService, registry, digestService,
		controlServer.IsConnected, controlServer, docsService, llmManager, deps.Metrics,
		func(f string, a ...any) { deps.Logger.Warnf("myseliasan.agent", f, a...) })
	apis.NewAgentApi(api, *deps.Auth, controlSession, digestService, chatService, docsService,
		llmManager, llmInstaller, llmSidecar, auditService,
		func() apis.AgentDigestStatus {
			c := deps.Config.Agent.Digest
			hour := 7
			if c.LocalHour != nil && *c.LocalHour >= 0 && *c.LocalHour <= 23 {
				hour = *c.LocalHour
			}
			window := c.WindowHours
			if window <= 0 {
				window = 24
			}
			lastRun := func(key string) string {
				if row, err := runtimeSettingRepo.GetByUnique(context.Background(), "", "key", key); err == nil && row != nil {
					return row.Value
				}
				return ""
			}
			return apis.AgentDigestStatus{
				Enabled:           c.Enabled == nil || *c.Enabled,
				LocalHour:         hour,
				WindowHours:       window,
				LastRunDate:       lastRun("agent.digest.lastRun"),
				WeeklyEnabled:     c.WeeklyEnabled != nil && *c.WeeklyEnabled,
				Weekday:           c.Weekday,
				LastWeeklyRunDate: lastRun("agent.digest.lastWeeklyRun"),
			}
		})
	services.RunDigestSchedule(bgCtx, digestService, runtimeSettingRepo,
		func() config.AgentConfigModel { return deps.Config.Agent },
		deps.Leader.IsLeader,
		func(f string, a ...any) { deps.Logger.Infof("myseliasan.agent", f, a...) })
	// Stored-digest retention, daily.
	periodic(bgCtx, "myseliasan.purge.digests", 24*time.Hour, leaderOnly(deps.Leader, func(ctx context.Context) {
		days := deps.Config.Agent.Digest.RetentionDays
		if days == 0 {
			days = 180
		}
		if deleted, err := digestService.PurgeOld(ctx, days); err != nil {
			deps.Logger.Warnf("myseliasan.agent", "digest retention purge failed: %v", err)
		} else if deleted > 0 {
			deps.Logger.Infof("myseliasan.agent", "purged %d expired digests", deleted)
		}
	}))

	// Per-node access: a role gets full access to nodes it adopted (owner role) and
	// whatever explicit grants it has elsewhere. Drives the tunnel's viewer/admin
	// decision and gates grant management.
	accessService := services.NewNodeAccessService(deps.Db, roleService)
	apis.NewNodeAccessApi(api, *deps.Auth, accessService, controlSession, auditService)

	// Node camera media relay: a dedicated fleet-mTLS listener accepts the node-dialed
	// media channel; per browser WebRTC subscription it asks the node to stream that
	// camera's RTP, then re-broadcasts it to the browser (the browser talks only to
	// myseliasan). The WebRTC engine advertises configured public IPs / a fixed UDP
	// port (and offers STUN/TURN) so the browser↔parent leg works across networks;
	// empty config = host candidates (same-LAN/local dev).
	mediaEngine, engErr := stream.NewWebRTCEngine(deps.Config.NodeStream.PublicIPs, deps.Config.NodeStream.UDPPort)
	if engErr != nil {
		stopBackground()
		return nil, fmt.Errorf("webrtc engine: %w", engErr)
	}
	mediaICE := make([]stream.ICEServer, 0, len(deps.Config.NodeStream.ICEServers))
	for _, s := range deps.Config.NodeStream.ICEServers {
		if len(s.URLs) == 0 {
			continue
		}
		mediaICE = append(mediaICE, stream.ICEServer{URLs: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	mediaHub := services.NewMediaRelayHub(func(format string, args ...any) { deps.Logger.Infof("myseliasan.media", format, args...) })
	mediaPort := p.MediaPort
	if mediaPort <= 0 {
		mediaPort = 49534
	}
	// Advisory readiness: track whether the media listener's serve loop is active so
	// /api/ready can surface it (it never gates the process's db/cache readiness).
	m.mediaListening = &atomic.Bool{}
	go func() {
		tlsCfg, terr := registry.ParentServerTLS(bgCtx)
		if terr != nil {
			deps.Logger.Warnf("myseliasan.media", "media listener TLS unavailable: %v", terr)
			return
		}
		srv := mediarelay.NewServer(fmt.Sprintf(":%d", mediaPort), tlsCfg, mediaHub.HandleConn,
			func(format string, args ...any) { deps.Logger.Infof("myseliasan.media", format, args...) })
		deps.Logger.Infof("myseliasan.media", "media channel listening on :%d", mediaPort)
		m.mediaListening.Store(true)
		defer m.mediaListening.Store(false)
		if rerr := srv.Run(bgCtx); rerr != nil {
			deps.Logger.Warnf("myseliasan.media", "media server stopped: %v", rerr)
		}
	}()
	// Which instance holds each node's MEDIA channel. Tracked separately from the control
	// channel: a node opens both, and nothing guarantees they land on the same instance.
	mediaOwners := services.NewMediaOwnerRegistry(deps.Cache, clusterCfg.AdvertiseURL,
		time.Duration(clusterCfg.OwnershipTTLSeconds)*time.Second,
		func(f string, a ...any) { deps.Logger.Warnf("myseliasan.cluster", f, a...) })
	mediaOwners.StartRenewal(bgCtx)
	mediaHub.SetOwnershipHooks(
		func(nodeID string) { mediaOwners.Claim(context.Background(), nodeID) },
		func(nodeID string) { mediaOwners.Release(context.Background(), nodeID) },
	)

	// Forward a browser's WebRTC offer to the instance holding the node's media channel.
	// Only the NEGOTIATION crosses instances — the answer carries the owning instance's own
	// ICE candidates, so the browser then peers with it directly and the video never
	// traverses the load balancer or a second instance. Nil when clustering is off.
	var mediaForward func(context.Context, services.MediaOfferRequest) (services.MediaOfferReply, error)
	if mediaOwners.Enabled() && clusterPeer != nil {
		mediaForward = func(ctx context.Context, req services.MediaOfferRequest) (services.MediaOfferReply, error) {
			owner, isLocal := mediaOwners.OwnerOf(ctx, req.NodeID)
			if owner == "" || isLocal {
				return services.MediaOfferReply{}, services.ErrMediaNotConnected
			}
			deps.Logger.Infof("myseliasan.cluster", "forwarding camera offer for node %s to %s", req.NodeID, owner)
			return clusterPeer.ForwardMediaOffer(ctx, owner, req)
		}
	}

	// Registered before the proxy catch-all so the specific media-offer path wins.
	mediaApi := apis.NewNodeMediaApi(api, *deps.Auth, mediaHub, accessService, mediaEngine, mediaICE, controlSession, mediaForward)
	if mediaOwners.Enabled() {
		// The receiving half: answer offers other instances forward here, using this
		// instance's own media connection and WebRTC engine.
		api.Handle(services.PeerMediaOfferPath,
			services.NewPeerMediaOfferHandler(deps.Config.Jwt.Secret, mediaApi.AnswerLocalOffer,
				func(f string, a ...any) { deps.Logger.Warnf("myseliasan.cluster", f, a...) })).Methods("POST")
	}

	// Reverse command tunnel: /api/nodes/{id}/proxy/<node-path> forwards over the
	// control channel to the node's own API, giving the commander the node's exact
	// capability surface. The operator's per-node grant decides viewer vs admin (no
	// read access → 403). Registered after NewNodesApi/NewNodeAccessApi so their
	// specific routes win; mux falls through to the proxy for /nodes/{id}/proxy/...
	// Range-capable recording playback over the tunnel (chunks each byte range under the
	// control-channel message cap). Registered before the generic proxy so its specific
	// /nodes/{id}/recording-stream/{segId} route wins.
	// Cluster-aware delivery. Both surfaces below already depend on the narrow
	// ControlSender interface, so wrapping it once teaches them to reach a node attached to
	// ANOTHER instance without either of them learning that instances exist. Standalone the
	// wrapper is a pass-through: everything is owned locally or owned nowhere.
	//
	// Live camera video is handled separately (see the media-owner registry above), because
	// the media channel is its own connection and only the NEGOTIATION is forwarded — the
	// video itself goes browser-to-owning-instance directly.
	nodeSender := services.ControlSender(controlServer)
	if nodeOwners.Enabled() && clusterPeer != nil {
		nodeSender = services.NewForwardingSender(controlServer, nodeOwners, clusterPeer,
			func(f string, a ...any) { deps.Logger.Infof("myseliasan.cluster", f, a...) })
		// The receiving half. Given the LOCAL sender on purpose — an instance named as the
		// owner either holds the connection or the claim is stale, and forwarding onward
		// from here would let a stale claim bounce a request between instances.
		peerHandler := services.NewPeerForwardHandler(deps.Config.Jwt.Secret, controlServer,
			func(f string, a ...any) { deps.Logger.Warnf("myseliasan.cluster", f, a...) })
		api.Handle(services.PeerForwardPath, peerHandler).Methods("POST")
	}

	apis.NewRecordingStreamApi(api, *deps.Auth, nodeSender, accessService, controlSession)

	// Federated cross-node search. Registered BEFORE the node proxy so /api/nodes/search
	// is matched here; the proxy's catch-all only claims /api/nodes/{id}/proxy/..., but
	// keeping the specific route ahead of the prefix route is what makes that independent
	// of how gorilla orders its fallbacks.
	fleetSearchService := services.NewFleetSearchService(registry, siteService, nodeSender, accessService)
	apis.NewFleetSearchApi(api, *deps.Auth, controlSession, fleetSearchService, auditService)

	apis.NewNodeProxyApi(api, *deps.Auth, nodeSender, accessService, controlSession, auditService)

	// Fleet configuration policy. Wired HERE, after nodeSender exists, because the
	// reconciler reads and writes node settings through the same tunnel the operator's own
	// node screens use — it gets no private path to an appliance.
	policyService := services.NewFleetPolicyService(deps.Db)
	policyReconciler := services.NewFleetPolicyReconciler(policyService, registry, nodeSender, auditService,
		func(f string, a ...any) { deps.Logger.Warnf("myseliasan.fleet-policy", f, a...) })
	apis.NewFleetPolicyApi(api, *deps.Auth, controlSession, policyService, policyReconciler, auditService)
	// Leader-gated: a sweep is a tunneled round trip per section per node, and N instances
	// all reconciling the same fleet would multiply that load — and, where a policy
	// enforces, race each other writing the same setting to the same appliance.
	//
	// Fifteen minutes because this is a slow-moving question. A node's configuration does
	// not change on its own; it changes when somebody changes it, and the cost of learning
	// about that ten minutes later is nil against the cost of every appliance in a large
	// estate answering a settings poll continuously.
	leaderTicker(bgCtx, deps.Leader, 15*time.Minute, func(ctx context.Context) {
		if _, err := policyReconciler.ReconcileAll(ctx); err != nil {
			deps.Logger.Warnf("myseliasan.fleet-policy", "reconcile sweep: %v", err)
		}
	})
	// One pass shortly after boot, so the screen has an answer before the first tick. It is
	// deliberately not immediate: nodes dial the control channel after this function
	// returns, and a sweep run now would report the entire fleet unreachable and store that
	// as the last known state.
	safego.Go("fleet-policy-initial", func() {
		select {
		case <-bgCtx.Done():
			return
		case <-time.After(90 * time.Second):
		}
		if !deps.Leader.IsLeader() {
			return
		}
		if _, err := policyReconciler.ReconcileAll(bgCtx); err != nil {
			deps.Logger.Warnf("myseliasan.fleet-policy", "initial reconcile: %v", err)
		}
	})

	return func(context.Context) error { stopBackground(); return nil }, nil
}

// ingestNodeEvent maps an event a node pushed up its control channel into the
// control plane's notification feed. "notification" events are re-published as-is
// (re-tagged with the node so the feed shows their origin and the parent assigns a
// fresh id); "going-offline" becomes a system warning. Any other kind (health,
// disk-full, alert, system, …) is surfaced rather than dropped: the frame is parsed
// as a notification when it carries one, otherwise wrapped in a generic message
// tagged with the raw kind — so a node reporting trouble is never silently lost.
// It returns the notification it published (if any) so a multi-instance deployment can
// relay that exact row to the other instances' live feeds. A false second return means
// nothing was published and nothing should be relayed.
func ingestNodeEvent(svc *notification.Service, dedup *services.RelayDedup, clips services.IClipArchiveService, nodeID, kind string, body []byte) (notification.Notification, bool) {
	switch kind {
	case "notification":
		var n notification.Notification
		if err := json.Unmarshal(body, &n); err != nil {
			return notification.Notification{}, false
		}
		return republishNodeNotification(svc, dedup, clips, nodeID, n)
	case "going-offline":
		return svc.Publish(context.Background(), notification.Notification{
			Category: notification.CategorySystem,
			Severity: notification.Warning,
			Title:    "Node going offline",
			Body:     "Node " + nodeID + " reported it is going offline.",
			Source:   "node:" + nodeID,
			Data:     map[string]any{"nodeId": nodeID},
		}), true
	default:
		// Unknown-but-present frame: prefer a structured notification payload, else
		// wrap the raw body so the operator still sees that the node reported something.
		var n notification.Notification
		if err := json.Unmarshal(body, &n); err == nil && (n.Title != "" || n.Body != "") {
			if n.Category == "" {
				n.Category = categoryForNodeKind(kind)
			}
			if n.Severity == "" {
				n.Severity = severityForNodeKind(kind)
			}
			return republishNodeNotification(svc, dedup, clips, nodeID, n)
		}
		return svc.Publish(context.Background(), notification.Notification{
			Category: categoryForNodeKind(kind),
			Severity: severityForNodeKind(kind),
			Title:    "Node " + kind + " event",
			Body:     truncateBody(string(body), 500),
			Source:   "node:" + nodeID,
			Data:     map[string]any{"nodeId": nodeID, "kind": kind},
		}), true
	}
}

// republishNodeNotification re-tags a node-originated notification with its origin node and
// lets the parent assign a fresh id in its own feed. It is called on BOTH paths — the live
// control-channel push and the reconnect replay pull — so it dedups on the node's stable engine
// id (n.ID, which is the same value on both paths): once a given node event has been ingested it
// is never published again, which is what makes replaying a disconnect window idempotent.
// It returns true when the notification was published, false when dedup suppressed it.
// It returns the notification as PUBLISHED (carrying the id this control plane assigned),
// so a multi-instance deployment can relay that exact row to the other instances' live
// feeds — see RelayToStream. A false second return means nothing was published, either
// because the dedup ledger had already seen it or because there was nothing to publish;
// nothing should be relayed in that case, or a peer's bell would show an event this
// instance deliberately dropped.
func republishNodeNotification(svc *notification.Service, dedup *services.RelayDedup, clips services.IClipArchiveService, nodeID string, n notification.Notification) (notification.Notification, bool) {
	originID := n.ID // the node's engine id; identical on the live push and a pulled row's __oid
	if dedup != nil && dedup.SeenOrRecord(context.Background(), nodeID, originID, n.CreatedAt) {
		return notification.Notification{}, false // already ingested (live or a prior replay)
	}
	n.ID = "" // parent assigns its own id in its own feed
	n.Source = "node:" + nodeID
	if n.Data == nil {
		n.Data = map[string]any{}
	}
	n.Data["nodeId"] = nodeID
	published := svc.Publish(context.Background(), n)
	// The archive hook lives HERE rather than at the live-event call site, because this
	// one function is also what the reconnect replay funnels through. A node that raised
	// a flagged alert while its channel was down has that alert backfilled minutes or
	// hours later, and the whole point of the feature is that THAT clip gets archived
	// too — hooking the live path alone would quietly archive only the easy half.
	if clips != nil {
		clips.Consider(context.Background(), nodeID, published)
	}
	return published, true
}

// notifReplayWindow bounds how far back a reconnect replay pulls a node's notifications. It must
// comfortably exceed a plausible disconnect; anything older is assumed already ingested (or not
// worth backfilling), and dedup markers past twice this are pruned.
const notifReplayWindow = 72 * time.Hour

// nodeNotifRow is the subset of a node's persisted notification the replay needs. Fields mirror
// domain/entities.Notification's JSON tags.
type nodeNotifRow struct {
	Category  string `json:"category"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	CameraId  int64  `json:"cameraId"`
	RefType   string `json:"refType"`
	RefId     int64  `json:"refId"`
	Link      string `json:"link"`
	Metadata  string `json:"metadata"`
	CreatedAt int64  `json:"createdAt"`
}

// parseNodeNotifRows extracts the items from a node's /api/notifications response, tolerating both
// the plain {result:{items}} envelope and a wrapping {data:{result:{items}}}.
func parseNodeNotifRows(body []byte) []nodeNotifRow {
	var env struct {
		Result *struct {
			Items []nodeNotifRow `json:"items"`
		} `json:"result"`
		Data *struct {
			Result struct {
				Items []nodeNotifRow `json:"items"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil
	}
	if env.Result != nil {
		return env.Result.Items
	}
	if env.Data != nil {
		return env.Data.Result.Items
	}
	return nil
}

// nodeRowToNotification rebuilds an in-memory notification from a pulled row, restoring the node's
// engine id (persisted under notification.OriginIDKey) as its ID so republish dedups on the same
// key the live push carries, and keeping the original timestamp.
func nodeRowToNotification(row nodeNotifRow) notification.Notification {
	data := map[string]any{}
	if row.Metadata != "" {
		_ = json.Unmarshal([]byte(row.Metadata), &data)
	}
	originID, _ := data[notification.OriginIDKey].(string)
	return notification.Notification{
		ID:        originID,
		Category:  row.Category,
		Severity:  notification.Severity(row.Severity),
		Title:     row.Title,
		Body:      row.Body,
		CameraId:  row.CameraId,
		RefType:   row.RefType,
		RefId:     row.RefId,
		Link:      row.Link,
		Data:      data,
		CreatedAt: row.CreatedAt,
	}
}

// replayNodeNotifications pulls a node's notifications from the replay window over the control
// tunnel and ingests the ones the control plane is missing. Idempotent via relayDedup: events
// already delivered live (or by an earlier replay) are skipped. Called on every (re)connect; a
// node that is offline or has nothing to replay is a cheap no-op.
func replayNodeNotifications(sender services.ControlSender, svc *notification.Service, dedup *services.RelayDedup, clips services.IClipArchiveService, nodeID string, logf func(string, ...any)) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cursor := time.Now().Add(-notifReplayWindow).Unix()
	ingested := 0
	for page := 0; page < 50; page++ { // hard cap 50*500 = 25k events/reconnect
		req := control.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/api/notifications?since=%d&limit=500", cursor),
			Role:   "admin",
			Actor:  "control-plane:notif-replay",
		}
		resp, err := sender.SendRequest(ctx, nodeID, req)
		if err != nil || resp.Status < 200 || resp.Status >= 300 {
			return
		}
		rows := parseNodeNotifRows(resp.Body)
		if len(rows) == 0 {
			break
		}
		maxTs := cursor
		for _, row := range rows {
			if _, published := republishNodeNotification(svc, dedup, clips, nodeID, nodeRowToNotification(row)); published {
				ingested++
			}
			if row.CreatedAt > maxTs {
				maxTs = row.CreatedAt
			}
		}
		if len(rows) < 500 || maxTs <= cursor {
			break // last page, or no time progress (a full page in one second) — stop
		}
		cursor = maxTs // re-includes same-second boundary rows; dedup drops them
	}
	if ingested > 0 && logf != nil {
		logf("replayed %d missed notification(s) from node %s", ingested, nodeID)
	}
}

// categoryForNodeKind maps a node event kind to a notification category so unknown
// frames still land in a sensible bucket.
func categoryForNodeKind(kind string) string {
	k := strings.ToLower(kind)
	switch {
	case strings.Contains(k, "health"), strings.Contains(k, "disk"), strings.Contains(k, "cert"):
		return notification.CategoryHealthCheck
	case strings.Contains(k, "alert"):
		return notification.CategoryVisionAlert
	default:
		return notification.CategorySystem
	}
}

// severityForNodeKind guesses an urgency for an unlabeled node event kind.
//
// Note this and categoryForNodeKind apply only to frames that are NOT full notifications — the
// live path (republishNodeNotification) preserves the node-authored category and severity, so a
// door node's duress alarm arrives Critical without any help from here. These heuristics are the
// fallback bucket, extended with the door-alarm vocabulary so a bare frame still lands loudly.
func severityForNodeKind(kind string) notification.Severity {
	k := strings.ToLower(kind)
	switch {
	case strings.Contains(k, "alert"), strings.Contains(k, "full"), strings.Contains(k, "critical"), strings.Contains(k, "fail"),
		strings.Contains(k, "duress"), strings.Contains(k, "forced"), strings.Contains(k, "tamper"):
		return notification.Critical
	case strings.Contains(k, "health"), strings.Contains(k, "warn"), strings.Contains(k, "disk"):
		return notification.Warning
	default:
		return notification.Info
	}
}

func truncateBody(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// publishFleetEvent turns a fleet-health transition the registry detected during
// reconciliation into a notification in the unified feed. Each event is tagged with
// its origin node ("node:<id>") so it attributes correctly in the dashboard's
// per-node breakdown alongside the events that node pushed itself.
func publishFleetEvent(svc *notification.Service, e services.FleetEvent) {
	node := e.Node
	if node == nil {
		return
	}
	name := node.Name
	if name == "" {
		name = node.NodeId
	}
	source := "node:" + node.NodeId
	data := map[string]any{"nodeId": node.NodeId}
	switch e.Kind {
	case services.FleetEventNodeLost:
		svc.Publish(context.Background(), notification.Notification{
			Category: notification.CategoryHealthCheck,
			Severity: notification.Critical,
			Title:    "Node offline",
			Body:     fmt.Sprintf("Node %q is unreachable — no control channel and no heartbeat past the grace window — and has been marked lost.", name),
			Source:   source,
			Data:     data,
		})
	case services.FleetEventNodeRecovered:
		svc.Publish(context.Background(), notification.Notification{
			Category: notification.CategoryHealthCheck,
			Severity: notification.Info,
			Title:    "Node back online",
			Body:     fmt.Sprintf("Node %q is reachable again.", name),
			Source:   source,
			Data:     data,
		})
	case services.FleetEventCertExpiring:
		data["certExpiresAt"] = e.ExpiresAt
		data["hoursLeft"] = e.HoursLeft
		var body string
		if e.HoursLeft <= 0 {
			body = fmt.Sprintf("Node %q certificate has expired; automatic re-enrollment is failing and the node cannot re-establish trust.", name)
		} else {
			body = fmt.Sprintf("Node %q certificate expires in about %s; automatic re-enrollment is overdue.", name, humanizeHours(e.HoursLeft))
		}
		svc.Publish(context.Background(), notification.Notification{
			Category: notification.CategoryHealthCheck,
			Severity: notification.Warning,
			Title:    "Node certificate expiring",
			Body:     body,
			Source:   source,
			Data:     data,
		})
	}
}

// llmSidecarDeploymentCheck contributes myseliasan's one app-specific preflight row.
//
// In sidecar mode every instance starts its OWN llama.cpp and loads its own copy of the
// model, so N instances cost N times the memory for one logical capability — and each
// downloads the model separately, which on a metered or air-gapped link is the part that
// actually hurts. External mode points them all at one inference endpoint.
//
// It is a warning, not a blocker: the deployment works, it is just wasteful, and an
// operator who genuinely wants a model per instance is entitled to that.
func llmSidecarDeploymentCheck(deps apphost.Dependencies) func(context.Context) []sharedservices.PreflightCheck {
	return func(context.Context) []sharedservices.PreflightCheck {
		mode := strings.ToLower(strings.TrimSpace(deps.Config.Agent.LLM.Mode))
		if mode == "" {
			mode = "off"
		}
		return []sharedservices.PreflightCheck{{
			Id:       "llmMode",
			Severity: sharedservices.SeverityWarning,
			Ok:       mode != "sidecar",
			Detail:   mode,
		}}
	}
}

// openFleetSecretCipher resolves the at-rest master key for fleet secrets, mirroring
// mymatasan's encryption-at-rest boot (infra/atrest). Returns nil (no encryption) when
// the feature is disabled. When a key that existed here before is now missing it FAILS
// CLOSED — refusing to boot rather than minting a new key and silently orphaning the
// encrypted CA key/PSK (which would reset the whole fleet's trust); restore the key
// file or configure security.recoveryPath and restart.
// The KeyStore is returned alongside the cipher so the factory reset can crypto-erase
// the key. Destroying it also clears the init marker, so the next boot reads as a clean
// first run and mints a fresh key rather than hitting the recovery gate above.
func openFleetSecretCipher(deps apphost.Dependencies) (*atrest.Cipher, *atrest.KeyStore, error) {
	if !boolValue(deps.Config.Security.EncryptAtRest, true) {
		return nil, nil, nil
	}
	keyPath := strings.TrimSpace(deps.Config.Security.KeyPath)
	if keyPath == "" {
		keyPath, _ = filepath.Abs(apphost.ResolveWritablePath(deps.DataDir, filepath.Join("secret", "atrest.key")))
	}
	recoveryPath := strings.TrimSpace(deps.Config.Security.RecoveryPath)
	if recoveryPath == "" {
		recoveryPath = filepath.Join(filepath.Dir(keyPath), "recovery.atrestkey")
	}
	protectorCfg := atrest.ProtectorConfig{
		Name:           deps.Config.Security.KeyProtector,
		Passphrase:     deps.Config.Security.Passphrase,
		PassphraseFile: deps.Config.Security.PassphraseFile,
		PassphraseEnv:  deps.Config.Security.PassphraseEnv,
	}
	outcome, err := atrest.OpenForStartup(keyPath, recoveryPath, protectorCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("fleet-secret encryption key: %w", err)
	}
	if outcome.Mode == atrest.ModeRecoveryPending {
		return nil, nil, fmt.Errorf("fleet-secret encryption key missing (id %s): restore %s or set security.recoveryPath, then restart — refusing to reset fleet trust", outcome.KeyId, keyPath)
	}
	deps.Logger.Infof("myseliasan.security", "fleet-secret encryption enabled (key %s, mode %s, id %s)", keyPath, outcome.Mode, outcome.KeyId)
	return outcome.KeyStore.Cipher(), outcome.KeyStore, nil
}

// boolValue dereferences an optional bool config flag, using fallback when unset.
func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

// humanizeHours renders an hour count as a compact "Nd Mh" / "Nh" string.
func humanizeHours(hours int) string {
	if hours >= 48 {
		days := hours / 24
		rem := hours % 24
		if rem == 0 {
			return fmt.Sprintf("%d days", days)
		}
		return fmt.Sprintf("%dd %dh", days, rem)
	}
	return fmt.Sprintf("%d hours", hours)
}

func (m *module) RegisterWebRoutes(router *mux.Router, deps apphost.Dependencies) error {
	// Always serve the SPA; the app renders its own login screen (SSO button + local
	// stock-superadmin form) when /api/session/me reports no session. This replaces
	// the old straight-to-SSO redirect so the local bootstrap login is reachable.
	//
	// Resolve against deps.HomeDir, NOT BaseDir(): BaseDir() is the CWD-relative dev
	// path "apps/myseliasan", so a packaged install (where the binary and static/ sit
	// side by side and the service's working directory is elsewhere) would 404 on "/"
	// and "/index.html". apphost's SPA catch-all already uses HomeDir; this must match.
	staticIndex := filepath.Join(deps.HomeDir, "static", "index.html")
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		// index.html references content-hashed chunk files, so it MUST NOT be cached:
		// a stale index.html keeps the browser on an old bundle even after a rebuild
		// (which is exactly what left a fixed request-storm still running client-side).
		// The hashed .js/.css can still be cached immutably — only this entry points to them.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, staticIndex)
	}
	router.HandleFunc("/", serveIndex).Methods("GET")
	router.HandleFunc("/index.html", serveIndex).Methods("GET")
	return nil
}

func (m *module) APIDocs() apidocs.SpecConfig {
	docVersion := "1.0.0"
	if manifest, err := versioning.LoadDefault(); err == nil {
		if info, err := manifest.InfoForApp(m.Name()); err == nil {
			docVersion = info.AppVersion
		}
	}

	return apidocs.SpecConfig{
		Metadata: apidocs.Metadata{
			Title:       "myseliasan API",
			Version:     docVersion,
			Description: "Control plane app for mymatasan using MyIDSan federated SSO.",
		},
		Endpoints: map[string]apidocs.EndpointDoc{
			"GET /api/auth/start": {
				Summary:     "Start MyIDSan login",
				Description: "Redirects to MyIDSan authorization endpoint.",
				Tags:        []string{"auth"},
			},
			"GET /api/auth/callback": {
				Summary:     "Handle MyIDSan callback",
				Description: "Exchanges authorization code and creates the myseliasan session.",
				Tags:        []string{"auth"},
			},
			"POST /api/auth/logout": {
				Summary:     "Logout",
				Description: "Clears the myseliasan session cookie.",
				Tags:        []string{"auth"},
			},
			"GET /api/session/me": {
				Summary:     "Current session",
				Description: "Returns current authenticated user claims for the dashboard.",
				Tags:        []string{"session"},
			},
			"GET /api/deployment/preflight": {
				Summary: "Deployment readiness checklist",
				Description: "Reports what this instance can verify from the inside about running behind a load balancer: " +
					"database engine, shared cache and distributed lock (pinged, not merely name-checked), the at-rest " +
					"encryption key's fingerprint, whether the JWT signing secret was configured or self-generated, and the " +
					"per-instance database connection budget. The at-rest fingerprint and the pool budget are meant to be " +
					"COMPARED between instances — two instances reporting different key fingerprints cannot read what the " +
					"other sealed. Says nothing about what only an operator can check (TLS termination, shared storage).",
				Tags: []string{"deployment"},
			},
			"GET /api/deployment/mode": {
				Summary:     "Read the declared deployment mode",
				Description: "Returns the persisted `standalone` / `clustered` declaration and whether its caveats were acknowledged. A process cannot detect that it is one of several replicas, so this is a stated fact rather than an inference.",
				Tags:        []string{"deployment"},
			},
			"POST /api/deployment/mode": {
				Summary:     "Declare the deployment mode",
				Description: "Persists the deployment-mode declaration. Declaring `clustered` requires acknowledging the caveats that are not yet cluster-safe; the declaration is what turns the boot-time shared-state warning from a heuristic into a definite one.",
				Tags:        []string{"deployment"},
			},
			"POST /api/internal/cluster/node-forward": {
				Summary: "Instance-to-instance command forward (internal)",
				Description: "NOT a public API and not called by browsers. A node holds its control channel open to exactly one instance, " +
					"so an instance that receives a command for a node it does not own forwards it here, to the owning instance. " +
					"Authenticated by a token derived one-way from the shared `jwt.secret`, NOT by a user session — every " +
					"instance therefore already shares the credential. Only mounted when `cluster.advertiseUrl` is set.",
				Tags: []string{"cluster-internal"},
			},
			"POST /api/internal/cluster/media-offer": {
				Summary: "Instance-to-instance WebRTC offer forward (internal)",
				Description: "NOT a public API and not called by browsers. A node's media channel and its control channel are " +
					"independent connections that need not land on the same instance, so a WebRTC offer for a camera is forwarded " +
					"to whichever instance holds that node's media channel. Only the negotiation is forwarded — the media itself " +
					"flows directly between the browser and the owning instance, outside the load balancer. Same derived-token " +
					"authentication as node-forward; only mounted when `cluster.advertiseUrl` is set.",
				Tags: []string{"cluster-internal"},
			},
		},
	}
}

// fleetEventKind maps a fleet-event kind to a stable, low-cardinality metric label.
func fleetEventKind(k services.FleetEventKind) string {
	switch k {
	case services.FleetEventNodeLost:
		return "node_lost"
	case services.FleetEventNodeRecovered:
		return "node_recovered"
	case services.FleetEventCertExpiring:
		return "cert_expiring"
	default:
		return "other"
	}
}
