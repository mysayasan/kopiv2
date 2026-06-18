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

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	appentities "github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	applog "github.com/mysayasan/kopiv2/infra/logging"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/rtsp"
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
		appentities.RuntimeSetting{},
		appentities.LocalUser{},
		appentities.RecordingSegment{},
		appentities.RecordingConfig{},
		sharedentities.Notification{},
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
		{Title: "Notifications", Description: "unified notification feed and live stream access", Path: "/api/notifications", AccessTier: apiaccessenums.AuthOnly},
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
	}

	if len(seedStatements) > 0 {
		seeders = append(seeders, bootstrap.NewSQLSeeder("config", seedStatements))
	}

	return seeders
}

func (m *module) RegisterAppRoutes(api *mux.Router, deps apphost.Dependencies) (apphost.ShutdownFunc, error) {
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
	// Build the object-detection backend once and share it between the live
	// monitor and the training auto-labeler. The YOLO worker reads the active-model
	// pointer file (set via env) so a trained/imported model can be hot-swapped.
	trainingDir := trainingDataDir(deps.Config)
	activeModelFile, _ := filepath.Abs(filepath.Join(trainingDir, "active_model.txt"))
	_ = os.Setenv("MYMATASAN_ACTIVE_MODEL_FILE", activeModelFile)
	stockModelFile, _ := filepath.Abs(filepath.Join(trainingDir, "stock_model.txt"))
	_ = os.Setenv("MYMATASAN_STOCK_MODEL_FILE", stockModelFile)
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
		objectBackend,
		deps.Config.Vision.Detector.MinObjectConfidence,
		trainingRunConfigFromAppConfig(deps.Config, deps.ConfigPath),
	)
	settingsService := services.NewRuntimeSettingsService(runtimeSettingsRepo, runtimeSettingsFromAppConfig(deps.Config))
	localUserService := services.NewLocalUserService(localUserRepo)
	recordingService := services.NewRecordingService(recordingSegmentRepo, recordingConfigRepo)
	notificationRepo := dbsql.NewGenericRepo[sharedentities.Notification](deps.Db)
	notificationService := notification.NewService(notificationRepo, notificationOptionsFromAppConfig(deps.Config, deps.Logger))
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
	// Load persisted notification delivery settings and apply them to the hub.
	if err := notificationSettingsService.Sync(context.Background()); err != nil {
		deps.Logger.Warnf("mymatasan.notification", "load notification settings failed: %v", err)
	}
	if err := localUserService.EnsureDefaultAdmin(context.Background()); err != nil {
		return nil, fmt.Errorf("seed local admin user failed: %w", err)
	}
	streamManager := stream.NewManager()

	// Resolve ffmpeg path and RTSP transport from persisted settings.
	ffmpegPath := ""
	rtspTransport := ""
	if dec, err := settingsService.Decoder(context.Background()); err == nil {
		ffmpegPath = dec.MJPEG.FFmpegPath
		rtspTransport = dec.FFmpeg.RTSPTransport
	}

	// Build the recording manager from persisted per-camera configs.
	// Each camera is configured in its own goroutine so RTSP URI lookups and
	// ffmpeg process launches happen in parallel across all cameras.
	recorderManager := recording.NewManager(recordingService)
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
					CameraId:        cfg.CameraId,
					Enabled:         cfg.Enabled,
					PreRollSec:      cfg.PreRollSec,
					PostRollSec:     cfg.PostRollSec,
					StoragePath:     cfg.StoragePath,
					FFmpegPath:      ffmpegPath,
					RTSPTransport:   rtspTransport,
					RTSPURI:         rtspURI,
					FallbackRTSPURI: fallbackURI,
					SegmentMinutes:  cfg.SegmentMinutes,
					RetentionDays:   cfg.RetentionDays,
				})
			}(cfg)
		}
		wg.Wait()
	}

	// Built before route registration so the camera API can expose an on-demand
	// health probe; started later alongside the other background monitors.
	cameraHealthMonitor := services.NewCameraHealthMonitor(cameraService, rtsp.NewClient(), healthSettingsService, notificationService)
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
	machineHealthMonitor := services.NewMachineHealthMonitor(machineHealthSettingsService, notificationService, recorderManager, recordingService, machineAutoPaths)
	m.machineHealth = machineHealthMonitor

	protected := api.PathPrefix("").Subrouter()
	protected.Use(apis.NewLocalBasicAuth(localUserService))
	apis.NewOnvifApi(protected, cameraService, settingsService, streamManager)
	apis.NewCameraApi(protected, cameraService, settingsService, streamManager, cameraHealthMonitor)
	apis.NewVisionApi(protected, visionService, detectionClassService, recorderManager, notificationService, cameraService, settingsService)
	apis.NewTrainingApi(protected, trainingService)
	apis.NewSettingsApi(protected, settingsService, cameraService, localUserService, notificationSettingsService, healthSettingsService, machineHealthSettingsService, machineHealthMonitor, visionToolSettingsFromAppConfig(deps.Config))
	apis.NewRecordingApi(protected, recordingService, recorderManager, cameraService, settingsService)
	apis.NewNotificationApi(protected, notificationService)

	monitorCtx, stopMonitor := context.WithCancel(context.Background())
	monitorSettings := visionMonitorSettingsFromAppConfig(deps.Config)
	monitorSettings.Detector = wrapMonitorDetector(deps.Config, objectBackend)
	monitorSettings.Recorder = recorderManager
	monitorSettings.Notifier = notificationService
	monitorSettings.Resolver = detectionClassService
	if monitorSettings.Enabled {
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

	// Purge expired segments once at startup, then every 6 hours.
	go func() {
		recordingService.PurgeOldSegments(monitorCtx)
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				recordingService.PurgeOldSegments(monitorCtx)
			case <-monitorCtx.Done():
				return
			}
		}
	}()

	// Purge expired notifications once at startup, then on a configured interval.
	// Retention (days / onlyRead) is read live from the notification settings each
	// run, so changes made in the UI take effect without a restart.
	{
		interval := time.Duration(deps.Config.Notification.PurgeIntervalHours) * time.Hour
		if interval <= 0 {
			interval = 6 * time.Hour
		}
		go func() {
			purge := func() {
				days, onlyRead := notificationSettingsService.Retention(monitorCtx)
				if days <= 0 {
					return
				}
				if deleted, err := notificationService.PurgeOlderThanDays(monitorCtx, days, onlyRead); err != nil {
					deps.Logger.Warnf("mymatasan.notification", "notification purge failed: %v", err)
				} else if deleted > 0 {
					deps.Logger.Infof("mymatasan.notification", "purged %d expired notifications", deleted)
				}
			}
			purge()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					purge()
				case <-monitorCtx.Done():
					return
				}
			}
		}()
	}

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

// notificationOptionsFromAppConfig builds always-on notification options. The
// outbound delivery channels (webhook, telegram) are applied separately from the
// persisted, runtime-editable notification settings.
func notificationOptionsFromAppConfig(cfg *config.AppConfigModel, logger applog.Logger) notification.Options {
	return notification.Options{
		Logger:          logger,
		SSEClientBuffer: cfg.Notification.SSEClientBuffer,
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
