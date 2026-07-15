package app

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
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
	"github.com/mysayasan/kopiv2/infra/atrest"
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
// Shipped through P4: the app boots and authenticates; devices are onboarded through a
// time-boxed, quarantined enrollment window; telemetry arrives over an embedded MQTT broker and
// is deadbanded, stored and rolled up; rules are evaluated and alerts raised; and devices can be
// COMMANDED, behind gates (read-only by default, admin-only, only-what-the-profile-declares,
// server-side bounds, rate-limited, audited, and never auto-retried — see services/commands.go).
//
// What remains: industrial protocols (P5 — Modbus, OPC-UA), fleet adoption by myseliasan and
// the cross-domain rules that are the reason this app exists (P6), and release plumbing (P7).
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
		appentities.DiscoveredDevice{},
		appentities.ProfileCommand{},
		appentities.DeviceCommand{},
		appentities.DeviceAttribute{},
		// Home automation: scenes (grouped commands) and time/sun schedules.
		appentities.Scene{},
		appentities.SceneAction{},
		appentities.Schedule{},
		// The fleet's node-side state (the fleet key, the pairing enrollment, the mTLS cert)
		// lives in the shared runtime_setting table, encrypted at rest.
		sharedentities.RuntimeSetting{},
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
		{Title: "Discovery", Description: "enrollment window and device adoption", Path: "/api/discovery", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Settings", Description: "users and roles", Path: "/api/settings", AccessTier: apiaccessenums.AuthOnly},
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

	// Runtime-editable settings, persisted in the shared RuntimeSetting KV and edited from the
	// Settings page. Two blobs: outbound notification delivery, and the storage/broker knobs.
	appCfg := m.appConfig()
	notificationSettings := services.NewNotificationSettingsService(deps.Db, notificationService)
	// Apply any saved webhook/telegram delivery config to the live hub. Until this runs, myiotsan
	// only writes alerts to its in-app feed — nothing reaches a webhook or telegram.
	if err := notificationSettings.Sync(ctx); err != nil {
		deps.Logger.Warnf("myiotsan.notification", "apply saved delivery config: %v", err)
	}
	telemetrySettings := services.NewTelemetrySettingsService(deps.Db, services.TelemetrySettings{
		RawRetentionDays:    appCfg.Telemetry.RawRetentionDays,
		RollupRetentionDays: appCfg.Telemetry.RollupRetentionDays,
		BatchSize:           appCfg.Telemetry.BatchSize,
		FlushMs:             appCfg.Telemetry.FlushMs,
		QueueSize:           appCfg.Telemetry.QueueSize,
		MqttAddr:            appCfg.MQTT.Addr,
	})
	// Read the effective storage/broker settings ONCE — they are consumed at construction below, so
	// an edit takes effect on the next restart (the Settings > Telemetry tab says so).
	effTelemetry, err := telemetrySettings.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("load telemetry settings: %w", err)
	}

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
	// User and role management. Without it the viewer and operator roles — which have existed
	// since P0 and which the policy catalog names — were UNASSIGNABLE, and the appliance was
	// effectively single-admin.
	apis.NewSettingsApi(protected, localUser, deps.AccessRoles, notificationSettings, telemetrySettings)
	// System controls for the Settings > System tab: a restart (needed to apply the storage/broker
	// settings, which are read once at boot). Version/health are already served by the host runtime.
	apis.NewSystemApi(protected, deps.Restarter)

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
	writer := services.NewReadingWriter(
		dbsql.NewGenericRepo[appentities.DeviceReading](deps.Db),
		services.ReadingWriterOptions{
			BatchSize:     effTelemetry.BatchSize,
			FlushInterval: time.Duration(effTelemetry.FlushMs) * time.Millisecond,
			QueueSize:     effTelemetry.QueueSize,
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

	// Onboarding. A device that does not exist yet cannot connect — that is the whole point of
	// the broker's security model — so an admin opens a time-boxed, key-gated enrollment window
	// and unknown devices are admitted QUARANTINED: what they say becomes a candidate and never
	// telemetry. See services.Enrollment for why that quarantine is the property that makes the
	// hole safe to open at all.
	enrollment := services.NewEnrollment(deps.Db, profileService, func(f string, a ...any) {
		deps.Logger.Infof("myiotsan.enrollment", f, a...)
		// Opening the window is a security event: it must not be possible to do it quietly.
		notificationService.Publish(context.Background(), notification.Notification{
			Category: notification.CategorySystem,
			Severity: notification.Warning,
			Title:    "Device enrollment",
			Body:     fmt.Sprintf(f, a...),
			Source:   "enrollment",
		})
	})
	deviceService.SetEnrollment(enrollment)
	ingest.SetEnrollment(enrollment)

	// The embedded MQTT broker. Embedded, not depended upon: requiring the operator to run
	// Mosquitto alongside would break the single-binary, air-gapped promise that is the product.
	// Its authenticator is the DEVICE TABLE, so a device that is not in the inventory cannot
	// connect at all.
	broker, err := iotmqtt.New(iotmqtt.Options{
		Addr:      effTelemetry.MqttAddr,
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
		RawDays:    effTelemetry.RawRetentionDays,
		RollupDays: effTelemetry.RollupRetentionDays,
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

	// Actuation. Every gate is enforced in the service (read-only by default, admin-only,
	// only-what-the-profile-declares, server-side bounds, rate limit, audit) — and a command is
	// NEVER retried automatically, because re-sending a relay write is a second physical action
	// that could open a door twice. See services/commands.go.
	commandService := services.NewCommandService(deps.Db, deviceService, broker.Publish,
		func(ctx context.Context, msg string, data map[string]any) {
			// Every command — including every REFUSED one — is a security event. "Somebody tried
			// to unlock the front door at 03:00 and was refused" is exactly what must not be
			// thrown away just because it did not succeed.
			notificationService.Publish(ctx, notification.Notification{
				Category: notification.CategorySystem,
				Severity: notification.Warning,
				Title:    "Device command",
				Body:     msg,
				Source:   "actuation",
				Data:     data,
			})
		},
		deps.Metrics,
		func(f string, a ...any) { deps.Logger.Infof("myiotsan.actuation", f, a...) })
	ingest.SetTwin(commandService)

	// Scenes: named groups of commands. Running one fans out through commandService.Issue, so every
	// actuation gate still applies per action — a scene is convenience, not a new authority.
	sceneService := services.NewSceneService(deps.Db, commandService,
		func(f string, a ...any) { deps.Logger.Infof("myiotsan.scenes", f, a...) })

	// Schedules: fire a scene or command on the clock, or at sunrise/sunset. Same actuation path,
	// same gates; the firing actor is a synthetic "schedule:<name>" so the audit trail attributes it.
	scheduleService := services.NewScheduleService(deps.Db, sceneService, commandService,
		func(f string, a ...any) { deps.Logger.Infof("myiotsan.scheduler", f, a...) })

	// Runtime metrics. Everything here instruments a SILENT failure — a dropped reading, a
	// mistuned deadband, a device gone quiet, a command that failed — none of which raises an
	// error a human sees. The ingest gauges are SAMPLED off ingest's own atomic counters rather
	// than instrumented on the publish path, which must stay off any shared lock.
	services.DescribeMetrics(deps.Metrics)
	services.RunMetricsSampler(bgCtx, deps.Metrics, ingest, deviceService, 10*time.Second)

	// Commands the device never confirmed are ENDED, not resent.
	safego.Supervise(bgCtx, "myiotsan.command-sweep", func(ctx context.Context) {
		ticker := time.NewTicker(commandSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				commandService.SweepUnconfirmed(ctx)
			}
		}
	})

	// The Modbus poll loop (P5). SunSpec and vendor register-map devices are POLLED, not published:
	// the app dials OUT to each on its profile's cadence and feeds the decoded samples through the
	// SAME ingest back half a broker message takes. The service reconciles against the inventory on
	// a ticker, so a Modbus device added, edited or disabled in the UI is picked up without a
	// restart — the same live-config property the rest of the app has.
	modbusPoller := services.NewModbusPoller(deviceService, profileService, ingest,
		func(f string, a ...any) { deps.Logger.Infof("myiotsan.modbus", f, a...) })
	safego.Supervise(bgCtx, "myiotsan.modbus-poller", func(ctx context.Context) {
		modbusPoller.Run(ctx, modbusReconcileInterval)
	})

	// The scheduler. Ticks once a minute (aligned to the minute boundary) and fires every schedule
	// due in that minute. Minute granularity is deliberate — a home schedule is "07:30", not
	// "07:30:12" — and it is what the LastFiredAt double-fire guard is keyed to.
	safego.Supervise(bgCtx, "myiotsan.scheduler", func(ctx context.Context) {
		// Align the first tick to the next whole minute so a schedule fires at :00, not at a
		// process-start offset into the minute.
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(time.Now().Truncate(time.Minute).Add(time.Minute))):
		}
		scheduleService.Tick(ctx, time.Now())
		ticker := time.NewTicker(schedulerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				scheduleService.Tick(ctx, time.Now())
			}
		}
	})

	apis.NewDevicesApi(protected, deviceService, telemetry, profileService, ingest)
	apis.NewDiscoveryApi(protected, enrollment, deviceService)
	apis.NewCommandsApi(protected, commandService, deviceService)
	apis.NewProfilesApi(protected, profileService, ingest)
	apis.NewRulesApi(protected, ruleService)
	apis.NewScenesApi(protected, sceneService)
	apis.NewSchedulesApi(protected, scheduleService)
	apis.NewNotificationsApi(protected, notificationService)

	// --- the fleet -------------------------------------------------------------------
	//
	// myiotsan is adopted by myseliasan exactly as mymatasan is, on the same shared node stack.
	// It reports KindIot, so the control plane knows a sensor hub is not a camera node.
	//
	// The event sink registered inside buildFleet is the line that makes the fourth app worth
	// building: every alert this node raises also lands in the control plane's unified feed, and
	// once myseliasan holds events from BOTH camera nodes and sensor nodes it can correlate
	// across them — motion on a camera AND a door opening AND no badge swipe. Neither node can
	// see that alone.
	if boolValue(deps.Config.Pairing.Enabled, true) {
		fleetCipher, cerr := openFleetSecretCipher(deps)
		if cerr != nil {
			stopBackground()
			return nil, cerr
		}
		f := buildFleet(api, deps, appVersion(m), fleetCipher, notificationService)

		// The PUBLIC pairing routes (adopt / release / self-drop) authenticate with the FLEET KEY,
		// not a user session — a control plane adopting a node has no user behind it. They must be
		// registered on the unauthenticated router, before the protected subrouter, or the auth
		// middleware swallows the adopt call and the node can never be adopted at all.
		sharedapis.NewPairingPublicApi(api, f.pairing, f.enrollment.Kick)
		sharedapis.NewPairingApi(protected, f.pairing)

		f.start(bgCtx, deps)
	}

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

// modbusReconcileInterval is how often the poller re-reads the inventory to start/stop/restart
// per-device poll goroutines. It governs how quickly a newly added Modbus device begins polling,
// not the poll cadence itself (which is the profile's PollSeconds).
const modbusReconcileInterval = 30 * time.Second

// schedulerInterval is the automation scheduler's tick. A minute is the resolution a home schedule
// is expressed at ("07:30"), and matches the LastFiredAt double-fire guard's minute granularity.
const schedulerInterval = time.Minute

// appVersion resolves this build's version for the fleet handshake.
func appVersion(m *module) string {
	if manifest, err := versioning.LoadDefault(); err == nil {
		if info, err := manifest.InfoForApp(m.Name()); err == nil {
			return info.AppVersion
		}
	}
	return "0.0.0"
}

// boolValue dereferences an optional bool config flag.
func boolValue(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

// openFleetSecretCipher resolves the at-rest key that protects the node's fleet secrets — the
// fleet key, the pairing token, the mTLS private key. Without it they sit in the database in
// plaintext, and anyone who can read the file can impersonate the node to its control plane.
//
// It FAILS CLOSED when a key that existed before is now missing: minting a new one would
// silently orphan the encrypted enrollment and quietly un-adopt the node from its fleet.
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
	outcome, err := atrest.OpenForStartup(keyPath, recoveryPath, atrest.ProtectorConfig{
		Name:           deps.Config.Security.KeyProtector,
		Passphrase:     deps.Config.Security.Passphrase,
		PassphraseFile: deps.Config.Security.PassphraseFile,
		PassphraseEnv:  deps.Config.Security.PassphraseEnv,
	})
	if err != nil {
		return nil, fmt.Errorf("fleet-secret encryption key: %w", err)
	}
	if outcome.Mode == atrest.ModeRecoveryPending {
		return nil, fmt.Errorf("fleet-secret encryption key missing (id %s): restore %s or set security.recoveryPath, then restart — refusing to orphan this node's fleet enrollment", outcome.KeyId, keyPath)
	}
	return outcome.KeyStore.Cipher(), nil
}

// commandSweepInterval is how often unconfirmed commands are ended. Frequent, because an
// operator staring at a "sent" command needs to be told promptly that it was never confirmed —
// a stale "in progress" is how somebody comes to believe a door is locked when it is not.
const commandSweepInterval = 10 * time.Second

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
