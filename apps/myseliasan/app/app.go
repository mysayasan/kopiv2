package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/control"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/mediarelay"
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
	}
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
		appentities.ManagedNode{},
		appentities.ControlSetting{},
		appentities.NodeAccessGrant{},
		appentities.ControlUser{},
		appentities.AuditLog{},
		sharedentities.AccessRole{},
		sharedentities.AccessRolePermission{},
		// Cross-domain correlation: the reason the suite has a fourth app.
		appentities.FleetRule{},
		appentities.FleetRuleClause{},
		// Fleet map: sites + uploaded floor plans (indoor view); node/camera placements.
		appentities.Site{},
		appentities.FloorPlan{},
		appentities.NodePlacement{},
		// Dedup ledger for node-relayed notifications (reconnect replay idempotency).
		appentities.RelayedNotif{},
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
		{Title: "Placements", Description: "reposition/remove node and camera markers on floor plans", Path: "/api/placements", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Node Floorplan", Description: "floor plans holding a node camera markers (geo-map drill-down)", Path: "/api/node-floorplan", AccessTier: apiaccessenums.AuthOnly},
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
	secretCipher, secErr := openFleetSecretCipher(deps)
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
	nodesApi := apis.NewNodesApi(api, *deps.Auth, controlSession, registry, auditService,
		func(f string, a ...any) { deps.Logger.Warnf("myseliasan.nodes", f, a...) })

	// Sites + floor plans (indoor map). Plan images are encrypted at rest with the same
	// fleet cipher that protects the CA key/PSK, stored under <dataDir>/floorplans.
	planDir := apphost.ResolveWritablePath(deps.DataDir, "floorplans")
	siteService := services.NewSiteService(deps.Db, secretCipher, planDir)
	apis.NewSitesApi(api, *deps.Auth, controlSession, siteService)

	bgCtx, stopBackground := context.WithCancel(context.Background())

	// Unified notification feed for the control plane: events nodes push up their
	// control channels (alerts, health, system, going-offline) land here so an
	// operator sees fleet activity in one place. Reuses the shared notification
	// engine (persist + log + live SSE).
	notificationRepo := dbsql.NewGenericRepo[sharedentities.Notification](deps.Db)
	notificationService := notification.NewService(notificationRepo, notification.Options{Logger: deps.Logger})
	apis.NewNotificationApi(api, *deps.Auth, controlSession, notificationService)

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
	if err := correlator.Reload(context.Background()); err != nil {
		stopBackground()
		return nil, fmt.Errorf("load fleet rules: %w", err)
	}
	apis.NewFleetRulesApi(api, *deps.Auth, controlSession, correlator)

	// The sweep is what makes an ABSENCE decidable: nothing arrives to tell you the badge was
	// never swiped, so the passage of time has to.
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ticker.C:
				correlator.Sweep(bgCtx)
			}
		}
	}()

	// Dedup ledger for node-relayed notifications: keys each ingested node event by the node's
	// stable engine id so the reconnect replay (below) never re-publishes one already delivered
	// live or by an earlier replay.
	relayDedup := services.NewRelayDedup(deps.Db)

	onNodeEvent := func(nodeID, kind string, body []byte) {
		ingestNodeEvent(notificationService, relayDedup, nodeID, kind, body)
		// Feed the same event to the correlator. It is deliberately fed the NODE event rather
		// than the control plane's own re-published notification: correlating on our own output
		// would let a fleet rule's alert satisfy another fleet rule's clause, and two rules could
		// then trigger each other forever.
		observeForCorrelation(context.Background(), correlator, nodeID, kind, body)
	}
	controlServer := services.NewControlServer(registry, p.ControlPort, onNodeEvent,
		func(format string, args ...any) { deps.Logger.Infof("myseliasan.control", format, args...) })
	// Replay-on-reconnect: the live push (above) drops any notification a node publishes while
	// its control channel is down, and nothing backfills it — so the control plane's feed could
	// undercount a busy node. When a node (re)connects, pull the notifications it created within
	// the replay window and ingest the ones we're missing (relayDedup makes this idempotent).
	controlServer.SetOnConnect(func(nodeID string) {
		replayNodeNotifications(controlServer, notificationService, relayDedup, nodeID,
			func(f string, a ...any) { deps.Logger.Infof("myseliasan.notif-replay", f, a...) })
	})
	// Keep the dedup ledger bounded: prune markers older than twice the replay window — a
	// windowed pull can never re-offer them, so they are dead weight.
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ticker.C:
				cutoff := time.Now().Add(-2 * notifReplayWindow).Unix()
				if _, err := relayDedup.Prune(bgCtx, cutoff); err != nil {
					deps.Logger.Warnf("myseliasan.notif-replay", "prune dedup ledger: %v", err)
				}
			}
		}
	}()
	// The persistent node-dialed control channel is the authoritative liveness signal:
	// a node holding a live connection is online even when the parent cannot reach its
	// mTLS port directly. Wire its presence into the heartbeat reconciler so the mTLS
	// poll becomes a fallback that can no longer flap a control-connected node offline.
	m.controlServer = controlServer
	registry.SetControlPresence(controlServer.IsConnected)
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
	go func() {
		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ticker.C:
				registry.Heartbeat(bgCtx)
			}
		}
	}()

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
	// Registered before the proxy catch-all so the specific media-offer path wins.
	apis.NewNodeMediaApi(api, *deps.Auth, mediaHub, accessService, mediaEngine, mediaICE, controlSession)

	// Reverse command tunnel: /api/nodes/{id}/proxy/<node-path> forwards over the
	// control channel to the node's own API, giving the commander the node's exact
	// capability surface. The operator's per-node grant decides viewer vs admin (no
	// read access → 403). Registered after NewNodesApi/NewNodeAccessApi so their
	// specific routes win; mux falls through to the proxy for /nodes/{id}/proxy/...
	// Range-capable recording playback over the tunnel (chunks each byte range under the
	// control-channel message cap). Registered before the generic proxy so its specific
	// /nodes/{id}/recording-stream/{segId} route wins.
	apis.NewRecordingStreamApi(api, *deps.Auth, controlServer, accessService, controlSession)
	apis.NewNodeProxyApi(api, *deps.Auth, controlServer, accessService, controlSession, auditService)

	return func(context.Context) error { stopBackground(); return nil }, nil
}

// ingestNodeEvent maps an event a node pushed up its control channel into the
// control plane's notification feed. "notification" events are re-published as-is
// (re-tagged with the node so the feed shows their origin and the parent assigns a
// fresh id); "going-offline" becomes a system warning. Any other kind (health,
// disk-full, alert, system, …) is surfaced rather than dropped: the frame is parsed
// as a notification when it carries one, otherwise wrapped in a generic message
// tagged with the raw kind — so a node reporting trouble is never silently lost.
func ingestNodeEvent(svc *notification.Service, dedup *services.RelayDedup, nodeID, kind string, body []byte) {
	switch kind {
	case "notification":
		var n notification.Notification
		if err := json.Unmarshal(body, &n); err != nil {
			return
		}
		republishNodeNotification(svc, dedup, nodeID, n)
	case "going-offline":
		svc.Publish(context.Background(), notification.Notification{
			Category: notification.CategorySystem,
			Severity: notification.Warning,
			Title:    "Node going offline",
			Body:     "Node " + nodeID + " reported it is going offline.",
			Source:   "node:" + nodeID,
			Data:     map[string]any{"nodeId": nodeID},
		})
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
			republishNodeNotification(svc, dedup, nodeID, n)
			return
		}
		svc.Publish(context.Background(), notification.Notification{
			Category: categoryForNodeKind(kind),
			Severity: severityForNodeKind(kind),
			Title:    "Node " + kind + " event",
			Body:     truncateBody(string(body), 500),
			Source:   "node:" + nodeID,
			Data:     map[string]any{"nodeId": nodeID, "kind": kind},
		})
	}
}

// republishNodeNotification re-tags a node-originated notification with its origin node and
// lets the parent assign a fresh id in its own feed. It is called on BOTH paths — the live
// control-channel push and the reconnect replay pull — so it dedups on the node's stable engine
// id (n.ID, which is the same value on both paths): once a given node event has been ingested it
// is never published again, which is what makes replaying a disconnect window idempotent.
// It returns true when the notification was published, false when dedup suppressed it.
func republishNodeNotification(svc *notification.Service, dedup *services.RelayDedup, nodeID string, n notification.Notification) bool {
	originID := n.ID // the node's engine id; identical on the live push and a pulled row's __oid
	if dedup != nil && dedup.SeenOrRecord(context.Background(), nodeID, originID, n.CreatedAt) {
		return false // already ingested (live or a prior replay)
	}
	n.ID = "" // parent assigns its own id in its own feed
	n.Source = "node:" + nodeID
	if n.Data == nil {
		n.Data = map[string]any{}
	}
	n.Data["nodeId"] = nodeID
	svc.Publish(context.Background(), n)
	return true
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
func replayNodeNotifications(sender services.ControlSender, svc *notification.Service, dedup *services.RelayDedup, nodeID string, logf func(string, ...any)) {
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
			if republishNodeNotification(svc, dedup, nodeID, nodeRowToNotification(row)) {
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
func severityForNodeKind(kind string) notification.Severity {
	k := strings.ToLower(kind)
	switch {
	case strings.Contains(k, "alert"), strings.Contains(k, "full"), strings.Contains(k, "critical"), strings.Contains(k, "fail"):
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

// openFleetSecretCipher resolves the at-rest master key for fleet secrets, mirroring
// mymatasan's encryption-at-rest boot (infra/atrest). Returns nil (no encryption) when
// the feature is disabled. When a key that existed here before is now missing it FAILS
// CLOSED — refusing to boot rather than minting a new key and silently orphaning the
// encrypted CA key/PSK (which would reset the whole fleet's trust); restore the key
// file or configure security.recoveryPath and restart.
func openFleetSecretCipher(deps apphost.Dependencies) (*atrest.Cipher, error) {
	if !boolValue(deps.Config.Security.EncryptAtRest, true) {
		return nil, nil
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
		return nil, fmt.Errorf("fleet-secret encryption key: %w", err)
	}
	if outcome.Mode == atrest.ModeRecoveryPending {
		return nil, fmt.Errorf("fleet-secret encryption key missing (id %s): restore %s or set security.recoveryPath, then restart — refusing to reset fleet trust", outcome.KeyId, keyPath)
	}
	deps.Logger.Infof("myseliasan.security", "fleet-secret encryption enabled (key %s, mode %s, id %s)", keyPath, outcome.Mode, outcome.KeyId)
	return outcome.KeyStore.Cipher(), nil
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
