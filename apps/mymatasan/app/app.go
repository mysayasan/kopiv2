package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mmconfig "github.com/mysayasan/kopiv2/apps/mymatasan/config"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	appentities "github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	"github.com/mysayasan/kopiv2/domain/notification"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/versioning"
	"github.com/mysayasan/kopiv2/infra/vision"
)

type module struct {
	// cfg is mymatasan's OWN config — the camera, decoder, stream, vision, health and
	// recording blocks, which used to sit in the shared config.AppConfigModel where they
	// were dead weight for every other app. It is decoded from the same config.json
	// document by DecodeAppConfig, before any route is registered. Never nil at that point:
	// the host aborts startup if decoding fails.
	cfg *mmconfig.Config

	// Health monitors, captured during RegisterAppRoutes so ReadinessStatus can
	// report their advisory state in the /ready payload.
	cameraHealth  *services.CameraHealthMonitor
	machineHealth *services.MachineHealthMonitor
}

// DecodeAppConfig implements apphost.AppConfigDecoder: mymatasan parses its own blocks out
// of the raw config document and resolves its own data-relative paths (the recordings root,
// the YOLO training dir) against the writable data dir.
//
// That path resolution used to live in infra/apphost, which meant the generic application
// host carried hardcoded knowledge of a vision feature. It belongs to the app that owns the
// setting.
func (m *module) DecodeAppConfig(raw []byte, dataDir string) error {
	cfg, err := mmconfig.Load(raw)
	if err != nil {
		return err
	}
	cfg.Normalize(dataDir)
	m.cfg = cfg
	return nil
}

func New() apphost.App {
	return &module{}
}

// ReadinessStatus contributes machine and camera health to the shared /ready
// endpoint (advisory only — it does not affect the ready/not-ready verdict).
func (m *module) ReadinessStatus(_ context.Context) map[string]string {
	out := map[string]string{}
	if m.machineHealth != nil {
		out["machine"] = m.machineHealth.ReadinessStatus()
	}
	if m.cameraHealth != nil {
		out["cameras"] = m.cameraHealth.ReadinessStatus()
	}
	return out
}

func (m *module) Name() string {
	return "mymatasan"
}

func (m *module) BaseDir() string {
	return filepath.Join("apps", "mymatasan")
}

func (m *module) SharedAPIs() apphost.SharedAPIConfig {
	return apphost.SharedAPIConfig{
		Version: true,
	}
}

// Migrations implements apphost.Migrator: the ordered, once-only schema changes the
// additive auto-migrator cannot express.
//
// It is empty, and that is the normal state. An ADDITIVE change — a new field on an entity
// — needs no migration at all; the auto-migrator adds the column. Write one here only for
// what additive cannot do: a rename (which otherwise adds an empty column and strands the
// data in the old one), a drop, a type change, or a data transform.
//
// Rules, because getting them wrong is silent:
//   - IDs are date-prefixed and sortable ("20260714-01-..."). Migrations run in ID order.
//   - NEVER change the ID or the SQL of a released migration. The bootstrapper checksums
//     them and refuses to start if one was edited after being applied — two databases that
//     both claim to have run "20260714-01" must have had the same thing done to them.
//   - The engines genuinely differ (SQLite could not DROP COLUMN before 3.35; none of them
//     agree on changing a column's type), so a structural migration usually needs to say
//     what it means per engine rather than pretend they are the same database.
//
// See infra/db/bootstrap/migration.go.
func (m *module) Migrations() []bootstrap.Migration {
	return []bootstrap.Migration{
		{
			// The rollup gained a per-source dimension; existing tables need the source
			// column added and the old slot unique index dropped (the auto-migrator then
			// recreates it including source). Without this, the maintainer's first
			// source-split insert violates the stale index and rollups stop advancing.
			ID:   "20260806-01-notification-rollup-source",
			Name: "add source to notification_rollup and rebuild the slot index",
			Exec: notification.MigrateRollupSourceColumn,
		},
	}
}

func (m *module) Entities() []any {
	return []any{
		sharedentities.ApiEndpoint{},
		sharedentities.ApiLog{},
		appentities.Camera{},
		appentities.CameraOnvif{},
		appentities.DetectionRule{},
		appentities.AlertEvent{},
		appentities.DetectionClass{},
		appentities.TrainingDataset{},
		appentities.TrainingImage{},
		appentities.TrainingModel{},
		appentities.TeachSkill{},
		appentities.FacePerson{},
		appentities.FaceEmbedding{},
		appentities.RuntimeSetting{},
		appentities.LocalUser{},
		// The shared accessrbac tables. mymatasan uses the shared role + permission MODEL
		// (so the suite has one authorization data model, and myiotsan inherits it) but not
		// the shared middleware, which hard-requires a JWT mymatasan does not have — see
		// apis.NewRequireRolePermission.
		sharedentities.AccessRole{},
		sharedentities.AccessRolePermission{},
		appentities.RecordingSegment{},
		appentities.RecordingConfig{},
		appentities.ObjectObservation{},
		sharedentities.Notification{},
		sharedentities.NotificationRollup{},
		// The append-only audit trail, shared with myidsan and myseliasan. New table on
		// this app; the auto-migrator creates it, so no migration is needed.
		sharedaudit.AuditLog{},
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
		{Title: "ONVIF Discovery", Description: "local ONVIF discovery and probe access", Path: "/api/onvif", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Vision Rules", Description: "AI detection rules and alert events access", Path: "/api/vision", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Runtime Settings", Description: "runtime decoder and stream settings access", Path: "/api/settings", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Local Users", Description: "standalone mymatasan user management access", Path: "/api/settings/users", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Recording", Description: "video recording segments and per-camera recording config access", Path: "/api/recording", AccessTier: apiaccessenums.AuthOnly},
		{Title: "AI Training", Description: "custom-model training datasets and labeled images access", Path: "/api/training", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Teach", Description: "Teach-wizard camera-taught skill access", Path: "/api/teach", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Notifications", Description: "unified notification feed and live stream access", Path: "/api/notifications", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Anomaly Detection", Description: "statistical anomaly monitor settings and on-demand scan access", Path: "/api/anomaly", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Object Observations", Description: "object metadata recorder search (what objects a camera saw) access", Path: "/api/observations", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Pairing", Description: "control-plane discovery and adoption (adopt/release are crypto-authenticated)", Path: "/api/pairing", AccessTier: apiaccessenums.Public},
		{Title: "User Manual", Description: "the built-in manual; public so help works on the sign-in screen and in the first-run wizard", Path: "/api/manual", AccessTier: apiaccessenums.Public},
	}

	coreRbac := make([]string, 0, len(endpoints)*2)
	for _, endpoint := range endpoints {
		coreRbac = append(coreRbac,
			fmt.Sprintf(`INSERT INTO api_endpoint (title, description, app_code, host, path, access_tier, is_active, created_by, created_at, updated_by, updated_at)
SELECT '%s', '%s', 'mymatasan', '*', '%s', %d, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM api_endpoint WHERE app_code = 'mymatasan' AND host = '*' AND path = '%s');`, endpoint.Title, endpoint.Description, endpoint.Path, endpoint.AccessTier, endpoint.Path),
			fmt.Sprintf(`UPDATE api_endpoint SET app_code = 'mymatasan', access_tier = %d WHERE host = '*' AND path = '%s' AND ((access_tier IS NULL OR access_tier <> %d) OR app_code IS NULL OR app_code = '');`, endpoint.AccessTier, endpoint.Path, endpoint.AccessTier),
		)
	}

	seeders := []bootstrap.Seeder{
		bootstrap.NewSQLSeeder("mymatasan-endpoints", coreRbac),
		// Backfill the is_diagnostic flag for alert rows created before the column
		// existed. ALTER TABLE ADD COLUMN leaves existing rows NULL, which the bool
		// row scanner cannot read, so every NULL must be set to a concrete value:
		// diagnostic samples to TRUE, everything else to FALSE. The IS NULL guard
		// makes both statements a no-op once applied.
		bootstrap.NewSQLSeeder("mymatasan-alert-diagnostic-backfill", []string{
			`UPDATE alert_event SET is_diagnostic = TRUE WHERE is_diagnostic IS NULL AND metadata LIKE '%"diagnostic":true%';`,
			`UPDATE alert_event SET is_diagnostic = FALSE WHERE is_diagnostic IS NULL;`,
		}),
		// The camera health columns are added via ALTER TABLE, which leaves existing
		// rows NULL. The int64 row scanner cannot read a NULL bigint, so backfill
		// last_health_check_at to 0; health_status is NULL-safe but normalized too.
		bootstrap.NewSQLSeeder("mymatasan-camera-health-backfill", []string{
			`UPDATE camera SET last_health_check_at = 0 WHERE last_health_check_at IS NULL;`,
			`UPDATE camera SET health_status = '' WHERE health_status IS NULL;`,
		}),
		// The metadata-recorder columns are added to recording_config via ALTER TABLE,
		// which leaves existing rows NULL. The bool/int row scanner cannot read a NULL,
		// so backfill both to their disabled defaults. The IS NULL guard makes this a
		// no-op once applied.
		bootstrap.NewSQLSeeder("mymatasan-recording-metadata-backfill", []string{
			`UPDATE recording_config SET metadata_enabled = FALSE WHERE metadata_enabled IS NULL;`,
			`UPDATE recording_config SET metadata_gap_seconds = 0 WHERE metadata_gap_seconds IS NULL;`,
		}),
		// Secondary indexes for the object-observation search (the ORM only emits unique
		// indexes from struct tags). CREATE INDEX IF NOT EXISTS is valid on sqlite,
		// postgres, and mariadb, so one statement set covers every engine.
		bootstrap.NewSQLSeeder("mymatasan-object-observation-indexes", []string{
			`CREATE INDEX IF NOT EXISTS ix_object_observation_camera_started ON object_observation (camera_id, started_at);`,
			`CREATE INDEX IF NOT EXISTS ix_object_observation_label_started ON object_observation (label, started_at);`,
		}),
	}

	if len(seedStatements) > 0 {
		seeders = append(seeders, bootstrap.NewSQLSeeder("config", seedStatements))
	}

	return seeders
}

func (m *module) RegisterAppRoutes(api *mux.Router, deps apphost.Dependencies) (apphost.ShutdownFunc, error) {
	// Route recovered background-goroutine panics into the app logger before anything
	// starts, so a panic is never lost to stdout on a service install. Until this runs,
	// safego falls back to the standard logger — it is never silent.
	safego.SetLogger(func(component string, format string, args ...any) {
		deps.Logger.Warnf(component, format, args...)
	})

	// Register metric help text so a /metrics scrape is readable by whoever is debugging
	// a customer box, not just by whoever wrote the instrumentation.
	// mymatasan's own config blocks, decoded by DecodeAppConfig before we got here.
	appCfg := m.cfg

	services.DescribeMetrics(deps.Metrics)
	recording.DescribeMetrics(deps.Metrics)

	// Encryption-at-rest FIRST, before any service is built: a missing key must not race a
	// service into writing plaintext. RECOVERY mode mounts only the public recovery gate
	// and starts nothing else until the operator restores the key.
	atrestBoot, err := openAtRest(api, deps)
	if err != nil {
		return nil, err
	}
	if atrestBoot.RecoveryPending {
		return func(context.Context) error { return nil }, nil
	}
	atrestKeyStore, atrestCipher := atrestBoot.KeyStore, atrestBoot.Cipher

	repo := newRepos(deps.Db)

	cameraService := services.NewCameraService(repo.Camera, repo.CameraOnvif, onvif.NewClient(), rtsp.NewClient())
	visionService := services.NewVisionService(repo.DetectionRule, repo.AlertEvent)
	detectionClassService := services.NewDetectionClassService(repo.DetectionClass)
	if err := detectionClassService.EnsureBuiltins(context.Background(), appCfg.Vision.Detector.ClassMap); err != nil {
		deps.Logger.Warnf("mymatasan.vision", "seed detection classes failed: %v", err)
	}
	// The detector's worker script and model-pointer paths, resolved once into a typed
	// value. Consumers take it as a parameter, so the compiler enforces the ordering that
	// used to be a comment: this previously MUTATED deps.Config in place, and three later
	// constructors silently depended on that write having already happened.
	detectorPaths := resolveDetectorModelPaths(deps, appCfg)
	// The one place that publishes the model pointers into the process environment, where
	// the Python workers read them. See detectorModelPaths for why this still exists.
	detectorPaths.PublishToProcessEnv()

	// Built once and shared between the live monitor and the training auto-labeler.
	objectBackend := buildObjectDetectorBackend(deps, appCfg, detectorPaths)

	trainingService := services.NewTrainingService(
		repo.TrainingDataset,
		repo.TrainingImage,
		repo.TrainingModel,
		visionService,
		detectionClassService,
		detectorPaths.TrainingDir,
		detectorPaths.ActiveModelFile,
		detectorPaths.StockModelFile,
		detectorPaths.LPRModelFile,
		objectBackend,
		appCfg.Vision.Detector.MinObjectConfidence,
		trainingRunConfigFromAppConfig(appCfg, deps.ConfigPath, detectorPaths.DetectorArgs),
		atrestCipher,
	)
	// Face recognition: the enrollment embedder (one-shot YuNet+SFace worker) + the global gallery
	// service. Off until an admin enrolls someone AND a camera has a face rule; the model files are
	// downloaded by the face-recognition setup, so a fresh install has the feature dormant.
	faceEmbedder := services.NewPythonFaceEmbedder(
		appCfg.Vision.Detector.Command, detectorPaths.FacesWorkerScript,
		detectorPaths.FaceYunetFile, detectorPaths.FaceSfaceFile,
		func(f string, a ...any) { deps.Logger.Infof("mymatasan.faces", f, a...) })
	faceGalleryService := services.NewFaceGalleryService(
		repo.FacePerson, repo.FaceEmbedding, atrestCipher, faceEmbedder,
		detectorPaths.FacesGalleryFile, trainingService.ReloadDetector,
		func(f string, a ...any) { deps.Logger.Infof("mymatasan.faces", f, a...) })

	settingsService := services.NewRuntimeSettingsService(repo.RuntimeSetting, runtimeSettingsFromAppConfig(appCfg))
	setupStateService := sharedservices.NewSetupStateService(repo.RuntimeSetting)
	pairingService := services.NewPairingService(repo.RuntimeSetting, atrestCipher, "", "")
	localUserService := services.NewLocalUserService(repo.LocalUser, deps.AccessRoles)
	shredPasses := resolveShredPasses(appCfg)
	// The at-rest recording codec is NOT captured here any more: RecorderConfigBuilder
	// reads it live on every build, so a Settings → Recording change applies the next time
	// a recorder is configured instead of waiting for a restart. Only the NVENC semaphore
	// is sized from the boot-time value, because it must be set before any recorder starts.
	recSettings, _ := settingsService.Recording(context.Background())
	recording.SetNVENCConcurrency(recSettings.Storage.MaxConcurrentEncodes)
	recordingService := services.NewRecordingService(repo.RecordingSegment, repo.RecordingConfig, shredPasses)
	// Metadata recorder: logs "what objects each camera saw" as searchable presence
	// intervals, reusing the detector's inference (no second decode). The recorder is
	// the write side (fed candidates via the detector's observation sink); the
	// observation service is the read/maintenance side (search + footage linkage + purge).
	metadataRecorder := services.NewMetadataRecorder(repo.ObjectObservation, recordingService, appCfg.Vision.Detector.MinObjectConfidence)
	observationService := services.NewObservationService(repo.ObjectObservation, recordingService)
	notificationService := notification.NewService(repo.Notification, notificationOptionsFromAppConfig(deps.Config, deps.Logger, deps.Metrics))
	// Incrementally aggregate the notifications feed into the hourly rollup table
	// that powers the dashboard's baseline/anomaly analytics (Phase 0). The cursor
	// persists the last-folded notification id so sweeps are exactly-once and the
	// first sweep on an existing database backfills all history.
	// Enable the dashboard analytics reads (heatmap + future baselines) off the rollup.
	notificationService.WithRollups(repo.NotificationRollup)
	notificationRollupMaintainer := notification.NewRollupMaintainer(
		repo.Notification,
		repo.NotificationRollup,
		services.NewRollupCursor(repo.RuntimeSetting),
		0,
		0,
	)
	notificationSettingsService := services.NewNotificationSettingsService(
		repo.RuntimeSetting,
		notificationService,
		notificationSettingsDefaultsFromAppConfig(deps.Config),
	)
	healthSettingsService := services.NewHealthSettingsService(
		repo.RuntimeSetting,
		healthSettingsDefaultsFromAppConfig(appCfg),
	)
	machineHealthSettingsService := services.NewMachineHealthSettingsService(
		repo.RuntimeSetting,
		services.DefaultMachineHealthSettings(),
	)
	anomalySettingsService := services.NewAnomalySettingsService(
		repo.RuntimeSetting,
		services.DefaultAnomalySettings(),
	)
	// Load persisted notification delivery settings and apply them to the hub.
	if err := notificationSettingsService.Sync(context.Background()); err != nil {
		deps.Logger.Warnf("mymatasan.notification", "load notification settings failed: %v", err)
	}
	// Roles BEFORE the admin is seeded: the bootstrap admin has to be given the superadmin
	// role, and an existing install's users have to be carried across from the legacy
	// IsAdmin bool. Both need the roles to exist first.
	if err := services.EnsureRoles(context.Background(), deps.AccessRoles, deps.AccessPerms); err != nil {
		return nil, fmt.Errorf("seed authorization roles: %w", err)
	}
	adminRole, err := deps.AccessRoles.GetByName(context.Background(), services.RoleAdmin)
	if err != nil || adminRole == nil {
		return nil, fmt.Errorf("resolve %s role: %w", services.RoleAdmin, err)
	}
	operatorRole, err := deps.AccessRoles.GetByName(context.Background(), services.RoleOperator)
	if err != nil || operatorRole == nil {
		return nil, fmt.Errorf("resolve %s role: %w", services.RoleOperator, err)
	}
	// Carry an existing install across: every user without a role gets one, derived from the
	// legacy IsAdmin bool. Non-admins become OPERATORS — today's non-admin can already review
	// footage and acknowledge alerts, and demoting them to viewer would silently take that
	// away. See ILocalUserService.BackfillRoles.
	if migrated, err := localUserService.BackfillRoles(context.Background(), adminRole.Id, operatorRole.Id); err != nil {
		return nil, fmt.Errorf("assign roles to existing users: %w", err)
	} else if migrated > 0 {
		deps.Logger.Infof("mymatasan.rbac", "assigned a role to %d existing user(s) from the legacy admin flag", migrated)
	}

	adminSeed, err := localUserService.EnsureDefaultAdmin(context.Background(), deps.Config.LocalAuth.Username, deps.Config.LocalAuth.Password)
	if err != nil {
		return nil, fmt.Errorf("seed local admin user failed: %w", err)
	}
	if adminSeed.Seeded {
		// Fresh install: surface the bootstrap login. On Windows the GUI installer
		// owns the reveal (finish page); everywhere else — CLI, Docker, systemd,
		// portable — this console banner (and a recovery file) is the only place the
		// operator learns the credentials.
		announceFirstRunAdmin(deps, adminSeed)
	} else {
		// One-shot admin reset: the Windows installer's "Reset admin login" option
		// (a reinstall over an existing data dir whose admin password is unknown)
		// drops a marker in the data dir + injects LOCAL_ADMIN_PASSWORD. Consume the
		// marker exactly once — deleting it before acting — so a normal service
		// restart never re-runs the reset and clobbers the operator's chosen password.
		if marker := filepath.Join(deps.DataDir, adminResetMarkerFile); fileExists(marker) {
			if rmErr := os.Remove(marker); rmErr != nil {
				deps.Logger.Warnf("mymatasan.setup", "could not consume admin-reset marker %s (skipping reset to avoid a loop): %v", marker, rmErr)
			} else if reset, rErr := localUserService.ResetAdmin(context.Background(), deps.Config.LocalAuth.Username, deps.Config.LocalAuth.Password); rErr != nil {
				deps.Logger.Warnf("mymatasan.setup", "admin reset failed: %v", rErr)
			} else if reset.Seeded {
				deps.Logger.Infof("mymatasan.setup", "admin login reset on request (must change on next login)")
				announceFirstRunAdmin(deps, reset)
			}
		}
	}
	// Resolve the ffmpeg path from persisted settings, for the live-view bridge and the
	// NVENC probe. Recorders no longer read it from here — RecorderConfigBuilder resolves
	// it (and the RTSP transport) live on every build.
	ffmpegPath := ""
	if dec, err := settingsService.Decoder(context.Background()); err == nil {
		ffmpegPath = dec.MJPEG.FFmpegPath
	}

	// ffmpegPath lets the live-view bridge transcode non-G.711 camera audio (AAC,
	// etc.) to Opus so live view has sound regardless of the camera's codec.
	streamManager := stream.NewManagerWithOptions(stream.Options{FFmpegPath: ffmpegPath})

	// Build the recording manager from persisted per-camera configs.
	// Each camera is configured in its own goroutine so RTSP URI lookups and
	// ffmpeg process launches happen in parallel across all cameras.
	recorderManager := recording.NewManager(recordingService)

	// Camera-delete cascade. Deleting a camera row alone left its recorder running
	// (ffmpeg still connected, still writing segments) and its detection rules alive
	// (the vision monitor kept sampling a camera that no longer existed, logging a
	// capture-failed diagnostic every interval) — both until the next restart. It also
	// stranded the camera's footage: retention is driven off the recording config, so
	// with the camera gone nothing would ever purge it.
	//
	// Cleanups run in order and any failure aborts the delete, so a camera is never
	// half-removed. Registered here rather than inside the camera service because the
	// camera service owns none of these subsystems.
	if cascade, ok := cameraService.(services.CameraDeletionCascade); ok {
		// Stop the writers first, so nothing is producing new segments while we purge.
		cascade.AddCameraCleanup(func(_ context.Context, cameraId int64) error {
			recorderManager.StopDetectionStream(cameraId)
			return recorderManager.Configure(recording.RecorderConfig{CameraId: cameraId, Enabled: false})
		})
		cascade.AddCameraCleanup(func(ctx context.Context, cameraId int64) error {
			_, err := recordingService.PurgeAllForCamera(ctx, cameraId)
			return err
		})
		cascade.AddCameraCleanup(func(ctx context.Context, cameraId int64) error {
			_, err := observationService.PurgeAllForCamera(ctx, cameraId)
			return err
		})
		cascade.AddCameraCleanup(func(ctx context.Context, cameraId int64) error {
			_, err := visionService.DeleteRulesForCamera(ctx, cameraId)
			return err
		})
		cascade.AddCameraCleanup(func(ctx context.Context, cameraId int64) error {
			_, err := visionService.PurgeAlertsForCamera(ctx, cameraId)
			return err
		})
		// Last: the config row that the purges above are driven from.
		cascade.AddCameraCleanup(func(ctx context.Context, cameraId int64) error {
			return recordingService.DeleteConfigForCamera(ctx, cameraId)
		})
	}

	// Teach wizard: skill registry + session capture engine. Constructed here
	// (not with the other services) because its frame source needs the recorder
	// manager for the siphon path.
	teachService := services.NewTeachService(
		repo.TeachSkill,
		detectionClassService,
		trainingService,
		visionService,
		services.NewDetectionSource(cameraService, recorderManager, settingsService, nil),
		teachDetectorConfig(appCfg, detectorPaths),
		appVersion(m.Name()),
		atrestCipher,
	)
	// One builder, used by every site that needs to turn a stored recording config into a
	// runnable recorder: this startup fan-out, the settings-save handler, and the
	// detect-only stream resolver. They used to each construct a RecorderConfig by hand,
	// which is how ShredPasses got silently dropped and how the at-rest codec ended up
	// captured at boot.
	recorderConfigBuilder := services.NewRecorderConfigBuilder(
		cameraService, recordingService, settingsService, atrestCipher, shredPasses, deps.Metrics,
	)

	if cfgs, err := recordingService.ListConfigs(context.Background()); err != nil {
		// Previously swallowed: on error the whole fan-out was skipped and recording
		// silently never started.
		deps.Logger.Warnf("mymatasan.recording", "could not list recording configs at startup — no recorder started: %v", err)
	} else {
		var wg sync.WaitGroup
		for _, cfg := range cfgs {
			wg.Add(1)
			go func(cfg *appentities.RecordingConfig) {
				defer wg.Done()
				recCfg, warning := recorderConfigBuilder.ForRecording(context.Background(), cfg)
				if warning != "" {
					deps.Logger.Warnf("mymatasan.recording", "cam%d: %s", cfg.CameraId, warning)
				}
				if err := recorderManager.Configure(recCfg); err != nil {
					deps.Logger.Warnf("mymatasan.recording", "cam%d: configure recorder: %v", cfg.CameraId, err)
				}
			}(cfg)
		}
		wg.Wait()
	}
	// Warm the NVENC capability probe in the background when the storage codec
	// re-encodes, so the recording-storage status endpoint (and its incompatibility
	// warning) answers instantly instead of running ffmpeg on the first request. This is
	// a cache warm-up, so the boot-time codec is the right value to use even though the
	// recorders themselves now read it live.
	if bootCodec := recSettings.Storage.Codec; recording.ReEncodes(bootCodec) {
		safego.Go("mymatasan.recording.nvenc-probe", func() {
			recording.StorageCodecUsable(ffmpegPath, bootCodec)
		})
	}

	// Built before route registration so the camera API can expose an on-demand
	// health probe; started later alongside the other background monitors.
	cameraHealthMonitor := services.NewCameraHealthMonitor(cameraService, rtsp.NewClient(), healthSettingsService, notificationService, deps.Metrics)
	m.cameraHealth = cameraHealthMonitor

	// Host (machine) health monitor: samples CPU/memory/disk and runs disk
	// mitigation. Auto-monitored volumes are the ones the app writes to — the
	// working dir, the snapshot/recordings dir, the log dir, and each camera's
	// recording storage path — plus any custom paths from settings.
	machineAutoPaths := []string{"."}
	if sd := strings.TrimSpace(appCfg.Vision.SnapshotDir); sd != "" {
		machineAutoPaths = append(machineAutoPaths, sd)
	}
	if lp := strings.TrimSpace(deps.Config.Logging.Path); lp != "" {
		machineAutoPaths = append(machineAutoPaths, filepath.Dir(lp))
	}
	if recConfigs, err := recordingService.ListConfigs(context.Background()); err == nil {
		for _, rc := range recConfigs {
			if rc != nil && strings.TrimSpace(rc.StoragePath) != "" {
				machineAutoPaths = append(machineAutoPaths, rc.StoragePath)
			}
		}
	}
	machineHealthMonitor := services.NewMachineHealthMonitor(machineHealthSettingsService, notificationService, recorderManager, recordingService, machineAutoPaths, deps.Metrics)
	m.machineHealth = machineHealthMonitor

	loginGuard := apis.NewLoginGuard(loginGuardConfigFromAppConfig(deps.Config))
	var loginLockoutNotifier services.INotificationPublisher
	if deps.Config.LoginSecurity.Effective().NotifyOnLockout {
		loginLockoutNotifier = notificationService
	}

	// The node side of the fleet: enrollment, control and media channels. All three are
	// dialed BY the node, so it can sit behind NAT with no inbound ports open.
	fleetChannels := buildFleet(api, deps, appVersion(m.Name()), pairingService, cameraService, streamManager, notificationService)

	// ffmpeg installs under the writable data dir (not the CWD): a packaged Windows service
	// runs with CWD=C:\Windows\System32, so a CWD-relative "bin" would land ffmpeg there
	// instead of MYMATASAN_DATA. Mirrors the Python runtime, which lives under
	// dataDir/pyruntime.
	ffmpegBinDir, _ := filepath.Abs(filepath.Join(deps.DataDir, "bin"))

	// Everything is built. Gather it once, and let the remaining phases take ONE parameter
	// instead of thirty free variables out of an 800-line scope.
	// The running version, resolved once. Both the self-update service and the evidence
	// export manifest need it, and a second manifest read would be the start of a drift.
	currentVersion := ""
	if manifest, err := versioning.LoadDefault(); err == nil {
		if info, err := manifest.InfoForApp(m.Name()); err == nil {
			currentVersion = info.AppVersion
		}
	}

	// The append-only audit trail. Built before the wiring bag because the handlers that
	// record into it are constructed from that bag, and because a trail that is wired late
	// is a trail that quietly misses the first actions after boot.
	//
	// The trusted-proxy list is the rate limiter's, so "which hops may set
	// X-Forwarded-For" has exactly one answer in this app — an untrusted caller must not
	// be able to forge the address recorded against their own deletion.
	auditService := services.WithAuditMetrics(
		services.NewAuditService(deps.Db, func(format string, args ...any) {
			deps.Logger.Warnf("mymatasan.audit", format, args...)
		}),
		deps.Metrics,
	)
	auditApi := apis.NewAuditor(auditService, deps.Config.RateLimit.TrustedProxies)

	w := &wiring{
		deps:           deps,
		appCfg:         appCfg,
		atrestKeyStore: atrestKeyStore,
		atrestCipher:   atrestCipher,
		detectorPaths:  detectorPaths,
		objectBackend:  objectBackend,
		httpsPort:      fleetChannels.httpsPort,

		camera:         cameraService,
		vision:         visionService,
		detectionClass: detectionClassService,
		training:       trainingService,
		teach:          teachService,
		faceGallery:    faceGalleryService,
		recording:      recordingService,
		observation:    observationService,
		metadata:       metadataRecorder,
		localUser:      localUserService,
		setupState:     setupStateService,
		pairing:        pairingService,

		settings:              settingsService,
		notificationSettings:  notificationSettingsService,
		healthSettings:        healthSettingsService,
		machineHealthSettings: machineHealthSettingsService,
		anomalySettings:       anomalySettingsService,

		notification:       notificationService,
		notificationRollup: notificationRollupMaintainer,
		recorder:           recorderManager,
		recorderConfig:     recorderConfigBuilder,
		streamManager:      streamManager,
		cameraHealth:       cameraHealthMonitor,
		machineHealth:      machineHealthMonitor,

		enrollment: fleetChannels.enrollment,
		control:    fleetChannels.control,
		media:      fleetChannels.media,

		loginGuard:           loginGuard,
		loginLockoutNotifier: loginLockoutNotifier,
		accessRoles:          deps.AccessRoles,
		accessPerms:          deps.AccessPerms,

		audit:        auditApi,
		auditService: auditService,

		continuitySettings: services.NewContinuitySettingsService(repo.RuntimeSetting),

		// Evidence export. Work lands under the data dir rather than the OS temp dir: a
		// bundle is DECRYPTED footage, and it belongs on the volume the operator already
		// governs, not somewhere a system cleaner may or may not reach.
		evidence: services.NewEvidenceExportService(
			recordingService, cameraService, atrestCipher,
			ffmpegPath,
			apphost.ResolveWritablePath(deps.DataDir, "exports"),
			currentVersion,
		),

		ffmpegInstaller: services.NewFFmpegInstaller(ffmpegBinDir, settingsService),
		pythonInstaller: services.NewPythonInstaller(deps.DataDir, deps.ConfigPath),
	}

	// Fail fast on a forgotten field rather than nil-dereferencing deep inside a phase.
	if err := w.validate(); err != nil {
		return nil, err
	}

	protected := registerRoutes(api, w)

	// Factory reset ("Secure Wipe & Reset"): shred all media, drop + rebuild + reseed
	// the database (honouring the configured engine), then restart into first-run
	// state. Runtime settings reset naturally because they live in the dropped DB.
	// Gated by bootstrap.allowReset; the restart primitive (deps.Restarter) is shared
	// with future self-update.
	resetMediaPaths := func(ctx context.Context) []string {
		paths := []string{}
		if sd := strings.TrimSpace(appCfg.Vision.SnapshotDir); sd != "" {
			paths = append(paths, sd)
		}
		if detectorPaths.TrainingDir != "" {
			paths = append(paths, detectorPaths.TrainingDir)
		}
		if fp := strings.TrimSpace(deps.Config.FileStorage.Path); fp != "" {
			paths = append(paths, fp)
		}
		if cfgs, err := recordingService.ListConfigs(ctx); err == nil {
			for _, c := range cfgs {
				if c != nil && strings.TrimSpace(c.StoragePath) != "" {
					paths = append(paths, c.StoragePath)
				}
			}
		}
		return paths
	}

	monitorCtx, stopMonitor := context.WithCancel(context.Background())

	// Vision monitor settings: assembled here because they thread together the detector,
	// the recorder, the notifier and the metadata sink, none of which the settings mapper
	// can know about.
	monitorSettings := visionMonitorSettingsFromAppConfig(appCfg)
	monitorSettings.Detector = wrapMonitorDetector(appCfg, objectBackend)
	monitorSettings.Recorder = recorderManager
	monitorSettings.Notifier = notificationService
	monitorSettings.NotificationDestinations = notificationSettingsService
	monitorSettings.Resolver = detectionClassService
	monitorSettings.SnapshotCipher = atrestCipher
	monitorSettings.Metadata = metadataRecorder
	monitorSettings.Metrics = deps.Metrics
	// The detection-only frame stream the monitor runs when a camera has AI rules but NVR
	// recording is off. Same builder as the real recorder, so the AI sees the same stream
	// it would have recorded.
	monitorSettings.DetectStreamConfig = recorderConfigBuilder.ForDetectOnly
	// Wire the metadata recorder as the detector's observation sink, so rule-bearing
	// cameras record metadata for free off their existing inference.
	if oc, ok := monitorSettings.Detector.(vision.ObservationCapable); ok {
		oc.SetObservationSink(metadataRecorder)
	}
	w.visionMonitorSettings = monitorSettings

	startBackgroundWorkers(monitorCtx, w)

	// Factory reset (Secure Wipe & Reset). Built here — after the monitors and
	// recorder exist — so its StopServices hook can quiesce them before wiping: the
	// recorder's ffmpeg holds the live .ts segment open (and keeps writing), which
	// otherwise leaves files behind and stalls the shred on a near-full disk.
	systemResetService := services.NewSystemResetService(services.SystemResetConfig{
		CollectMediaPaths: resetMediaPaths,
		ShredPasses:       shredPasses,
		BootstrapOpts: bootstrap.Options{
			AppName: m.Name(),
			Config:  deps.Config.Db,
			Bootstrap: bootstrap.BootstrapConfig{
				Enabled:            deps.Config.Bootstrap.Enabled,
				AutoCreateDatabase: deps.Config.Bootstrap.AutoCreateDatabase,
				AutoCreateSchema:   deps.Config.Bootstrap.AutoCreateSchema,
				AutoMigrate:        deps.Config.Bootstrap.AutoMigrate,
				AutoSeed:           deps.Config.Bootstrap.AutoSeed,
				AllowReset:         deps.Config.Bootstrap.AllowReset,
				SetupPath:          deps.Config.Bootstrap.SetupPath,
				SeedStatements:     deps.Config.Bootstrap.SeedStatements,
			},
			Entities: m.Entities(),
			// The reset drops and rebuilds the database, so the rebuilt one is FRESH and
			// baselines these rather than running them. Omitting them here would be a trap:
			// the rebuild would save a manifest (so the next boot reads as "not fresh") with
			// an empty migration table, and every migration would then be replayed against a
			// brand-new schema — and fail.
			Migrations: m.Migrations(),
			Seeders:    m.Seeders(deps.Config.Bootstrap.SeedStatements),
		},
		Restarter: deps.Restarter,
		KeyStore:  atrestKeyStore,
		StopServices: func() {
			stopMonitor()           // vision + camera/host health monitors + purge loops
			recorderManager.Close() // kills ffmpeg, releasing the open .ts segments
			if c, ok := objectBackend.(io.Closer); ok {
				_ = c.Close() // stops the detector worker process
			}
		},
		// Close our DB pool right before the wipe drops the database. Required for sqlite
		// on Windows, where the file can't be deleted while this process holds it open.
		CloseDatabase: func() error {
			if c, ok := deps.Db.(io.Closer); ok {
				return c.Close()
			}
			return nil
		},
	})
	// The reset gate middleware, registered before this existed, reads it through a closure
	// at request time — so publishing it here is what arms the gate.
	w.systemReset = systemResetService

	// Self-update: check GitHub Releases (scheduled + on demand) and, on portable/
	// installer installs, download+verify+swap the binary/assets and restart.
	updateService := services.NewUpdateService(currentVersion, deps.HomeDir, deps.Restarter)
	updateService.CleanupStaleFiles()
	if deps.Scheduler != nil {
		updateService.RegisterScheduler(deps.Scheduler.StartPeriodic)
	}
	// The recovery-escrow endpoints need the key store; pass nil (not a typed-nil
	// interface) when encryption-at-rest is off so the endpoints cleanly report unavailable.
	var escrowStore interface {
		Enabled() bool
		Protector() string
		ExportEscrow(string) ([]byte, error)
		VerifyEscrow(string, []byte) (bool, error)
	}
	if atrestKeyStore != nil {
		escrowStore = atrestKeyStore
	}
	apis.NewSystemApi(protected, systemResetService, deps.Restarter, updateService, escrowStore)

	// Settings → Backup & Restore: a portable, passphrase-encrypted bundle of
	// cameras, AI detection, notifications, and app settings so a fresh install can
	// be recovered without reconfiguring. Reuses the repos wired above.
	backupService := services.NewBackupService(
		repo.Camera,
		repo.CameraOnvif,
		repo.RecordingConfig,
		repo.DetectionClass,
		repo.DetectionRule,
		repo.RuntimeSetting,
		currentVersion,
	)
	apis.NewBackupApi(protected, backupService)

	return func(ctx context.Context) error {
		stopMonitor()
		recorderManager.Close()
		_ = notificationService.Close(ctx)
		if closer, ok := monitorSettings.Detector.(io.Closer); ok {
			_ = closer.Close()
		}
		if closer, ok := trainingService.(io.Closer); ok {
			_ = closer.Close()
		}
		return streamManager.Close()
	}, nil
}

// firstRunCredentialFile is the recovery file (in the data dir) that holds the
// bootstrap login, so it survives the console banner scrolling away.
const firstRunCredentialFile = "INITIAL_ADMIN_LOGIN.txt"

// adminResetMarkerFile is the one-shot marker the Windows installer's "reset admin
// login" option drops in the data dir to request a password reset on next start.
// The app deletes it before acting so the reset runs exactly once.
const adminResetMarkerFile = "RESET_ADMIN"

// fileExists reports whether path is an existing (non-directory-agnostic) file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// announceFirstRunAdmin surfaces the bootstrap admin credentials established this
// run (a fresh seed or a one-shot reset). It always writes a recovery file in the
// data dir — the reliable, platform-independent place to find the login (Windows
// service consoles are invisible; a Docker/journal banner scrolls away) — and
// prints a console banner. The password is echoed in the banner only when it was
// generated (the operator doesn't already know it); a config/env-supplied one is
// pointed at, not logged. The account is must-change on next sign-in.
func announceFirstRunAdmin(deps apphost.Dependencies, seed services.AdminSeedResult) {
	url := firstRunConsoleURL(deps.Config)
	credPath := filepath.Join(deps.DataDir, firstRunCredentialFile)
	saved := writeFirstRunCredentialFile(credPath, url, seed) == nil
	if !saved {
		deps.Logger.Warnf("mymatasan.setup", "could not write credential recovery file %s", credPath)
	}

	const bar = "======================================================================"
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n", bar)
	fmt.Fprint(&b, "  MyMataSan is ready.\n\n")
	fmt.Fprintf(&b, "  Open:      %s\n", url)
	fmt.Fprintf(&b, "  Username:  %s\n", seed.Username)
	if seed.Generated {
		fmt.Fprintf(&b, "  Password:  %s\n", seed.Password)
		fmt.Fprint(&b, "\n  This one-time password was generated for this install; you must set\n")
		fmt.Fprint(&b, "  your own on first sign-in.\n")
	} else {
		fmt.Fprint(&b, "  Password:  the value from your config.json localAuth / LOCAL_ADMIN_PASSWORD\n")
		fmt.Fprint(&b, "\n  You must change it on first sign-in.\n")
	}
	if saved {
		fmt.Fprintf(&b, "\n  Saved to:  %s  (delete after you sign in)\n", credPath)
	}
	fmt.Fprintf(&b, "%s\n", bar)
	// Print to stdout so it lands verbatim in `docker logs` / journal / the terminal,
	// then log a password-free line for the persistent log.
	fmt.Print(b.String())
	deps.Logger.Infof("mymatasan.setup", "bootstrap admin %q established (must change on next login); sign-in details written to %s", seed.Username, credPath)
}

// firstRunConsoleURL builds the console sign-in URL from the server config: https
// when a TLS port is configured, otherwise http, on the first configured port
// (defaulting to 3000). Host is localhost; the operator opens it on the box or
// substitutes the host's LAN address.
func firstRunConsoleURL(cfg *config.AppConfigModel) string {
	scheme := "http"
	port := 0
	switch {
	case len(cfg.Server.TLSPorts) > 0:
		scheme = "https"
		port = cfg.Server.TLSPorts[0]
	case len(cfg.Server.NonTLSPorts) > 0:
		port = cfg.Server.NonTLSPorts[0]
	case len(cfg.Server.Ports) > 0:
		port = cfg.Server.Ports[0]
	}
	if port == 0 {
		port = 3000
	}
	return fmt.Sprintf("%s://localhost:%d", scheme, port)
}

// writeFirstRunCredentialFile persists the generated bootstrap login (0600) so it
// is recoverable after the console banner scrolls away.
func writeFirstRunCredentialFile(path, url string, seed services.AdminSeedResult) error {
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o750)
	}
	body := "MyMataSan - one-time administrator login\n" +
		"=========================================\n\n" +
		"Open:      " + url + "\n" +
		"Username:  " + seed.Username + "\n" +
		"Password:  " + seed.Password + "\n\n" +
		"You will be asked to set your own password on first sign-in.\n" +
		"Delete this file once you have signed in.\n"
	return os.WriteFile(path, []byte(body), 0o600)
}

// resolveShredPasses decides how many secure-overwrite passes to apply when
// deleting recorded footage. Shredding is on by default (DefaultShredPasses);
// recording.shred.enabled=false disables it (plain delete), and a positive
// recording.shred.passes overrides the count.
// periodic runs fn once immediately, then on every interval tick, until ctx is done.
//
// It is supervised: a panic inside fn restarts the loop with backoff instead of killing
// the process. That matters more than it looks — these loops are the retention purges,
// and a dead purge loop is invisible. Nothing re-creates it, so the disk simply fills
// until every write fails, the database included.
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

// buildTrainingObjectDetector builds the raw object-detection backend used to
// auto-label training images. It mirrors the backend selection in
// visionDetectorFromAppConfig but returns the unwrapped ObjectDetector (no rule
// mapping / motion dispatch). Auto-label requires an external/persistent object
// detector; motion mode has no object backend.
// resolveDetectorScriptArgs makes any relative Python worker script in the detector
// args absolute against homeDir, so the worker is found no matter the process working
// directory. Non-script args are left untouched.
func resolveDetectorScriptArgs(homeDir string, args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = resolveDetectorScript(homeDir, arg)
	}
	return out
}

// resolveDetectorScript resolves one worker-script arg. It prefers the script relative
// to homeDir (the intended "ai/yolo_worker.py" layout), then recovers legacy
// repo-root-relative values (e.g. "./apps/mymatasan/ai/yolo_worker.py") by basename in
// <homeDir>/ai, and finally leaves the value untouched (CWD-relative, old behavior).
func resolveDetectorScript(homeDir, arg string) string {
	a := strings.TrimSpace(arg)
	if a == "" || filepath.IsAbs(a) || !strings.HasSuffix(strings.ToLower(a), ".py") {
		return arg
	}
	if p, err := filepath.Abs(filepath.Join(homeDir, a)); err == nil && isRegularFile(p) {
		return p
	}
	if p, err := filepath.Abs(filepath.Join(homeDir, "ai", filepath.Base(a))); err == nil && isRegularFile(p) {
		return p
	}
	return arg
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func buildTrainingObjectDetector(detectorCfg mmconfig.VisionDetectorConfigModel) (vision.ObjectDetector, error) {
	mode := strings.ToLower(strings.TrimSpace(detectorCfg.Mode))
	timeout := time.Duration(detectorCfg.TimeoutMs) * time.Millisecond
	switch mode {
	case vision.DetectorModeExternal, vision.DetectorModeHybrid:
		return vision.NewExternalObjectDetector(vision.ExternalObjectDetectorOptions{
			Command: detectorCfg.Command,
			Args:    detectorCfg.Args,
			Timeout: timeout,
		})
	case vision.DetectorModePersistent, "externalpersistent", "external-persistent", "external_persistent":
		return vision.NewPersistentObjectDetector(vision.PersistentObjectDetectorOptions{
			Command: detectorCfg.Command,
			Args:    detectorCfg.Args,
			Timeout: timeout,
		})
	default:
		return nil, fmt.Errorf("auto-label requires an external or persistent object detector (current mode %q)", detectorCfg.Mode)
	}
}

// wrapMonitorDetector wraps the shared object backend into the live monitor's
// detector: rule mapping via ObjectRuleDetector, plus optional motion-intrusion
// dispatch. A nil backend (motion mode, or backend build failure) falls back to
// the native motion detector.
func wrapMonitorDetector(cfg *mmconfig.Config, backend vision.ObjectDetector) vision.Detector {
	detectorCfg := cfg.Vision.Detector
	mode := strings.ToLower(strings.TrimSpace(detectorCfg.Mode))
	if mode == "" {
		mode = vision.DetectorModeMotion
	}
	motionDetector := vision.NewMotionDetector()
	if mode == vision.DetectorModeMotion || backend == nil {
		return motionDetector
	}

	source := "persistent-yolo-detector"
	if mode == vision.DetectorModeExternal || mode == vision.DetectorModeHybrid {
		source = "external-object-detector"
	}
	objectDetector := vision.NewObjectRuleDetector(backend, vision.ObjectRuleDetectorOptions{
		ClassMap:            detectorCfg.ClassMap,
		MinObjectConfidence: detectorCfg.MinObjectConfidence,
		Source:              source,
	})
	// External (non-persistent) mode historically ran object-only, no motion dispatch.
	if mode == vision.DetectorModeExternal {
		return objectDetector
	}
	motionTypes := []string{}
	if boolValue(detectorCfg.UseMotionIntrusion, true) {
		motionTypes = append(motionTypes, vision.DetectionIntrusion)
	}
	if len(motionTypes) == 0 {
		return objectDetector
	}
	return vision.NewDispatchDetector(vision.DispatchDetectorOptions{
		Object:      objectDetector,
		Motion:      motionDetector,
		MotionTypes: motionTypes,
	})
}

// appVersion resolves this app's version from the version manifest for the control
// channel Hello (best-effort; empty when the manifest is unavailable).
func appVersion(appName string) string {
	manifest, err := versioning.LoadDefault()
	if err != nil {
		return ""
	}
	info, err := manifest.InfoForApp(appName)
	if err != nil {
		return ""
	}
	return info.AppVersion
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
			Title:       "mymatasan API",
			Version:     docVersion,
			Description: "Runtime-generated OpenAPI docs for shared and app-specific endpoints.",
		},
		Endpoints: map[string]apidocs.EndpointDoc{
			"GET /api/deployment/preflight": {
				Summary: "Deployment answer (single instance by design)",
				Description: "Read-only. Reports that mymatasan cannot be replicated behind a load balancer, with the reason: " +
					"it owns capture pipelines, writes recordings to local disk, and is GPU-affine, so a second instance would " +
					"not share that hardware — it would compete for it. There is no mode to set; the answer is fixed. Use " +
					"active/passive with shared storage instead. The suite's clusterable apps are myseliasan and myidsan.",
				Tags: []string{"deployment"},
			},
			"GET /health": {
				Summary:     "Service liveness",
				Description: "Returns service alive status.",
				Tags:        []string{"system"},
			},
			"GET /ready": {
				Summary:     "Service readiness",
				Description: "Checks database connectivity and runtime readiness.",
				Tags:        []string{"system"},
			},
			"GET /setup": {
				Summary:     "Bootstrap setup page",
				Description: "Shows current bootstrap status page.",
				Tags:        []string{"bootstrap"},
			},
			"GET /setup/status": {
				Summary:     "Bootstrap status",
				Description: "Returns JSON bootstrap readiness and migration state.",
				Tags:        []string{"bootstrap"},
			},
			"GET /api/health": {
				Summary:     "API namespace health",
				Description: "Quick health check under /api prefix.",
				Tags:        []string{"system"},
			},
			"GET /api/version": {
				Summary:     "Runtime version",
				Description: "Returns the running app version and shared core version.",
				Tags:        []string{"system"},
			},
			"POST /api/onvif/discover": {
				Summary:     "Discover ONVIF devices",
				Description: "Sends a local WS-Discovery probe and returns discovered ONVIF devices.",
				Tags:        []string{"onvif"},
			},
			"POST /api/onvif/probe": {
				Summary:     "Probe ONVIF device",
				Description: "Checks one manually entered IP or ONVIF device-service URL.",
				Tags:        []string{"onvif"},
			},
			"GET /api/onvif/devices": {
				Summary:     "List saved ONVIF devices",
				Description: "Returns saved ONVIF device records with pagination.",
				Tags:        []string{"onvif"},
			},
			"GET /api/onvif/stream-config": {
				Summary:     "Get live-view stream config",
				Description: "Returns whether browser live view should use WebRTC, MJPEG fallback, and configured WebRTC ICE servers.",
				Tags:        []string{"stream"},
			},
			"GET /api/settings/runtime": {
				Summary:     "Get runtime settings",
				Description: "Returns decoder and stream settings persisted in the local database.",
				Tags:        []string{"settings"},
			},
			"PUT /api/settings/runtime": {
				Summary:     "Update runtime settings",
				Description: "Updates decoder and stream settings without restarting the app.",
				Tags:        []string{"settings"},
			},
			"POST /api/settings/runtime/auto-tune": {
				Summary:     "Auto-tune decoder runtime settings",
				Description: "Inspects saved camera RTSP metadata and local ffmpeg capabilities, then applies conservative decoder settings.",
				Tags:        []string{"settings"},
			},
			"GET /api/settings/runtime/gpu-devices": {
				Summary:     "List decoder GPU devices",
				Description: "Returns selectable local GPU or hardware decoder device values for the runtime decoder GPU/device setting.",
				Tags:        []string{"settings"},
			},
			"POST /api/settings/runtime/reset": {
				Summary:     "Reset runtime settings",
				Description: "Resets runtime settings to the startup config defaults.",
				Tags:        []string{"settings"},
			},
			"GET /api/settings/decoder/status": {
				Summary:     "ffmpeg availability",
				Description: "Reports whether a usable ffmpeg is found (found, path, version) for the FFmpeg-path status indicator and setup wizard.",
				Tags:        []string{"settings"},
			},
			"POST /api/settings/decoder/ffmpeg/install": {
				Summary:     "Install ffmpeg",
				Description: "Starts the background in-app ffmpeg download/installer and returns its initial state. Admin only.",
				Tags:        []string{"settings"},
			},
			"GET /api/settings/decoder/ffmpeg/install/status": {
				Summary:     "ffmpeg install status",
				Description: "Polls the background ffmpeg install job (running, status, log, path, supported).",
				Tags:        []string{"settings"},
			},
			"GET /api/settings/fs/browse": {
				Summary:     "Browse server filesystem",
				Description: "Admin-only, read-only directory picker used to choose the ffmpeg binary; lists one directory level confined to a whitelist of roots.",
				Tags:        []string{"settings"},
			},
			"GET /api/settings/notification": {
				Summary:     "Get notification settings",
				Description: "Returns runtime-editable notification delivery settings (webhook, telegram, retention).",
				Tags:        []string{"settings"},
			},
			"PUT /api/settings/notification": {
				Summary:     "Update notification settings",
				Description: "Updates notification delivery settings and reconfigures the live delivery channels without a restart.",
				Tags:        []string{"settings"},
			},
			"POST /api/settings/notification/test": {
				Summary:     "Send test notification",
				Description: "Dispatches a test notification at the given severity so webhook/telegram configuration can be verified.",
				Tags:        []string{"settings"},
			},
			"GET /api/settings/health": {
				Summary:     "Get camera health settings",
				Description: "Returns the runtime-editable camera health monitor settings (enabled, interval, timeout, failure/recovery thresholds).",
				Tags:        []string{"settings"},
			},
			"PUT /api/settings/health": {
				Summary:     "Update camera health settings",
				Description: "Updates the camera health monitor settings; the monitor reads them live on the next sweep, so changes apply without a restart.",
				Tags:        []string{"settings"},
			},
			"GET /api/settings/vision/ai-tool/status": {
				Summary:     "Check AI tool readiness",
				Description: "Checks the configured external AI detector command, Python packages, worker script, model file, and native fallback status.",
				Tags:        []string{"settings"},
			},
			"GET /api/settings/vision/ai-runtime/status": {
				Summary:     "AI runtime status",
				Description: "Reports whether the self-contained AI Python runtime (Python + torch + ultralytics) is installed, its path, and whether CUDA is available.",
				Tags:        []string{"settings"},
			},
			"POST /api/settings/vision/ai-runtime/install": {
				Summary:     "Install AI runtime",
				Description: "Starts the background installer that downloads a self-contained Python and pip-installs torch (GPU build when an NVIDIA GPU is present, else CPU) + ultralytics, then points the detector at it. Admin only.",
				Tags:        []string{"settings"},
			},
			"GET /api/settings/vision/ai-runtime/install/status": {
				Summary:     "AI runtime install status",
				Description: "Polls the background AI-runtime install job (running, status, log, python path, supported).",
				Tags:        []string{"settings"},
			},
			"GET /api/settings/users": {
				Summary:     "List local users",
				Description: "Returns standalone mymatasan login users. Admin local user required.",
				Tags:        []string{"settings"},
			},
			"POST /api/settings/users": {
				Summary:     "Create local user",
				Description: "Creates a standalone mymatasan login user with a bcrypt password hash. Admin local user required.",
				Tags:        []string{"settings"},
			},
			"PUT /api/settings/users/{id}": {
				Summary:     "Update local user",
				Description: "Updates username, display name, admin flag, and active flag. Admin local user required.",
				Tags:        []string{"settings"},
			},
			"POST /api/settings/users/{id}/password": {
				Summary:     "Reset local user password",
				Description: "Resets a standalone mymatasan user's password. Admin local user required.",
				Tags:        []string{"settings"},
			},
			"DELETE /api/settings/users/{id}": {
				Summary:     "Delete local user",
				Description: "Deletes a standalone mymatasan login user. The last active admin cannot be deleted.",
				Tags:        []string{"settings"},
			},
			"GET /api/vision/rules": {
				Summary:     "List AI detection rules",
				Description: "Returns saved AI detection rules for local cameras.",
				Tags:        []string{"vision"},
			},
			"POST /api/vision/rules": {
				Summary:     "Save AI detection rule",
				Description: "Creates or updates a detection rule with target camera, detection type, zone polygon, optional ruleConfig for line crossing, rule-level schedule policy, threshold, cooldown, and alert options.",
				Tags:        []string{"vision"},
			},
			"DELETE /api/vision/rules/{id}": {
				Summary:     "Delete AI detection rule",
				Description: "Deletes a saved AI detection rule by ID.",
				Tags:        []string{"vision"},
			},
			"GET /api/vision/alerts": {
				Summary:     "List AI alert events",
				Description: "Returns AI alert events raised by detection rules.",
				Tags:        []string{"vision"},
			},
			"POST /api/vision/alerts": {
				Summary:     "Create AI alert event",
				Description: "Creates an alert event for manual tests or detector workers.",
				Tags:        []string{"vision"},
			},
			"POST /api/vision/alerts/{id}/ack": {
				Summary:     "Acknowledge AI alert",
				Description: "Marks one AI alert event as acknowledged.",
				Tags:        []string{"vision"},
			},
			"GET /api/notifications": {
				Summary:     "List notifications",
				Description: "Returns the unified notification feed (vision alerts, health checks, system events). Supports paging, cameraId, and unread filters.",
				Tags:        []string{"notification"},
			},
			"GET /api/notifications/stream": {
				Summary:     "Stream notifications (SSE)",
				Description: "Server-Sent Events stream that pushes new notifications to connected clients in real time.",
				Tags:        []string{"notification"},
			},
			"POST /api/notifications/{id}/read": {
				Summary:     "Mark notification read",
				Description: "Marks one notification as read by the current user.",
				Tags:        []string{"notification"},
			},
			"POST /api/notifications/purge": {
				Summary:     "Purge old notifications",
				Description: "Deletes notifications older than the olderThanDays query parameter. Set onlyRead=true to keep unread notifications.",
				Tags:        []string{"notification"},
			},
			"POST /api/onvif/devices": {
				Summary:     "Save ONVIF device",
				Description: "Creates or updates a saved ONVIF device record by XAddr.",
				Tags:        []string{"onvif"},
			},
			"POST /api/onvif/devices/discovered": {
				Summary:     "Save discovered ONVIF device",
				Description: "Creates or updates a saved ONVIF device record from a discovery result.",
				Tags:        []string{"onvif"},
			},
			"POST /api/onvif/devices/{id}/stream-uri": {
				Summary:     "Resolve RTSP stream URI",
				Description: "Uses ONVIF media services to resolve and save a selected media profile to an RTSP URI.",
				Tags:        []string{"onvif"},
			},
			"POST /api/onvif/devices/{id}/stream-options": {
				Summary:     "List RTSP stream options",
				Description: "Uses ONVIF media services to list every media profile with its RTSP URI so stream1/stream2 can be selected.",
				Tags:        []string{"onvif"},
			},
			"POST /api/onvif/devices/{id}/camera-password": {
				Summary:     "Change camera ONVIF password",
				Description: "Uses ONVIF Device Management SetUser to update the saved camera user's password.",
				Tags:        []string{"onvif"},
			},
			"POST /api/onvif/devices/{id}/rtsp-test": {
				Summary:     "Probe RTSP stream",
				Description: "Checks whether the saved RTSP URI can be described and set up.",
				Tags:        []string{"rtsp"},
			},
			"POST /api/onvif/devices/{id}/live-view": {
				Summary:     "Prepare MJPEG live view",
				Description: "Resolves and stores the ONVIF snapshot URI used for browser MJPEG live view.",
				Tags:        []string{"onvif"},
			},
			"POST /api/onvif/devices/{id}/ptz/move": {
				Summary:     "Move PTZ camera",
				Description: "Uses ONVIF PTZ ContinuousMove for saved cameras that expose PTZ capability.",
				Tags:        []string{"ptz"},
			},
			"POST /api/onvif/devices/{id}/ptz/stop": {
				Summary:     "Stop PTZ camera",
				Description: "Uses ONVIF PTZ Stop for saved cameras that expose PTZ capability.",
				Tags:        []string{"ptz"},
			},
			"POST /api/onvif/devices/{id}/webrtc/offer": {
				Summary:     "Create WebRTC live-view answer",
				Description: "Answers a browser WebRTC offer and forwards the saved camera H264 RTSP stream as live video.",
				Tags:        []string{"stream"},
			},
			"GET /api/onvif/devices/{id}/live.mjpeg": {
				Summary:     "MJPEG live view",
				Description: "Streams a browser-friendly multipart MJPEG view from ONVIF snapshot frames.",
				Tags:        []string{"onvif"},
			},
			"DELETE /api/onvif/devices/{id}": {
				Summary:     "Delete ONVIF device",
				Description: "Deletes a saved ONVIF device record by ID.",
				Tags:        []string{"onvif"},
			},
		},
	}
}
