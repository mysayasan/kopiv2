package app

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/apis"
	iotconfig "github.com/mysayasan/kopiv2/apps/myiotsan/config"
	appentities "github.com/mysayasan/kopiv2/apps/myiotsan/entities"
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
	iotmqtt "github.com/mysayasan/kopiv2/infra/iot/mqtt"
	"github.com/mysayasan/kopiv2/infra/safego"
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
// P0-P2 (the MVP): the app boots, authenticates, ingests telemetry from real devices over an
// embedded MQTT broker, evaluates rules against it, and raises alerts. What remains is
// discovery (P3), actuation (P4), industrial protocols (P5) and fleet adoption (P6).
type module struct {
	// cfg is myiotsan's own slice of config.json, decoded through the apphost.AppConfigDecoder
	// seam. See apps/myiotsan/config.
	cfg *iotconfig.Config
}

func New() apphost.App {
	return &module{}
}

// DecodeAppConfig gives myiotsan its own config blocks (the MQTT broker, the telemetry store)
// without adding them to the shared AppConfigModel that every other app would then carry.
func (m *module) DecodeAppConfig(raw []byte, dataDir string) error {
	cfg, err := iotconfig.Load(raw)
	if err != nil {
		return err
	}
	m.cfg = cfg
	return nil
}

// appConfig returns the decoded config, defaulted if the host never called the decoder (which
// only happens in a test that constructs the module directly).
func (m *module) appConfig() *iotconfig.Config {
	if m.cfg == nil {
		cfg, _ := iotconfig.Load(nil)
		m.cfg = cfg
	}
	return m.cfg
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

// Entities is the schema. LocalUser is the SHARED appliance user (domain/entities), the same type mymatasan uses —
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

		// The IoT domain.
		appentities.DeviceProfile{},
		appentities.TelemetryKey{},
		appentities.IotDevice{},
		appentities.DeviceReading{},
		appentities.ReadingRollup{},
		appentities.IotRule{},
		appentities.AlertEvent{},
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
		{Title: "Devices", Description: "device inventory and telemetry", Path: "/api/devices", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Profiles", Description: "device-type catalog", Path: "/api/profiles", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Rules", Description: "alert rules over telemetry", Path: "/api/rules", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Alerts", Description: "the alert log", Path: "/api/alerts", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Notifications", Description: "unified event feed", Path: "/api/notifications", AccessTier: apiaccessenums.AuthOnly},
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

	// The unified feed: rule alerts, device health, and the app's own security events (a
	// sign-in lockout) all land here, so an operator has one place to look.
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

	// --- the ingest spine -------------------------------------------------------------
	//
	//	broker -> ingest (decode -> deadband -> batched write)
	//	                       \
	//	                        -> rules -> alert -> notification
	//
	// Built bottom-up because each stage owns the one before it.
	bgCtx, stopBackground := context.WithCancel(context.Background())

	deviceService := services.NewDeviceService(deps.Db, func(f string, a ...any) {
		deps.Logger.Warnf("myiotsan.devices", f, a...)
	})
	profileService := services.NewProfileService(deps.Db)
	// Seed the shipped device catalog. Existing profiles are left alone, so a site that has
	// tuned a builtin's deadbands does not have that overwritten on the next boot.
	if err := profileService.EnsureBuiltins(ctx); err != nil {
		stopBackground()
		return nil, fmt.Errorf("seed device profiles: %w", err)
	}

	telemetry := services.NewTelemetryService(deps.Db, func(f string, a ...any) {
		deps.Logger.Infof("myiotsan.telemetry", f, a...)
	})

	gate := services.NewDeadbandGate()
	appCfg := m.appConfig()
	writer := services.NewReadingWriter(
		dbsql.NewGenericRepo[appentities.DeviceReading](deps.Db),
		services.ReadingWriterOptions{
			BatchSize:     appCfg.Telemetry.BatchSize,
			FlushInterval: time.Duration(appCfg.Telemetry.FlushMs) * time.Millisecond,
			QueueSize:     appCfg.Telemetry.QueueSize,
			Logf:          func(f string, a ...any) { deps.Logger.Warnf("myiotsan.telemetry", f, a...) },
		})
	writer.Run(bgCtx)

	engine := services.NewRuleEngine()
	ruleService := services.NewRuleService(deps.Db, engine, telemetry, notificationService, deviceService,
		func(f string, a ...any) { deps.Logger.Warnf("myiotsan.rules", f, a...) })
	// Loading the rules also RE-SEEDS EACH COOLDOWN from the database. Skip it and every
	// restart re-arms every rule that is still true — the alert storm mymatasan shipped.
	if err := ruleService.Reload(ctx); err != nil {
		stopBackground()
		return nil, fmt.Errorf("load rules: %w", err)
	}

	ingest := services.NewIngest(deviceService, profileService, gate, writer, ruleService,
		func(f string, a ...any) { deps.Logger.Warnf("myiotsan.ingest", f, a...) })

	// The embedded MQTT broker. Embedded, not depended upon: requiring the operator to run
	// Mosquitto alongside would break the single-binary, air-gapped promise that is the product.
	// Its authenticator is the DEVICE TABLE, so a device that is not in the inventory cannot
	// connect at all.
	broker, err := iotmqtt.New(iotmqtt.Options{
		Addr:      appCfg.MQTT.Addr,
		Auth:      deviceService,
		OnMessage: ingest.Handle,
		Logf:      func(f string, a ...any) { deps.Logger.Infof("myiotsan.mqtt", f, a...) },
	})
	if err != nil {
		stopBackground()
		return nil, fmt.Errorf("mqtt broker: %w", err)
	}
	safego.Go("myiotsan.mqtt", func() {
		if err := broker.Run(bgCtx); err != nil {
			deps.Logger.Warnf("myiotsan.mqtt", "broker stopped: %v", err)
		}
	})

	// Rollup + retention. Rollups are built BEFORE the raw rows they summarize are purged.
	telemetry.RunRollup(bgCtx, services.RetentionConfig{
		RawDays:    appCfg.Telemetry.RawRetentionDays,
		RollupDays: appCfg.Telemetry.RollupRetentionDays,
	})

	// The offline sweep. An "offline" rule cannot be driven by a reading — its whole subject is
	// the ABSENCE of readings, and a device that has gone silent will never call OnReading
	// again. This ticker is what makes silence audible; without it a dead sensor is a monitoring
	// system quietly lying to you.
	safego.Supervise(bgCtx, "myiotsan.offline-sweep", func(ctx context.Context) {
		ticker := time.NewTicker(offlineSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ruleService.SweepOffline(ctx)
			}
		}
	})

	apis.NewDevicesApi(protected, deviceService, telemetry, profileService, ingest)
	apis.NewProfilesApi(protected, profileService, ingest)
	apis.NewRulesApi(protected, ruleService)
	apis.NewNotificationsApi(protected, notificationService)

	return func(context.Context) error {
		stopBackground()
		// Let the batcher flush what it has already accepted. A clean shutdown should not throw
		// away readings it took responsibility for.
		writer.Wait(5 * time.Second)
		return nil
	}, nil
}

// offlineSweepInterval is how often silence is checked for. A minute is well under any sane
// offline window and costs one query.
const offlineSweepInterval = time.Minute

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
