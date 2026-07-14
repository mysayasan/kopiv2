package app

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	"github.com/mysayasan/kopiv2/domain/notification"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/versioning"
)

// myiotsan is the suite's IoT sensor hub — "the NVR, but for sensors". It is an appliance
// like mymatasan (single binary, on-prem, air-gapped, adopted into the myseliasan fleet),
// and it deliberately reuses mymatasan's spine:
//
//	device -> signal -> detector -> rule -> alert -> notify -> historize -> dashboard
//
// Cameras are one signal type; sensors are another. See docs/MYIOTSAN_PLAN.md.
//
// THIS IS P0: the scaffolding. The app boots, authenticates, and serves its SPA shell. The
// domain — devices, profiles, telemetry ingest, rules — lands in P1/P2 and is deliberately
// absent rather than stubbed, so nothing here is a placeholder pretending to work.
type module struct{}

func New() apphost.App {
	return &module{}
}

func (m *module) Name() string {
	return "myiotsan"
}

func (m *module) BaseDir() string {
	return filepath.Join("apps", "myiotsan")
}

// SharedAPIs trims the shared API surface to what an appliance actually needs. myiotsan is
// a single-tenant device on someone's LAN, not a platform: it has no app registry, no
// file-storage service and no multi-app endpoint catalog to administer.
func (m *module) SharedAPIs() apphost.SharedAPIConfig {
	cfg := apphost.DefaultSharedAPIConfig()
	cfg.AppRegistry = false
	cfg.ApiEndpoint = false
	cfg.FileStorage = false
	cfg.CacheService = false
	return cfg
}

// Entities is the P0 schema: everything needed to sign a user in and record what happened.
// The IoT domain tables (iot_device, device_profile, telemetry_key, device_reading,
// reading_rollup, iot_rule, alert_event) arrive with the code that uses them.
//
// LocalUser is the SHARED appliance user (domain/entities), the same type mymatasan uses —
// so both apps run one implementation of bcrypt, sessions and the last-admin guard rather
// than two that drift.
func (m *module) Entities() []any {
	return []any{
		sharedentities.ApiEndpoint{},
		sharedentities.ApiLog{},
		sharedentities.UserSession{},
		sharedentities.Notification{},
		sharedentities.LocalUser{},
		sharedentities.AccessRole{},
		sharedentities.AccessRolePermission{},
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
		{Title: "Login", Description: "exchange a credential for a session cookie", Path: "/api/auth/login", AccessTier: apiaccessenums.Public},
		{Title: "Auth", Description: "session probe and self-service password change", Path: "/api/auth", AccessTier: apiaccessenums.AuthOnly},
	}

	statements := make([]string, 0, len(endpoints)*2)
	for _, endpoint := range endpoints {
		statements = append(statements,
			fmt.Sprintf(`INSERT INTO api_endpoint (title, description, app_code, host, path, access_tier, is_active, created_by, created_at, updated_by, updated_at)
SELECT '%s', '%s', 'myiotsan', '*', '%s', %d, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM api_endpoint WHERE app_code = 'myiotsan' AND host = '*' AND path = '%s');`, endpoint.Title, endpoint.Description, endpoint.Path, endpoint.AccessTier, endpoint.Path),
			fmt.Sprintf(`UPDATE api_endpoint SET app_code = 'myiotsan', access_tier = %d WHERE host = '*' AND path = '%s' AND ((access_tier IS NULL OR access_tier <> %d) OR app_code IS NULL OR app_code = '');`, endpoint.AccessTier, endpoint.Path, endpoint.AccessTier),
		)
	}

	seeders := []bootstrap.Seeder{
		bootstrap.NewSQLSeeder("myiotsan-endpoints", statements),
	}
	if len(seedStatements) > 0 {
		seeders = append(seeders, bootstrap.NewSQLSeeder("config", seedStatements))
	}
	return seeders
}

func (m *module) RegisterAppRoutes(api *mux.Router, deps apphost.Dependencies) (apphost.ShutdownFunc, error) {
	ctx := context.Background()

	userRepo := dbsql.NewGenericRepo[sharedentities.LocalUser](deps.Db)
	localUser := sharedservices.NewLocalUserService(userRepo, deps.AccessRoles)

	// Roles BEFORE the admin is seeded: the bootstrap admin has to be given the superadmin
	// role, and the role has to exist to be given.
	if err := services.EnsureRoles(ctx, deps.AccessRoles, deps.AccessPerms); err != nil {
		return nil, fmt.Errorf("seed authorization roles: %w", err)
	}
	adminRole, err := deps.AccessRoles.GetByName(ctx, services.RoleAdmin)
	if err != nil || adminRole == nil {
		return nil, fmt.Errorf("resolve %s role: %w", services.RoleAdmin, err)
	}

	adminSeed, err := localUser.EnsureDefaultAdmin(ctx, deps.Config.LocalAuth.Username, deps.Config.LocalAuth.Password)
	if err != nil {
		return nil, fmt.Errorf("seed local admin user: %w", err)
	}
	if adminSeed.Seeded {
		// Fresh install: this console banner is the only place a CLI/Docker/systemd operator
		// learns the bootstrap credentials. The account is flagged must-change.
		announceFirstRunAdmin(deps, adminSeed)
	}

	// The notification store exists in P0 so security events (a sign-in lockout) are RECORDED
	// from the first boot. There is no read API for them yet — that arrives with the feed in
	// P1 — but an event that was never written cannot be shown later.
	notificationRepo := dbsql.NewGenericRepo[sharedentities.Notification](deps.Db)
	notificationService := notification.NewService(notificationRepo, notification.Options{Logger: deps.Logger})

	authCfg := sharedapis.LocalAuthConfig{
		AppName: m.Name(),
		OnLockout: func(ctx context.Context, info sharedapis.LockoutInfo) {
			notificationService.Publish(ctx, notification.Notification{
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

	// PUBLIC first. /auth/login is the endpoint that authenticates, so it cannot sit behind
	// the middleware that demands authentication. It must be registered before the protected
	// subrouter or the catch-all swallows it.
	sharedapis.NewLocalLoginApi(api, authCfg, localUser, loginGuard)

	protected := api.PathPrefix("").Subrouter()
	// Order is load-bearing: auth puts the principal in context, and the matrix needs a
	// principal to decide against — reversed, it fails closed on everything.
	protected.Use(sharedapis.NewLocalBasicAuth(authCfg, localUser, loginGuard))
	protected.Use(sharedapis.NewRequireRolePermission(deps.AccessRoles, deps.AccessPerms))

	sharedapis.NewLocalAuthApi(protected, authCfg, localUser)

	return func(context.Context) error { return nil }, nil
}

// loginGuardConfig maps the shared login-security config onto the failed-login lockout.
func loginGuardConfig(deps apphost.Dependencies) sharedapis.LoginGuardConfig {
	ls := deps.Config.LoginSecurity
	return sharedapis.LoginGuardConfig{
		Enabled:     ls.Enabled,
		MaxAttempts: ls.MaxAttempts,
		Window:      time.Duration(ls.WindowSeconds) * time.Second,
		BaseLockout: time.Duration(ls.LockoutSeconds) * time.Second,
		MaxLockout:  time.Duration(ls.LockoutMaxSeconds) * time.Second,
		FailedDelay: time.Duration(ls.FailedDelayMs) * time.Millisecond,
	}
}

func (m *module) RegisterWebRoutes(router *mux.Router, deps apphost.Dependencies) error {
	// Resolve against deps.HomeDir, NOT BaseDir(): BaseDir() is the CWD-relative dev path,
	// so a packaged install (binary and static/ side by side, working directory elsewhere)
	// would 404 on "/". apphost's SPA catch-all uses HomeDir; this must match.
	staticIndex := filepath.Join(deps.HomeDir, "static", "index.html")
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		// index.html points at content-hashed chunks, so it must never be cached: a stale
		// index keeps the browser on an old bundle even after a rebuild.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, staticIndex)
	}
	router.HandleFunc("/", serveIndex).Methods("GET")
	router.HandleFunc("/index.html", serveIndex).Methods("GET")
	return nil
}

func (m *module) APIDocs() apidocs.SpecConfig {
	docVersion := "0.1.0"
	if manifest, err := versioning.LoadDefault(); err == nil {
		if info, err := manifest.InfoForApp(m.Name()); err == nil {
			docVersion = info.AppVersion
		}
	}

	return apidocs.SpecConfig{
		Metadata: apidocs.Metadata{
			Title:       "myiotsan API",
			Version:     docVersion,
			Description: "On-prem IoT sensor hub: device inventory, telemetry, rules and alerts.",
		},
		Endpoints: map[string]apidocs.EndpointDoc{
			"POST /api/auth/login": {
				Summary:     "Sign in",
				Description: "Exchanges a username and password for a session cookie.",
				Tags:        []string{"auth"},
			},
			"POST /api/auth/logout": {
				Summary:     "Sign out",
				Description: "Clears the session cookie.",
				Tags:        []string{"auth"},
			},
			"GET /api/auth/session": {
				Summary:     "Current session",
				Description: "Returns the signed-in user, including whether a password change is pending.",
				Tags:        []string{"auth"},
			},
			"POST /api/auth/change-password": {
				Summary:     "Change your password",
				Description: "Verifies the current password and sets a new one, rotating the session.",
				Tags:        []string{"auth"},
			},
		},
	}
}
