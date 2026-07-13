package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	appentities "github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	applog "github.com/mysayasan/kopiv2/infra/logging"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/pairing"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/telemetry"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/versioning"
	"github.com/mysayasan/kopiv2/infra/vision"
)

type module struct {
	// Health monitors, captured during RegisterAppRoutes so ReadinessStatus can
	// report their advisory state in the /ready payload.
	cameraHealth  *services.CameraHealthMonitor
	machineHealth *services.MachineHealthMonitor
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
		appentities.RuntimeSetting{},
		appentities.LocalUser{},
		appentities.RecordingSegment{},
		appentities.RecordingConfig{},
		appentities.ObjectObservation{},
		sharedentities.Notification{},
		sharedentities.NotificationRollup{},
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
	services.DescribeMetrics(deps.Metrics)
	recording.DescribeMetrics(deps.Metrics)

	// Resolve the encryption-at-rest master key FIRST, before building any service. When
	// encryption is on and the key is missing but a key existed here before, the app enters
	// RECOVERY mode: it mounts only the public recovery gate and returns, so nothing else
	// starts (no service writes plaintext, no replacement key is minted) until the operator
	// restores the key. The key lives OUTSIDE the media roots so a factory reset destroys it
	// explicitly (KeyStore.Destroy) — the instant, device-independent secure wipe.
	var atrestKeyStore *atrest.KeyStore
	var atrestCipher *atrest.Cipher
	if boolValue(deps.Config.Security.EncryptAtRest, true) {
		keyPath := strings.TrimSpace(deps.Config.Security.KeyPath)
		if keyPath == "" {
			// ResolveWritablePath keeps an existing legacy key (pre-packaging:
			// CWD/secret/atrest.key) in place so upgrades don't orphan encrypted footage.
			keyPath, _ = filepath.Abs(apphost.ResolveWritablePath(deps.DataDir, filepath.Join("secret", "atrest.key")))
		}
		// A disaster-recovery escrow placed here (or configured) is restored automatically
		// on boot when no key exists yet. Defaults to recovery.atrestkey beside the key.
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
			return nil, fmt.Errorf("encryption-at-rest key: %w", err)
		}
		if outcome.Mode == atrest.ModeRecoveryPending {
			apis.NewRecoveryGateApi(api, keyPath, protectorCfg, deps.Restarter, outcome.KeyId)
			log.Printf("encryption-at-rest: master key missing but a key existed here before (id %s) — starting in RECOVERY mode; open the app to restore it", outcome.KeyId)
			return func(context.Context) error { return nil }, nil
		}
		atrestKeyStore = outcome.KeyStore
		atrestCipher = atrestKeyStore.Cipher()
		protector := strings.TrimSpace(deps.Config.Security.KeyProtector)
		if protector == "" {
			protector = "file"
		}
		if outcome.Mode == atrest.ModeRestored {
			log.Printf("encryption-at-rest: restored master key from recovery file %s", recoveryPath)
		}
		log.Printf("encryption-at-rest enabled (key %s, protector %s, id %s)", keyPath, protector, outcome.KeyId)
	}

	cameraRepo := dbsql.NewGenericRepo[appentities.Camera](deps.Db)
	cameraOnvifRepo := dbsql.NewGenericRepo[appentities.CameraOnvif](deps.Db)
	detectionRuleRepo := dbsql.NewGenericRepo[appentities.DetectionRule](deps.Db)
	alertEventRepo := dbsql.NewGenericRepo[appentities.AlertEvent](deps.Db)
	detectionClassRepo := dbsql.NewGenericRepo[appentities.DetectionClass](deps.Db)
	trainingDatasetRepo := dbsql.NewGenericRepo[appentities.TrainingDataset](deps.Db)
	trainingImageRepo := dbsql.NewGenericRepo[appentities.TrainingImage](deps.Db)
	runtimeSettingsRepo := dbsql.NewGenericRepo[appentities.RuntimeSetting](deps.Db)
	localUserRepo := dbsql.NewGenericRepo[appentities.LocalUser](deps.Db)
	recordingSegmentRepo := dbsql.NewGenericRepo[appentities.RecordingSegment](deps.Db)
	recordingConfigRepo := dbsql.NewGenericRepo[appentities.RecordingConfig](deps.Db)

	cameraService := services.NewCameraService(cameraRepo, cameraOnvifRepo, onvif.NewClient(), rtsp.NewClient())
	visionService := services.NewVisionService(detectionRuleRepo, alertEventRepo)
	detectionClassService := services.NewDetectionClassService(detectionClassRepo)
	if err := detectionClassService.EnsureBuiltins(context.Background(), deps.Config.Vision.Detector.ClassMap); err != nil {
		deps.Logger.Warnf("mymatasan.vision", "seed detection classes failed: %v", err)
	}
	// Resolve the detector's Python worker script to an absolute path against the app
	// HomeDir so it is found regardless of the process working directory: a dev run from
	// the repo root, or the staged bin/ bundle (where the script sits in <HomeDir>/ai and
	// a repo-root-relative config path would otherwise double up as
	// <bin>/apps/mymatasan/ai/...). Downstream consumers (live detector, auto-labeler,
	// and the training script/base-model paths derived from the worker dir) all read the
	// resolved args. Also drives MYMATASAN_YOLO worker discovery for training.
	deps.Config.Vision.Detector.Args = resolveDetectorScriptArgs(deps.HomeDir, deps.Config.Vision.Detector.Args)
	// Build the object-detection backend once and share it between the live
	// monitor and the training auto-labeler. The YOLO worker reads the active-model
	// pointer file (set via env) so a trained/imported model can be hot-swapped.
	trainingDir := trainingDataDir(deps.Config)
	activeModelFile, _ := filepath.Abs(filepath.Join(trainingDir, "active_model.txt"))
	_ = os.Setenv("MYMATASAN_ACTIVE_MODEL_FILE", activeModelFile)
	stockModelFile, _ := filepath.Abs(filepath.Join(trainingDir, "stock_model.txt"))
	_ = os.Setenv("MYMATASAN_STOCK_MODEL_FILE", stockModelFile)
	lprModelFile, _ := filepath.Abs(filepath.Join(trainingDir, "lpr_model.txt"))
	_ = os.Setenv("MYMATASAN_LPR_MODEL_FILE", lprModelFile)
	// Activated Teach anomaly skills register here; the worker scores listed
	// cameras against their normal-memory banks on every frame.
	anomalyManifestFile, _ := filepath.Abs(filepath.Join(trainingDir, "anomaly_models.json"))
	_ = os.Setenv("MYMATASAN_ANOMALY_FILE", anomalyManifestFile)
	objectBackend, backendErr := buildTrainingObjectDetector(deps.Config.Vision.Detector)
	if backendErr != nil {
		deps.Logger.Warnf("mymatasan.vision", "object detector backend unavailable (%v); auto-label and custom models are disabled", backendErr)
		objectBackend = nil
	}
	trainingModelRepo := dbsql.NewGenericRepo[appentities.TrainingModel](deps.Db)
	trainingService := services.NewTrainingService(
		trainingDatasetRepo,
		trainingImageRepo,
		trainingModelRepo,
		visionService,
		detectionClassService,
		trainingDir,
		activeModelFile,
		stockModelFile,
		lprModelFile,
		objectBackend,
		deps.Config.Vision.Detector.MinObjectConfidence,
		trainingRunConfigFromAppConfig(deps.Config, deps.ConfigPath),
		atrestCipher,
	)
	settingsService := services.NewRuntimeSettingsService(runtimeSettingsRepo, runtimeSettingsFromAppConfig(deps.Config))
	setupStateService := services.NewSetupStateService(runtimeSettingsRepo)
	pairingService := services.NewPairingService(runtimeSettingsRepo, atrestCipher, "", "")
	localUserService := services.NewLocalUserService(localUserRepo)
	shredPasses := resolveShredPasses(deps.Config)
	// At-rest recording codec is runtime-editable (Settings → Recording); read the
	// effective value (seeded from config) so a UI change survives restart.
	recSettings, _ := settingsService.Recording(context.Background())
	recStorage := recSettings.Storage
	recordCodec, recordQuality := recStorage.Codec, recStorage.Quality
	// Auto-fallback to stream-copy when the GPU re-encode can't run; defaults on.
	recordFallbackCopy := recStorage.FallbackToCopy == nil || *recStorage.FallbackToCopy
	// Size the shared NVENC semaphore before any recorder starts so remux-time
	// re-encoding (and playback transcode) never oversubscribes the GPU.
	recording.SetNVENCConcurrency(recStorage.MaxConcurrentEncodes)
	recordingService := services.NewRecordingService(recordingSegmentRepo, recordingConfigRepo, shredPasses)
	// Metadata recorder: logs "what objects each camera saw" as searchable presence
	// intervals, reusing the detector's inference (no second decode). The recorder is
	// the write side (fed candidates via the detector's observation sink); the
	// observation service is the read/maintenance side (search + footage linkage + purge).
	objectObservationRepo := dbsql.NewGenericRepo[appentities.ObjectObservation](deps.Db)
	metadataRecorder := services.NewMetadataRecorder(objectObservationRepo, recordingService, deps.Config.Vision.Detector.MinObjectConfidence)
	observationService := services.NewObservationService(objectObservationRepo, recordingService)
	notificationRepo := dbsql.NewGenericRepo[sharedentities.Notification](deps.Db)
	notificationService := notification.NewService(notificationRepo, notificationOptionsFromAppConfig(deps.Config, deps.Logger, deps.Metrics))
	// Incrementally aggregate the notifications feed into the hourly rollup table
	// that powers the dashboard's baseline/anomaly analytics (Phase 0). The cursor
	// persists the last-folded notification id so sweeps are exactly-once and the
	// first sweep on an existing database backfills all history.
	notificationRollupRepo := dbsql.NewGenericRepo[sharedentities.NotificationRollup](deps.Db)
	// Enable the dashboard analytics reads (heatmap + future baselines) off the rollup.
	notificationService.WithRollups(notificationRollupRepo)
	notificationRollupMaintainer := notification.NewRollupMaintainer(
		notificationRepo,
		notificationRollupRepo,
		services.NewRollupCursor(runtimeSettingsRepo),
		0,
		0,
	)
	notificationSettingsService := services.NewNotificationSettingsService(
		runtimeSettingsRepo,
		notificationService,
		notificationSettingsDefaultsFromAppConfig(deps.Config),
	)
	healthSettingsService := services.NewHealthSettingsService(
		runtimeSettingsRepo,
		healthSettingsDefaultsFromAppConfig(deps.Config),
	)
	machineHealthSettingsService := services.NewMachineHealthSettingsService(
		runtimeSettingsRepo,
		services.DefaultMachineHealthSettings(),
	)
	anomalySettingsService := services.NewAnomalySettingsService(
		runtimeSettingsRepo,
		services.DefaultAnomalySettings(),
	)
	// Load persisted notification delivery settings and apply them to the hub.
	if err := notificationSettingsService.Sync(context.Background()); err != nil {
		deps.Logger.Warnf("mymatasan.notification", "load notification settings failed: %v", err)
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
	// Resolve ffmpeg path and RTSP transport from persisted settings.
	ffmpegPath := ""
	rtspTransport := ""
	if dec, err := settingsService.Decoder(context.Background()); err == nil {
		ffmpegPath = dec.MJPEG.FFmpegPath
		rtspTransport = dec.FFmpeg.RTSPTransport
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
	teachSkillRepo := dbsql.NewGenericRepo[appentities.TeachSkill](deps.Db)
	teachService := services.NewTeachService(
		teachSkillRepo,
		detectionClassService,
		trainingService,
		visionService,
		services.NewDetectionSource(cameraService, recorderManager, settingsService, nil),
		services.TeachDetectorConfig{
			Command:         deps.Config.Vision.Detector.Command,
			Args:            deps.Config.Vision.Detector.Args,
			TimeoutMs:       deps.Config.Vision.Detector.TimeoutMs,
			AnomalyManifest: anomalyManifestFile,
		},
		appVersion(m.Name()),
		atrestCipher,
	)
	siphonFPS, siphonWidth := services.SiphonTeeParams(settingsService)
	siphonHWAccel, siphonHWDevice, siphonInitHWDevice, siphonVideoDecoder := services.SiphonDecoderParams(settingsService)
	if cfgs, err := recordingService.ListConfigs(context.Background()); err == nil {
		var wg sync.WaitGroup
		for _, cfg := range cfgs {
			wg.Add(1)
			go func(cfg *appentities.RecordingConfig) {
				defer wg.Done()
				// Prefer the explicit StreamURL override; fall back to the ONVIF-discovered URI.
				// Always fetch device credentials so they can be injected into bare URLs.
				rtspURI := strings.TrimSpace(cfg.StreamURL)
				fallbackURI := strings.TrimSpace(cfg.FallbackStreamUrl)
				if src, err := cameraService.SnapshotSource(context.Background(), uint64(cfg.CameraId)); err == nil {
					if rtspURI == "" {
						rtspURI = src.RTSPURI
					} else {
						rtspURI = services.RTSPURIWithCredentials(rtspURI, src.Username, src.Password)
					}
					fallbackURI = services.RTSPURIWithCredentials(fallbackURI, src.Username, src.Password)
				}
				_ = recorderManager.Configure(recording.RecorderConfig{
					CameraId:           cfg.CameraId,
					Enabled:            cfg.Enabled,
					PreRollSec:         cfg.PreRollSec,
					PostRollSec:        cfg.PostRollSec,
					StoragePath:        cfg.StoragePath,
					FFmpegPath:         ffmpegPath,
					RTSPTransport:      rtspTransport,
					RTSPURI:            rtspURI,
					FallbackRTSPURI:    fallbackURI,
					SegmentMinutes:     cfg.SegmentMinutes,
					RetentionDays:      cfg.RetentionDays,
					SiphonFPS:          siphonFPS,
					SiphonWidth:        siphonWidth,
					HWAccel:            siphonHWAccel,
					HWAccelDevice:      siphonHWDevice,
					InitHWDevice:       siphonInitHWDevice,
					VideoDecoder:       siphonVideoDecoder,
					ShredPasses:        shredPasses,
					RecordCodec:        recordCodec,
					RecordQuality:      recordQuality,
					RecordFallbackCopy: recordFallbackCopy,
					Cipher:             atrestCipher,
					Metrics:            deps.Metrics,
				})
			}(cfg)
		}
		wg.Wait()
	}
	// Warm the NVENC capability probe in the background when the storage codec
	// re-encodes, so the recording-storage status endpoint (and its incompatibility
	// warning) answers instantly instead of running ffmpeg on the first request.
	if recording.ReEncodes(recordCodec) {
		go recording.StorageCodecUsable(ffmpegPath, recordCodec)
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
	if sd := strings.TrimSpace(deps.Config.Vision.SnapshotDir); sd != "" {
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
	if deps.Config.LoginSecurity.NotifyOnLockout {
		loginLockoutNotifier = notificationService
	}

	// HTTPS port advertised to the control plane in discovery announces and used as
	// the adoption-call origin.
	httpsPort := 0
	if len(deps.Config.Server.TLSPorts) > 0 {
		httpsPort = deps.Config.Server.TLSPorts[0]
	}

	// Node mTLS enrollment manager: after adoption it enrolls with the control-plane
	// fleet CA, serves the mutual-TLS management listener (heartbeat/release), and
	// renews its certificate before expiry. Kicked on adopt; runs in the monitor
	// lifecycle below.
	pairingRenewBefore := time.Duration(deps.Config.Pairing.RenewBeforeHours) * time.Hour
	enrollmentManager := services.NewEnrollmentManager(
		pairingService,
		deps.Config.Pairing.MTLSPort,
		pairingRenewBefore,
		func(format string, args ...any) { deps.Logger.Infof("mymatasan.pairing", format, args...) },
	)

	// Control channel: once paired + enrolled, the node dials the control plane's
	// control listener over mTLS and maintains a persistent bi-directional channel
	// (commander commands down, events up). Reads state live from the pairing
	// service; shares the monitor lifecycle alongside the enrollment manager.
	// The dispatcher re-injects tunneled commands into this app's own /api router
	// (built below) so the commander reaches the node's exact API surface, gated by
	// the node's normal authorization via the synthetic principal it carries.
	controlChannel := services.NewControlChannelManager(
		pairingService,
		deps.Config.Pairing.ControlPort,
		appVersion(m.Name()),
		apis.NewControlDispatcher(api),
		func(format string, args ...any) { deps.Logger.Infof("mymatasan.control", format, args...) },
	)
	// Forward this node's notifications up the control channel so the control plane's
	// unified feed receives them live (in addition to local delivery).
	notificationService.Register(services.NewControlEventSink(controlChannel))

	// Media channel: a second node-dialed mTLS connection (separate port) that streams
	// a camera's RTP up to the control plane on request, so myseliasan can re-broadcast
	// full-frame-rate live view over WebRTC. Resolves each camera's RTSP via the same
	// snapshot-source path used for browser live view, and shares the stream manager's
	// RTSP sessions. Shares the monitor lifecycle alongside the control channel.
	mediaResolve := func(ctx context.Context, camID uint64) (stream.Source, error) {
		src, err := cameraService.SnapshotSource(ctx, camID)
		if err != nil {
			return stream.Source{}, err
		}
		return stream.Source{ID: fmt.Sprintf("camera-%d", camID), URI: src.RTSPURI}, nil
	}
	mediaChannel := services.NewMediaChannelManager(
		pairingService,
		deps.Config.Pairing.MediaPort,
		appVersion(m.Name()),
		streamManager,
		mediaResolve,
		func(format string, args ...any) { deps.Logger.Infof("mymatasan.media", format, args...) },
	)

	// Pairing adopt/release are called by the control plane (no local session) and
	// authenticate cryptographically, so they mount on the public /api router — and
	// must be registered before the session catch-all so requests match here first.
	apis.NewPairingPublicApi(api, pairingService, enrollmentManager.Kick)

	// systemResetService is built further down (after the monitors/recorder exist), but
	// the reset gate below needs to consult it, so declare it here and read it via a
	// closure at request time.
	var systemResetService *services.SystemResetService

	protected := api.PathPrefix("").Subrouter()
	// Shed load with a clean 503 while a factory reset is running: the reset closes the DB
	// pool and keeps serving through the slow free-space scrub, so DB-backed requests would
	// otherwise 500 (including the login probe) until the restart. Runs FIRST — before auth —
	// so a request never hits the closed database.
	protected.Use(apis.NewResetGate(func() bool { return systemResetService != nil && systemResetService.InProgress() }))
	protected.Use(apis.NewLocalBasicAuth(localUserService, loginGuard, loginLockoutNotifier))
	// Non-admins are read-only (plus a small viewer allow-list); admins get full
	// access. Registered after auth so the authenticated user is in context.
	protected.Use(apis.NewRequireAdminForWrites())
	apis.NewLocalAuthApi(protected, localUserService)
	apis.NewOnvifApi(protected, cameraService, settingsService, streamManager)
	apis.NewCameraApi(protected, cameraService, settingsService, streamManager, cameraHealthMonitor)
	apis.NewVisionApi(protected, visionService, detectionClassService, recorderManager, notificationService, cameraService, settingsService, notificationSettingsService, atrestCipher)
	apis.NewTrainingApi(protected, trainingService)
	apis.NewTeachApi(protected, teachService)
	// ffmpeg installs under the writable data dir (not the CWD): a packaged
	// Windows service runs with CWD=C:\Windows\System32, so a CWD-relative "bin"
	// would land ffmpeg there instead of MYMATASAN_DATA. Mirrors the Python
	// runtime, which lives under dataDir/pyruntime.
	ffmpegBinDir, _ := filepath.Abs(filepath.Join(deps.DataDir, "bin"))
	ffmpegInstaller := services.NewFFmpegInstaller(ffmpegBinDir, settingsService)
	pythonInstaller := services.NewPythonInstaller(deps.DataDir, deps.ConfigPath)
	apis.NewSettingsApi(protected, settingsService, cameraService, localUserService, notificationSettingsService, healthSettingsService, machineHealthSettingsService, machineHealthMonitor, visionToolSettingsFromAppConfig(deps.Config), ffmpegInstaller, pythonInstaller, deps.Config.Decoder.BrowseRoots)
	apis.NewRecordingApi(protected, recordingService, recorderManager, cameraService, settingsService, atrestCipher, visionService, shredPasses, deps.Metrics)
	apis.NewObservationApi(protected, observationService)
	apis.NewNotificationApi(protected, notificationService)
	apis.NewAnomalyApi(protected, anomalySettingsService, notificationService, cameraService)
	apis.NewCapacityApi(protected, cameraService, settingsService, machineHealthMonitor, recordingService, objectBackend)
	apis.NewSetupApi(protected, setupStateService)
	apis.NewPairingApi(protected, pairingService)

	// Factory reset ("Secure Wipe & Reset"): shred all media, drop + rebuild + reseed
	// the database (honouring the configured engine), then restart into first-run
	// state. Runtime settings reset naturally because they live in the dropped DB.
	// Gated by bootstrap.allowReset; the restart primitive (deps.Restarter) is shared
	// with future self-update.
	resetMediaPaths := func(ctx context.Context) []string {
		paths := []string{}
		if sd := strings.TrimSpace(deps.Config.Vision.SnapshotDir); sd != "" {
			paths = append(paths, sd)
		}
		if trainingDir != "" {
			paths = append(paths, trainingDir)
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
	monitorSettings := visionMonitorSettingsFromAppConfig(deps.Config)
	monitorSettings.Detector = wrapMonitorDetector(deps.Config, objectBackend)
	monitorSettings.Recorder = recorderManager
	monitorSettings.Notifier = notificationService
	monitorSettings.NotificationDestinations = notificationSettingsService
	monitorSettings.Resolver = detectionClassService
	monitorSettings.SnapshotCipher = atrestCipher
	// Metadata recorder: wire it as the detector's observation sink (so rule cameras
	// record metadata for free off their existing inference) and hand it to the monitor
	// (so metadata-enabled cameras are sampled even without alert rules).
	if oc, ok := monitorSettings.Detector.(vision.ObservationCapable); ok {
		oc.SetObservationSink(metadataRecorder)
	}
	monitorSettings.Metadata = metadataRecorder
	monitorSettings.Metrics = deps.Metrics
	// Resolver for the detection-only frame stream the monitor runs when a camera
	// has AI rules but NVR recording is off (siphon/auto). It reuses the same stream
	// selection + credential injection + siphon params as the real recorder so the
	// AI sees the same stream it would record. Prefers the detection/recording stream,
	// then the configured live-view stream, then the camera's discovered RTSP URI.
	monitorSettings.DetectStreamConfig = func(ctx context.Context, cameraID int64) (recording.RecorderConfig, bool) {
		var streamURL, fallbackURL, liveURL string
		if rcfg, err := recordingService.GetConfig(ctx, cameraID); err == nil && rcfg != nil {
			streamURL = strings.TrimSpace(rcfg.StreamURL)
			fallbackURL = strings.TrimSpace(rcfg.FallbackStreamUrl)
			liveURL = strings.TrimSpace(rcfg.LiveStreamUrl)
		}
		var username, password, discovered string
		if src, err := cameraService.SnapshotSource(ctx, uint64(cameraID)); err == nil {
			username, password, discovered = src.Username, src.Password, src.RTSPURI
		}
		pick := streamURL
		if pick == "" {
			pick = liveURL
		}
		rtspURI := discovered
		if pick != "" {
			rtspURI = services.RTSPURIWithCredentials(pick, username, password)
		}
		if strings.TrimSpace(rtspURI) == "" {
			return recording.RecorderConfig{}, false
		}
		fallbackURI := ""
		if fallbackURL != "" {
			fallbackURI = services.RTSPURIWithCredentials(fallbackURL, username, password)
		}
		siphonFPS, siphonWidth := services.SiphonTeeParams(settingsService)
		hwaccel, hwDevice, initHWDevice, videoDecoder := services.SiphonDecoderParams(settingsService)
		return recording.RecorderConfig{
			CameraId:        cameraID,
			Enabled:         true,
			DetectOnly:      true,
			FFmpegPath:      ffmpegPath,
			RTSPTransport:   rtspTransport,
			RTSPURI:         rtspURI,
			FallbackRTSPURI: fallbackURI,
			SiphonFPS:       siphonFPS,
			SiphonWidth:     siphonWidth,
			HWAccel:         hwaccel,
			HWAccelDevice:   hwDevice,
			InitHWDevice:    initHWDevice,
			VideoDecoder:    videoDecoder,
			Metrics:         deps.Metrics,
		}, true
	}
	if monitorSettings.Enabled {
		// Warm the detector model once at startup, uncapped, so the first live-detection
		// inference doesn't hit the per-frame timeout on a cold GPU/CUDA model load —
		// which would kill and restart the worker in a loop and never warm. Runs in the
		// background so boot isn't blocked by the (potentially 10–15s) model load.
		if objectBackend != nil {
			// One-shot: guarded but never restarted — if warmup panics, live detection
			// simply warms on the first real inference instead.
			safego.Go("mymatasan.vision.warmup", func() {
				warmCtx, cancel := context.WithTimeout(monitorCtx, 120*time.Second)
				defer cancel()
				if err := services.WarmupInference(warmCtx, objectBackend); err != nil {
					log.Printf("vision: detector warmup failed (live detection will warm on first inference): %v", err)
				} else {
					log.Printf("vision: detector model warmed up")
				}
			})
		}
		// The metadata recorder shares the monitor's lifecycle: it aggregates the
		// observations the monitor feeds it and flushes open intervals on shutdown.
		metadataRecorder.Start(monitorCtx)
		services.NewVisionMonitor(cameraService, visionService, settingsService, monitorSettings).Start(monitorCtx)
	}

	// Camera health monitor: probes camera reachability and raises offline/recovery
	// notifications. Independent of the vision monitor; shares the same lifecycle.
	// It reads its settings live from healthSettingsService each sweep, so enabling
	// or retuning it from the Settings UI takes effect without a restart — there is
	// no startup Enabled gate.
	cameraHealthMonitor.Start(monitorCtx)

	// Host health monitor: samples CPU/memory/disk live, raises threshold
	// notifications, and runs disk mitigation (early purge + pause/resume
	// recording). Reads its settings live, so it can be retuned without a restart.
	machineHealthMonitor.Start(monitorCtx)

	// Notification rollup maintainer: incrementally aggregates the notifications
	// feed into the hourly rollup table for dashboard analytics. Shares the monitor
	// lifecycle; the first sweep backfills existing history.
	notificationRollupMaintainer.Start(monitorCtx)

	// Statistical anomaly monitor: scores each closed hour against per-camera
	// baselines (built from the rollup) and raises spike / "unusual silence" alerts.
	// Reads its settings live and is opt-in (disabled until enabled in Settings).
	services.NewAnalyticsMonitor(notificationService, notificationService, anomalySettingsService, cameraService).Start(monitorCtx)

	// Discovery responder: answers authenticated pairing probes while the node is
	// unpaired and a fleet key is set, then goes silent once adopted. The fleet key
	// and discoverability are read live per probe, so setting a key or adopting the
	// node takes effect without a restart. Shares the monitor lifecycle.
	if boolValue(deps.Config.Pairing.Enabled, true) {
		pairingResponder := pairing.NewResponder(pairing.ResponderConfig{
			FleetKey:      func() []byte { k, _ := pairingService.FleetKey(monitorCtx); return k },
			Discoverable:  func() bool { return pairingService.Discoverable(monitorCtx) },
			AnnounceInfo:  func() pairing.AnnounceInfo { return pairingService.AnnounceInfo(monitorCtx, httpsPort) },
			MulticastAddr: deps.Config.Pairing.MulticastAddr,
			ReplayWindow:  time.Duration(deps.Config.Pairing.ReplayWindowSeconds) * time.Second,
			Logf:          func(format string, args ...any) { deps.Logger.Infof("mymatasan.pairing", format, args...) },
		})
		// Supervised: if the discovery responder dies the node stops answering pairing
		// probes and simply becomes un-adoptable, with nothing to say why.
		safego.Supervise(monitorCtx, "mymatasan.pairing.responder", func(ctx context.Context) {
			if err := pairingResponder.Run(ctx); err != nil && ctx.Err() == nil {
				deps.Logger.Warnf("mymatasan.pairing", "discovery responder stopped: %v", err)
			}
		})
		// Enrollment manager (mTLS): enroll after adoption, serve the management
		// listener, and renew certs. Shares the monitor lifecycle.
		go enrollmentManager.Run(monitorCtx)
		// Control channel: dial the parent and maintain the persistent bi-directional
		// channel once paired + enrolled. Shares the monitor lifecycle.
		go controlChannel.Run(monitorCtx)
		// Media channel: dial the parent's media listener and stream camera RTP on
		// request (full-frame-rate live view relayed to myseliasan). Shares lifecycle.
		go mediaChannel.Run(monitorCtx)
	}

	// Factory reset (Secure Wipe & Reset). Built here — after the monitors and
	// recorder exist — so its StopServices hook can quiesce them before wiping: the
	// recorder's ffmpeg holds the live .ts segment open (and keeps writing), which
	// otherwise leaves files behind and stalls the shred on a near-full disk.
	systemResetService = services.NewSystemResetService(services.SystemResetConfig{
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
			Seeders:  m.Seeders(deps.Config.Bootstrap.SeedStatements),
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
	// Self-update: check GitHub Releases (scheduled + on demand) and, on portable/
	// installer installs, download+verify+swap the binary/assets and restart.
	currentVersion := ""
	if manifest, err := versioning.LoadDefault(); err == nil {
		if info, err := manifest.InfoForApp(m.Name()); err == nil {
			currentVersion = info.AppVersion
		}
	}
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
		cameraRepo,
		cameraOnvifRepo,
		recordingConfigRepo,
		detectionClassRepo,
		detectionRuleRepo,
		runtimeSettingsRepo,
		currentVersion,
	)
	apis.NewBackupApi(protected, backupService)

	// The retention purges. Each runs once at startup and then on its own interval, and
	// each is supervised: a panic in a purge used to kill the process, and simply
	// recovering would be no better — nothing else notices a dead purge loop, so the
	// disk quietly fills until writes (including the database's) start failing.
	purgeInterval := func(hours int) time.Duration {
		if d := time.Duration(hours) * time.Hour; d > 0 {
			return d
		}
		return 6 * time.Hour
	}

	// Expired segments + metadata observations. Observation retention aligns with each
	// camera's recording retention.
	periodic(monitorCtx, "mymatasan.purge.segments", 6*time.Hour, func(ctx context.Context) {
		if _, err := recordingService.PurgeOldSegments(ctx); err != nil {
			deps.Logger.Warnf("mymatasan.recording", "segment retention purge failed: %v", err)
		}
		if _, err := observationService.PurgeOldObservations(ctx); err != nil {
			deps.Logger.Warnf("mymatasan.recording", "observation retention purge failed: %v", err)
		}
	})

	// Expired notifications. Retention (days / onlyRead) is read live from the
	// notification settings each run, so UI changes take effect without a restart.
	periodic(monitorCtx, "mymatasan.purge.notifications", purgeInterval(deps.Config.Notification.PurgeIntervalHours), func(ctx context.Context) {
		days, onlyRead := notificationSettingsService.Retention(ctx)
		if days <= 0 {
			return
		}
		if deleted, err := notificationService.PurgeOlderThanDays(ctx, days, onlyRead); err != nil {
			deps.Logger.Warnf("mymatasan.notification", "notification purge failed: %v", err)
		} else if deleted > 0 {
			deps.Logger.Infof("mymatasan.notification", "purged %d expired notifications", deleted)
		}
	})

	// Expired AI detection alerts. DiagnosticRetentionDays trims the noisy vision-monitor
	// diagnostics; AlertRetentionDays (0 = keep forever) trims real detections too. Both
	// also unlink the snapshot image files of the rows they remove.
	periodic(monitorCtx, "mymatasan.purge.alerts", purgeInterval(deps.Config.Vision.AlertPurgeIntervalHours), func(ctx context.Context) {
		if days := deps.Config.Vision.DiagnosticRetentionDays; days > 0 {
			if deleted, err := visionService.PurgeAlertsOlderThanDays(ctx, days, true); err != nil {
				deps.Logger.Warnf("mymatasan.vision", "diagnostic alert purge failed: %v", err)
			} else if deleted > 0 {
				deps.Logger.Infof("mymatasan.vision", "purged %d diagnostic alerts older than %d day(s)", deleted, days)
			}
		}
		if days := deps.Config.Vision.AlertRetentionDays; days > 0 {
			if deleted, err := visionService.PurgeAlertsOlderThanDays(ctx, days, false); err != nil {
				deps.Logger.Warnf("mymatasan.vision", "alert purge failed: %v", err)
			} else if deleted > 0 {
				deps.Logger.Infof("mymatasan.vision", "purged %d alerts older than %d day(s)", deleted, days)
			}
		}
	})

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

// loginGuardConfigFromAppConfig maps the config.json loginSecurity block into the
// failed-login guard config. Numeric tunables left at zero are filled with safe
// defaults inside the guard.
func loginGuardConfigFromAppConfig(cfg *config.AppConfigModel) apis.LoginGuardConfig {
	ls := cfg.LoginSecurity
	return apis.LoginGuardConfig{
		Enabled:     ls.Enabled,
		MaxAttempts: ls.MaxAttempts,
		Window:      time.Duration(ls.WindowSeconds) * time.Second,
		BaseLockout: time.Duration(ls.LockoutSeconds) * time.Second,
		MaxLockout:  time.Duration(ls.LockoutMaxSeconds) * time.Second,
		FailedDelay: time.Duration(ls.FailedDelayMs) * time.Millisecond,
	}
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

// notificationOptionsFromAppConfig builds always-on notification options. The
// outbound delivery channels (webhook, telegram) are applied separately from the
// persisted, runtime-editable notification settings.
func notificationOptionsFromAppConfig(cfg *config.AppConfigModel, logger applog.Logger, metrics telemetry.Metrics) notification.Options {
	return notification.Options{
		Logger:          logger,
		SSEClientBuffer: cfg.Notification.SSEClientBuffer,
		Metrics:         metrics,
	}
}

// notificationSettingsDefaultsFromAppConfig maps the config.json notification
// block into the default runtime-editable notification settings. These seed the
// persisted settings on first run; thereafter the UI-edited copy wins.
func notificationSettingsDefaultsFromAppConfig(cfg *config.AppConfigModel) services.NotificationSettings {
	n := cfg.Notification
	retentionInterval := n.PurgeIntervalHours
	if retentionInterval <= 0 {
		retentionInterval = 6
	}
	return services.NotificationSettings{
		Webhook: services.NotificationWebhookSettings{
			Enabled:     boolValue(n.Webhook.Enabled, false),
			URL:         strings.TrimSpace(n.Webhook.URL),
			MinSeverity: n.Webhook.MinSeverity,
		},
		Telegram: services.NotificationTelegramSettings{
			Enabled:     boolValue(n.Telegram.Enabled, false),
			BotToken:    strings.TrimSpace(n.Telegram.BotToken),
			ChatId:      strings.TrimSpace(n.Telegram.ChatId),
			MinSeverity: n.Telegram.MinSeverity,
		},
		Retention: services.NotificationRetentionSettings{
			Days:          n.RetentionDays,
			OnlyRead:      n.PurgeReadOnly,
			IntervalHours: retentionInterval,
		},
	}
}

func runtimeSettingsFromAppConfig(cfg *config.AppConfigModel) services.RuntimeSettings {
	ffmpegPath := cfg.Decoder.MJPEG.FFmpegPath
	if ffmpegPath == "" {
		ffmpegPath = cfg.Camera.FFmpegPath
	}
	result := services.RuntimeSettings{
		Decoder: services.DecoderSettings{
			MJPEG: services.MJPEGDecoderSettings{
				FFmpegPath: ffmpegPath,
				Quality:    cfg.Decoder.MJPEG.Quality,
				Threads:    cfg.Decoder.MJPEG.Threads,
			},
			FFmpeg: services.FFmpegDecoderSettings{
				RTSPTransport:   cfg.Decoder.FFmpeg.RTSPTransport,
				HWAccel:         cfg.Decoder.FFmpeg.HWAccel,
				HWAccelDevice:   cfg.Decoder.FFmpeg.HWAccelDevice,
				InitHWDevice:    cfg.Decoder.FFmpeg.InitHWDevice,
				VideoDecoder:    cfg.Decoder.FFmpeg.VideoDecoder,
				ProbeSize:       cfg.Decoder.FFmpeg.ProbeSize,
				AnalyzeDuration: cfg.Decoder.FFmpeg.AnalyzeDuration,
				LowDelay:        cfg.Decoder.FFmpeg.LowDelay,
				NoBuffer:        cfg.Decoder.FFmpeg.NoBuffer,
			},
		},
		Stream: services.StreamSettings{
			WebRTC: services.WebRTCSettings{
				Enabled:    boolValue(cfg.Stream.WebRTC.Enabled, false),
				ICEServers: []stream.ICEServer{},
			},
			MJPEGFallback: services.MJPEGFallbackSettings{
				Enabled: boolValue(cfg.Stream.MJPEGFallback.Enabled, true),
			},
		},
		Recording: services.RecordingSettings{
			Storage: services.RecordingStorageSettings{
				Codec:                cfg.Recording.Storage.Codec,
				Quality:              cfg.Recording.Storage.Quality,
				MaxConcurrentEncodes: cfg.Recording.Storage.MaxConcurrentEncodes,
				FallbackToCopy:       cfg.Recording.Storage.FallbackToCopy,
			},
		},
	}
	for _, server := range cfg.Stream.WebRTC.ICEServers {
		if len(server.URLs) == 0 {
			continue
		}
		result.Stream.WebRTC.ICEServers = append(result.Stream.WebRTC.ICEServers, stream.ICEServer{
			URLs:       server.URLs,
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return result
}

// visionMonitorSettingsFromAppConfig builds the monitor settings WITHOUT the
// detector; the caller assigns Detector from the shared object backend via
// wrapMonitorDetector so the same backend serves live detection and auto-label.
func visionMonitorSettingsFromAppConfig(cfg *config.AppConfigModel) services.VisionMonitorSettings {
	snapshotDir := cfg.Vision.SnapshotDir
	if snapshotDir == "" {
		snapshotDir = "recordings"
	}
	return services.VisionMonitorSettings{
		Enabled:                   boolValue(cfg.Vision.Enabled, true),
		Interval:                  int64(cfg.Vision.IntervalMs),
		CaptureTimeout:            int64(cfg.Vision.CaptureTimeoutMs),
		DiagnosticCooldownSeconds: int64(cfg.Vision.DiagnosticCooldownSeconds),
		PersistSampledDiagnostics: cfg.Vision.PersistSampledDiagnostics,
		SnapshotDir:               snapshotDir,
	}
}

// healthSettingsDefaultsFromAppConfig maps the config.json health block into the
// default runtime-editable health settings. These seed the persisted settings on
// first run; thereafter the UI-edited copy wins.
func healthSettingsDefaultsFromAppConfig(cfg *config.AppConfigModel) services.HealthSettings {
	h := cfg.Health
	return services.HealthSettings{
		Enabled:           boolValue(h.Enabled, true),
		IntervalMs:        h.IntervalMs,
		TimeoutMs:         h.TimeoutMs,
		FailureThreshold:  h.FailureThreshold,
		RecoveryThreshold: h.RecoveryThreshold,
	}
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

func resolveShredPasses(cfg *config.AppConfigModel) int {
	s := cfg.Recording.Shred
	if s.Enabled != nil && !*s.Enabled {
		return 0
	}
	if s.Passes > 0 {
		return s.Passes
	}
	return recording.DefaultShredPasses
}

// trainingDataDir resolves the on-disk root for training datasets and models.
// It defaults to a "training" sibling of the snapshot dir so all AI artifacts
// live together under the same volume the machine health monitor watches.
func trainingDataDir(cfg *config.AppConfigModel) string {
	if dir := strings.TrimSpace(cfg.Vision.Training.DataDir); dir != "" {
		return dir
	}
	base := strings.TrimSpace(cfg.Vision.SnapshotDir)
	if base == "" {
		base = "recordings"
	}
	return filepath.Join(base, "training")
}

// trainingRunConfigFromAppConfig derives the in-app trainer config: the Python
// command (shared with the detector) and the train_worker.py / base weights that
// sit next to the configured YOLO worker script.
func trainingRunConfigFromAppConfig(cfg *config.AppConfigModel, configPath string) services.TrainingRunConfig {
	detectorCfg := cfg.Vision.Detector
	workerScript := ""
	for _, arg := range detectorCfg.Args {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(arg)), ".py") {
			workerScript = strings.TrimSpace(arg)
			break
		}
	}
	cfgOut := services.TrainingRunConfig{PythonCmd: detectorCfg.Command, ConfigFile: configPath}
	if workerScript != "" {
		dir := filepath.Dir(workerScript)
		cfgOut.TrainScript = filepath.Join(dir, "train_worker.py")
		cfgOut.BaseModel = filepath.Join(dir, "yolo11n.pt")
	}
	return cfgOut
}

func visionToolSettingsFromAppConfig(cfg *config.AppConfigModel) services.VisionToolSettings {
	detectorCfg := cfg.Vision.Detector
	return services.VisionToolSettings{
		Mode:              detectorCfg.Mode,
		Command:           detectorCfg.Command,
		Args:              detectorCfg.Args,
		TimeoutMs:         detectorCfg.TimeoutMs,
		UseMotionFallback: boolValue(detectorCfg.UseMotionFallback, true),
	}
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

func buildTrainingObjectDetector(detectorCfg config.VisionDetectorConfigModel) (vision.ObjectDetector, error) {
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
func wrapMonitorDetector(cfg *config.AppConfigModel, backend vision.ObjectDetector) vision.Detector {
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

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
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
