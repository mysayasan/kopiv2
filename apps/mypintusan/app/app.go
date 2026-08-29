package app

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/apis"
	pintuconfig "github.com/mysayasan/kopiv2/apps/mypintusan/config"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	"github.com/mysayasan/kopiv2/domain/notification"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"
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
		// The administrative trail, shared with myidsan, myseliasan and mymatasan. The access log
		// (entities.AccessEvent) records door DECISIONS; this records who decided them — who
		// changed a grant, a schedule, a holiday, a door's offline policy, and who sealed the site.
		sharedaudit.AuditLog{},
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
		{Title: "Access groups", Description: "named sets of holders", Path: "/api/groups", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Grants", Description: "which groups reach which doors, on what schedule", Path: "/api/grants", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Schedules", Description: "time policies and the holiday calendar", Path: "/api/schedules", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Administrative trail", Description: "who changed the rules about who gets in", Path: "/api/audit", AccessTier: apiaccessenums.AuthOnly},
		// The CSV export is its own row for the same reason it is its own catalog rule: paths here
		// are matched segment-wise, and "audit.csv" is not a child of "audit".
		{Title: "Administrative trail (CSV)", Description: "the trail as a download, for an auditor outside the product", Path: "/api/audit.csv", AccessTier: apiaccessenums.AuthOnly},
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

	// The administrative trail, built early because the handlers that record into it are wired
	// below and a trail wired late is a trail that quietly misses the first actions after boot.
	//
	// The trusted-proxy list is the rate limiter's, so "which hops may set X-Forwarded-For" has
	// exactly one answer in this app: an untrusted caller must not be able to forge the address
	// recorded next to their change to who may enter the building.
	services.DescribeMetrics(deps.Metrics)
	auditService := services.WithAuditMetrics(
		services.NewAuditService(deps.Db, func(format string, args ...any) {
			deps.Logger.Warnf("mypintusan.audit", format, args...)
		}),
		deps.Metrics,
	)
	auditor := apis.NewAuditor(auditService, deps.Config.RateLimit.TrustedProxies)
	startAuditRetention(deps, auditService)

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

	// How stale this controller's access rules are allowed to get before its doors stop honouring
	// them. `Decide()` has always compared a cache age against each door's TTL; nothing ever
	// computed the age, so the comparison was against zero on every install and the whole offline
	// design's "past the TTL the door denies" could not happen. See services.CacheClock.
	cacheClock := services.NewCacheClock(dbsql.NewGenericRepo[sharedentities.RuntimeSetting](deps.Db))

	// One controller per RS-485 bus. Each owns its port for the life of the process — a CP holds
	// the port open and polls continuously, unlike the open/poll/close of the Modbus driver.
	// Badge decisions flow into the notification feed alongside the alarms — that stream is what
	// the fleet control plane correlates across nodes once this node is adopted.
	runtime := newRuntime(deps, live, location, store, alarms, alarms.Decision, store.StrikeFor, cacheClock)
	// Applied through SetOffline rather than seeded in the constructor, so an appliance installed
	// with `access.offline` already true in config.json raises the degraded-mode alert at BOOT.
	// Such a site never crosses the edge from online to offline, and would otherwise run from cache
	// forever with nobody told — which is the case the alert most needs to cover.
	runtime.SetOffline(ctx, live.Offline)
	if err := runtime.start(ctx); err != nil {
		return nil, err
	}
	// A settings edit has to reach the RUNNING controllers, not just the database. Only `offline`
	// can be applied without a restart; the runtime says so for the rest. Registered before the API
	// is mounted, so no save can slip through unapplied.
	settings.OnChange(runtime.ApplySettings)

	// bgCtx bounds the fleet workers (discovery responder, enrollment, control channel); the OSDP
	// runtime keeps its own cancel because it predates them and owns its bus supervisors. Created
	// HERE, after the last early setup-error return, so every path from this point either cancels
	// it (the fleet-cipher failure below) or hands it to the ShutdownFunc.
	bgCtx, stopBackground := context.WithCancel(context.Background())

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

	// BEFORE NewLocalAuthApi: that call mounts a subrouter on the /auth prefix which serves only
	// the two routes it declares, so registering this one first keeps it from depending on how a
	// prefix subrouter behaves when none of its children match.
	//
	// The screens gate their controls on this rather than on a client-side isAdmin, which is what
	// stops the rail and the matrix being two independent copies of the same policy.
	apis.NewCapabilitiesApi(protected, deps.AccessPerms)
	sharedapis.NewLocalAuthApi(protected, authCfg, localUser)

	// Every accepted administrative change to the access rules resets the offline cache clock.
	//
	// This is the second half of what "the cache is stale" means, and without it the idea is
	// absurd. A controller cut off from its control plane cannot be TOLD that a credential was
	// revoked — that is what the TTL defends against. But an operator who can still sign in to this
	// appliance and revoke the credential here IS an authority reaching the controller, and a door
	// that locked out a site being actively administered would be enforcing a staleness that does
	// not exist. So a rule edit counts as contact, exactly as a control-plane connection does.
	//
	// Scoped by PATH and by 2xx: a badge decision is not contact (the whole point is that badges
	// keep arriving at a controller nobody can reach), and a refused edit is not contact either.
	protected.Use(ruleChangeTouch(cacheClock))

	// The administrative trail. Registered as the INNERMOST middleware so the principal, put in
	// context by the auth middleware above, is available to attribute an entry — and so that
	// nothing accepted goes unrecorded even if a future handler forgets to audit itself. See
	// apis.NewAuditMiddleware for why the default is "recorded" rather than "opt in".
	protected.Use(apis.NewAuditMiddleware(auditor))
	apis.NewAuditApi(protected, auditService)

	apis.NewSettingsApi(protected, settings, auditor)
	// Users and roles. Without this the three roles services.EnsureRoles seeds on every boot are
	// unassignable and the appliance is single-admin — which is what it was until now.
	apis.NewUserApi(protected, localUser, deps.AccessRoles, auditor)
	apis.NewDoorApi(protected, store, runtime, deps.Db, auditor)
	apis.NewHolderApi(protected, deps.Db, auditor)
	apis.NewEventApi(protected, deps.Db)
	apis.NewLockdownApi(protected, runtime, auditor)
	apis.NewSetupApi(protected, setupState)
	// Single-instance by design (the OSDP bus owns its serial port).
	apis.NewDeploymentApi(protected)
	apis.NewNotificationsApi(protected, notifications)
	apis.NewAccessRulesApi(protected, deps.Db, notifications, auditor)

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
		f := buildFleet(api, deps, appVersion(m), fleetCipher, notifications, cacheClock)

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

// accessRulePaths are the API prefixes whose mutations are a change to who may enter. Settings is
// included: the offline flag and the bus layout are as much a part of the access decision as a
// grant is, and an operator on that screen is as present as one on any other.
var accessRulePaths = []string{
	"/api/doors", "/api/holders", "/api/groups", "/api/schedules", "/api/grants",
	"/api/holidays", "/api/settings/access", "/api/lockdown",
}

// accessRuleExcluded are mutations under those prefixes that operate a door rather than change the
// rules. A remote unlock is the one that matters: it is a POST under /api/doors, and it is an
// OPERATION, not a statement about who may enter. Counting it would let the act of using the site
// keep its own staleness clock at zero, which is precisely the thing the clock is supposed to
// measure independently of.
var accessRuleExcluded = []string{"/unlock"}

// ruleChangeTouch resets the offline cache clock after an accepted change to the access rules.
func ruleChangeTouch(clock *services.CacheClock) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				next.ServeHTTP(w, r)
				return
			}
			matched := false
			for _, p := range accessRulePaths {
				if strings.HasPrefix(r.URL.Path, p) {
					matched = true
					break
				}
			}
			for _, suffix := range accessRuleExcluded {
				if strings.HasSuffix(r.URL.Path, suffix) {
					matched = false
					break
				}
			}
			if !matched {
				next.ServeHTTP(w, r)
				return
			}
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			if rec.status >= 200 && rec.status < 300 {
				clock.Touch(r.Context())
			}
		})
	}
}

// statusRecorder remembers the status code so the middleware can tell an accepted edit from a
// refused one. A handler that never calls WriteHeader has written 200, which is why the zero value
// is not left as the default.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
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
			StatusMillis: b.StatusMillis,
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
