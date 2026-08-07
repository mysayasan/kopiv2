package app

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/apis"
	pintuconfig "github.com/mysayasan/kopiv2/apps/mypintusan/config"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	"github.com/mysayasan/kopiv2/domain/notification"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// mypintusan is the suite's physical access-control appliance: myidsan decides who signs into
// software, this decides who goes through a door.
//
// It is the first app in the suite whose failure mode is A PERSON TRAPPED BEHIND A DOOR, and that
// shapes everything below. Free egress is hardware — a mechanical lever, or a power-cut interlock
// wired in series on a maglock feed — and nothing in this process can override it. A panic in a Go
// goroutine must not be able to trap anybody in a stairwell.
//
// Shipped so far: the OSDP driver and simulator (infra/access/osdp, tools/osdp-sim), the decision
// path, the door state machine, and SQLite persistence. This file is what makes those bootable.
//
// What remains: the myiotsan bindings for door contacts and relay actuation. Fleet adoption by
// myseliasan is wired (wire_fleet.go, KindDoor) on the same shared node stack the other
// appliances use.
type module struct {
	cfg *pintuconfig.Config
}

// New returns the app module for the host to run.
func New() apphost.App {
	return &module{}
}

func (m *module) Name() string { return "mypintusan" }

func (m *module) BaseDir() string { return filepath.Join("apps", "mypintusan") }

// DecodeAppConfig gives mypintusan its own config blocks (the site timezone, the RS-485 buses)
// without adding them to the shared model every other app would then carry.
func (m *module) DecodeAppConfig(raw []byte, dataDir string) error {
	cfg, err := pintuconfig.Load(raw)
	if err != nil {
		return err
	}
	m.cfg = cfg
	return nil
}

func (m *module) appConfig() *pintuconfig.Config {
	if m.cfg == nil {
		cfg, _ := pintuconfig.Load(nil)
		m.cfg = cfg
	}
	return m.cfg
}

// SharedAPIs trims the shared surface to what an appliance needs. A door controller is a
// single-tenant box on a building's LAN, not a platform: no app registry, no file storage, no
// multi-app endpoint catalog.
func (m *module) SharedAPIs() apphost.SharedAPIConfig {
	cfg := apphost.DefaultSharedAPIConfig()
	cfg.AppRegistry = false
	cfg.ApiEndpoint = false
	cfg.FileStorage = false
	cfg.CacheService = false
	return cfg
}

// Entities is the schema. LocalUser and the rest of the shared block are the same types the other
// appliances use, so one implementation of bcrypt, sessions and the last-admin guard serves them
// all rather than four that drift.
func (m *module) Entities() []any {
	return append([]any{
		sharedentities.ApiEndpoint{},
		sharedentities.ApiLog{},
		sharedentities.UserSession{},
		sharedentities.Notification{},
		sharedentities.LocalUser{},
		sharedentities.AccessRole{},
		sharedentities.AccessRolePermission{},
		sharedentities.RuntimeSetting{},
	}, services.Entities()...)
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
		{Title: "Login", Description: "exchange a credential for a session cookie", Path: "/api/auth/login", AccessTier: apiaccessenums.Public},
		{Title: "Auth", Description: "session probe and self-service password change", Path: "/api/auth", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Doors", Description: "doors, their live state and remote unlock", Path: "/api/doors", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Readers", Description: "reader health and Secure Channel state", Path: "/api/readers", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Holders", Description: "people and their badges", Path: "/api/holders", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Access log", Description: "every access decision, granted or denied", Path: "/api/events", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Lockdown", Description: "seal the site", Path: "/api/lockdown", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Settings", Description: "users and roles", Path: "/api/settings", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Setup Wizard", Description: "first-run setup state and completion", Path: "/api/setup", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Notifications", Description: "unified event feed: alarms, badge decisions, security events", Path: "/api/notifications", AccessTier: apiaccessenums.AuthOnly},
	}

	statements := make([]string, 0, len(endpoints)*2)
	for _, e := range endpoints {
		statements = append(statements,
			fmt.Sprintf(`INSERT INTO api_endpoint (title, description, app_code, host, path, access_tier, is_active, created_by, created_at, updated_by, updated_at)
SELECT '%s', '%s', 'mypintusan', '*', '%s', %d, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM api_endpoint WHERE app_code = 'mypintusan' AND host = '*' AND path = '%s');`,
				e.Title, e.Description, e.Path, e.AccessTier, e.Path),
			fmt.Sprintf(`UPDATE api_endpoint SET app_code = 'mypintusan', access_tier = %d WHERE host = '*' AND path = '%s' AND ((access_tier IS NULL OR access_tier <> %d) OR app_code IS NULL OR app_code = '');`,
				e.AccessTier, e.Path, e.AccessTier),
		)
	}

	seeders := []bootstrap.Seeder{bootstrap.NewSQLSeeder("mypintusan-endpoints", statements)}
	if len(seedStatements) > 0 {
		seeders = append(seeders, bootstrap.NewSQLSeeder("config", seedStatements))
	}
	return seeders
}

// RegisterAppRoutes wires the app: identity, the access store, the OSDP buses, and the HTTP API.
func (m *module) RegisterAppRoutes(api *mux.Router, deps apphost.Dependencies) (apphost.ShutdownFunc, error) {
	ctx := context.Background()
	// bgCtx bounds the fleet workers (discovery responder, enrollment, control channel); the OSDP
	// runtime keeps its own cancel because it predates them and owns its bus supervisors.
	bgCtx, stopBackground := context.WithCancel(context.Background())
	cfg := m.appConfig()

	userRepo := dbsql.NewGenericRepo[sharedentities.LocalUser](deps.Db)
	localUser := sharedservices.NewLocalUserService(userRepo, deps.AccessRoles)

	// Roles before the admin is seeded: the bootstrap admin has to be given the superadmin role,
	// and the role has to exist to be given.
	if err := services.EnsureRoles(ctx, deps.AccessRoles, deps.AccessPerms); err != nil {
		return nil, fmt.Errorf("seed authorization roles: %w", err)
	}
	adminSeed, err := localUser.EnsureDefaultAdmin(ctx, deps.Config.LocalAuth.Username, deps.Config.LocalAuth.Password)
	if err != nil {
		return nil, fmt.Errorf("seed local admin user: %w", err)
	}
	if adminSeed.Seeded {
		announceFirstRunAdmin(deps, adminSeed)
	}
	notificationRepo := dbsql.NewGenericRepo[sharedentities.Notification](deps.Db)
	notifications := notification.NewService(notificationRepo, notification.Options{Logger: deps.Logger})

	store := services.NewSQLStore(deps.Db)
	alarms := services.NewNotificationAlarmer(notifications, deps.Logger)

	// config.json SEEDS the first run; the database owns these values afterwards. This is
	// mymatasan's pattern, and it is what makes the app configurable by a facilities manager
	// rather than by somebody editing JSON over SSH.
	settings := services.NewAccessSettingsService(store.SettingsRepo(), settingsFromConfig(cfg))
	live, err := settings.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("load access settings: %w", err)
	}
	// Refuse to start on a bad timezone rather than falling back to UTC, which would silently shift
	// every schedule on the site.
	location, err := live.Location()
	if err != nil {
		return nil, fmt.Errorf("access settings timezone %q: %w", live.Timezone, err)
	}

	// One controller per RS-485 bus. Each owns its port for the life of the process — a CP holds
	// the port open and polls continuously, unlike the open/poll/close of the Modbus driver.
	// Badge decisions flow into the notification feed alongside the alarms — that stream is what
	// the fleet control plane correlates across nodes once this node is adopted.
	runtime := newRuntime(deps, live, location, store, alarms, alarms.Decision, store.StrikeFor)
	if err := runtime.start(ctx); err != nil {
		stopBackground()
		return nil, err
	}

	setupState := sharedservices.NewSetupStateService(dbsql.NewGenericRepo[sharedentities.RuntimeSetting](deps.Db))

	authCfg := sharedapis.LocalAuthConfig{
		AppName: m.Name(),
		OnLockout: func(ctx context.Context, info sharedapis.LockoutInfo) {
			notifications.Publish(ctx, notification.Notification{
				Category: notification.CategorySystem,
				Severity: notification.Warning,
				Title:    "Sign-in locked out",
				Body: fmt.Sprintf("Too many failed sign-ins for %q from %s. Locked for %d seconds.",
					info.Username, info.IP, info.LockSeconds),
				Source: "auth",
				Data:   map[string]any{"username": info.Username, "ip": info.IP},
			})
		},
	}
	loginGuard := sharedapis.NewLoginGuard(loginGuardConfig(deps))

	// PUBLIC first. /auth/login is the endpoint that authenticates, so it cannot sit behind the
	// middleware that demands authentication — and it must be registered BEFORE the protected
	// subrouter, or the catch-all swallows it.
	sharedapis.NewLocalLoginApi(api, authCfg, localUser, loginGuard)

	protected := api.PathPrefix("").Subrouter()
	// Order is load-bearing: auth puts the principal in context, and the permission matrix needs a
	// principal to decide against. Reversed, it fails closed on everything.
	protected.Use(sharedapis.NewLocalBasicAuth(authCfg, localUser, loginGuard))
	protected.Use(sharedapis.NewRequireRolePermission(deps.AccessRoles, deps.AccessPerms))

	sharedapis.NewLocalAuthApi(protected, authCfg, localUser)

	apis.NewSettingsApi(protected, settings)
	apis.NewDoorApi(protected, store, runtime, deps.Db)
	apis.NewHolderApi(protected, deps.Db)
	apis.NewEventApi(protected, deps.Db)
	apis.NewLockdownApi(protected, runtime)
	apis.NewSetupApi(protected, setupState)
	apis.NewNotificationsApi(protected, notifications)

	// --- the fleet -------------------------------------------------------------------
	//
	// mypintusan is adopted by myseliasan exactly as the other appliances are, on the same shared
	// node stack. It reports KindDoor, so the control plane knows a door controller is neither a
	// camera nor a sensor hub.
	//
	// The event sink registered inside buildFleet is what makes the fifth app a fleet citizen:
	// every alarm AND every badge decision this node raises also lands in the control plane's
	// unified feed, where it can be correlated with camera and sensor events — motion AND a door
	// opening AND no badge accepted.
	if boolValue(deps.Config.Pairing.Enabled, true) {
		fleetCipher, cerr := openFleetSecretCipher(deps)
		if cerr != nil {
			stopBackground()
			runtime.stop()
			return nil, cerr
		}
		f := buildFleet(api, deps, appVersion(m), fleetCipher, notifications)

		// The PUBLIC pairing routes (adopt / release / self-drop) authenticate with the FLEET KEY,
		// not a user session — a control plane adopting a node has no user behind it. They must be
		// registered on the unauthenticated router, before the protected subrouter, or the auth
		// middleware swallows the adopt call and the node can never be adopted at all.
		sharedapis.NewPairingPublicApi(api, f.pairing, f.enrollment.Kick)
		sharedapis.NewPairingApi(protected, f.pairing)

		f.start(bgCtx, deps)
	}

	return func(ctx context.Context) error {
		stopBackground()
		runtime.stop()
		return nil
	}, nil
}

// announceFirstRunAdmin prints the bootstrap credentials. On a fresh install this console banner is
// the only place a CLI, Docker or systemd operator learns them; the account is must-change.
func announceFirstRunAdmin(deps apphost.Dependencies, seed sharedservices.AdminSeedResult) {
	if deps.Logger == nil {
		return
	}
	deps.Logger.Infof("mypintusan.firstrun",
		"first-run admin: username=%q password=%q (must be changed at first sign-in)",
		seed.Username, seed.Password)
}

// RegisterWebRoutes serves the SPA shell.
//
// Resolved against deps.HomeDir, NOT BaseDir(): BaseDir() is the CWD-relative dev path, so a
// packaged install — binary and static/ side by side, working directory elsewhere — would 404 on
// "/". apphost's SPA catch-all uses HomeDir, and this has to match it.
func (m *module) RegisterWebRoutes(router *mux.Router, deps apphost.Dependencies) error {
	staticIndex := filepath.Join(deps.HomeDir, "static", "index.html")
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		// index.html points at content-hashed chunks, so it must never be cached: a stale index
		// keeps a browser on an old bundle even after the app has been upgraded.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, staticIndex)
	}
	router.HandleFunc("/", serveIndex).Methods("GET")
	router.HandleFunc("/index.html", serveIndex).Methods("GET")
	return nil
}

// loginGuardConfig translates the shared login-security block into the guard's shape.
func loginGuardConfig(deps apphost.Dependencies) sharedapis.LoginGuardConfig {
	ls := deps.Config.LoginSecurity.Effective()
	return sharedapis.LoginGuardConfig{
		Enabled:     ls.Enabled,
		MaxAttempts: ls.MaxAttempts,
		Window:      time.Duration(ls.WindowSeconds) * time.Second,
		BaseLockout: time.Duration(ls.LockoutSeconds) * time.Second,
		MaxLockout:  time.Duration(ls.LockoutMaxSeconds) * time.Second,
		FailedDelay: time.Duration(ls.FailedDelayMs) * time.Millisecond,
	}
}

// settingsFromConfig converts the config.json block into the first-run seed.
//
// After that first boot this function's output is never used again — the runtime_setting row wins.
// It stays as the RESET target, so a settings edit that stops the controller booting can be undone
// from the UI instead of from the database.
func settingsFromConfig(cfg *pintuconfig.Config) services.AccessSettings {
	out := services.AccessSettings{
		Timezone:         cfg.Access.Timezone,
		TickSeconds:      cfg.Access.TickSeconds,
		PINWindowSeconds: cfg.Access.PINWindowSeconds,
		Offline:          cfg.Access.Offline,
	}
	for _, b := range cfg.Buses {
		bus := services.BusSettings{
			Port: b.Port, SlotMillis: b.SlotMillis, ReplyTimeoutMillis: b.ReplyTimeoutMillis,
		}
		for _, r := range b.Readers {
			bus.Readers = append(bus.Readers, services.ReaderSettings{
				Address: r.Address, SCBK: r.SCBK, RequireSecureChannel: r.RequireSecureChannel,
			})
		}
		out.Buses = append(out.Buses, bus)
	}
	return out
}
