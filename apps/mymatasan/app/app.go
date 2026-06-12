package app

import (
	"context"
	"fmt"
	"io"
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
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/versioning"
	"github.com/mysayasan/kopiv2/infra/vision"
)

type module struct{}

func New() apphost.App {
	return &module{}
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
		appentities.RuntimeSetting{},
		appentities.LocalUser{},
		appentities.RecordingSegment{},
		appentities.RecordingConfig{},
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
	runtimeSettingsRepo := dbsql.NewGenericRepo[appentities.RuntimeSetting](deps.Db)
	localUserRepo := dbsql.NewGenericRepo[appentities.LocalUser](deps.Db)
	recordingSegmentRepo := dbsql.NewGenericRepo[appentities.RecordingSegment](deps.Db)
	recordingConfigRepo := dbsql.NewGenericRepo[appentities.RecordingConfig](deps.Db)

	cameraService := services.NewCameraService(cameraRepo, cameraOnvifRepo, onvif.NewClient(), rtsp.NewClient())
	visionService := services.NewVisionService(detectionRuleRepo, alertEventRepo)
	settingsService := services.NewRuntimeSettingsService(runtimeSettingsRepo, runtimeSettingsFromAppConfig(deps.Config))
	localUserService := services.NewLocalUserService(localUserRepo)
	recordingService := services.NewRecordingService(recordingSegmentRepo, recordingConfigRepo)
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

	protected := api.PathPrefix("").Subrouter()
	protected.Use(apis.NewLocalBasicAuth(localUserService))
	apis.NewOnvifApi(protected, cameraService, settingsService, streamManager)
	apis.NewVisionApi(protected, visionService, recorderManager)
	apis.NewSettingsApi(protected, settingsService, cameraService, localUserService, visionToolSettingsFromAppConfig(deps.Config))
	apis.NewRecordingApi(protected, recordingService, recorderManager, cameraService, settingsService)

	monitorCtx, stopMonitor := context.WithCancel(context.Background())
	monitorSettings, err := visionMonitorSettingsFromAppConfig(deps.Config)
	if err != nil {
		stopMonitor()
		recorderManager.Close()
		return nil, err
	}
	monitorSettings.Recorder = recorderManager
	if monitorSettings.Enabled {
		services.NewVisionMonitor(cameraService, visionService, settingsService, monitorSettings).Start(monitorCtx)
	}

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

	return func(ctx context.Context) error {
		stopMonitor()
		recorderManager.Close()
		if closer, ok := monitorSettings.Detector.(io.Closer); ok {
			_ = closer.Close()
		}
		return streamManager.Close()
	}, nil
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

func visionMonitorSettingsFromAppConfig(cfg *config.AppConfigModel) (services.VisionMonitorSettings, error) {
	detector, err := visionDetectorFromAppConfig(cfg)
	if err != nil {
		return services.VisionMonitorSettings{}, err
	}
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
		Detector:                  detector,
	}, nil
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

func visionDetectorFromAppConfig(cfg *config.AppConfigModel) (vision.Detector, error) {
	detectorCfg := cfg.Vision.Detector
	mode := strings.ToLower(strings.TrimSpace(detectorCfg.Mode))
	if mode == "" {
		mode = vision.DetectorModeMotion
	}
	motionDetector := vision.NewMotionDetector()
	useMotionFallback := boolValue(detectorCfg.UseMotionFallback, true)
	useMotionIntrusion := boolValue(detectorCfg.UseMotionIntrusion, true)

	switch mode {
	case vision.DetectorModeMotion:
		return motionDetector, nil
	case vision.DetectorModeExternal, vision.DetectorModeHybrid:
		external, err := vision.NewExternalObjectDetector(vision.ExternalObjectDetectorOptions{
			Command: detectorCfg.Command,
			Args:    detectorCfg.Args,
			Timeout: time.Duration(detectorCfg.TimeoutMs) * time.Millisecond,
		})
		if err != nil {
			if useMotionFallback {
				return motionDetector, nil
			}
			return nil, err
		}
		objectDetector := vision.NewObjectRuleDetector(external, vision.ObjectRuleDetectorOptions{
			ClassMap:            detectorCfg.ClassMap,
			MinObjectConfidence: detectorCfg.MinObjectConfidence,
			Source:              "external-object-detector",
		})
		if mode == vision.DetectorModeExternal {
			return objectDetector, nil
		}
		motionTypes := []string{}
		if useMotionIntrusion {
			motionTypes = append(motionTypes, vision.DetectionIntrusion)
		}
		return vision.NewDispatchDetector(vision.DispatchDetectorOptions{
			Object:      objectDetector,
			Motion:      motionDetector,
			MotionTypes: motionTypes,
		}), nil
	case vision.DetectorModePersistent, "externalpersistent", "external-persistent", "external_persistent":
		persistent, err := vision.NewPersistentObjectDetector(vision.PersistentObjectDetectorOptions{
			Command: detectorCfg.Command,
			Args:    detectorCfg.Args,
			Timeout: time.Duration(detectorCfg.TimeoutMs) * time.Millisecond,
		})
		if err != nil {
			if useMotionFallback {
				return motionDetector, nil
			}
			return nil, err
		}
		objectDetector := vision.NewObjectRuleDetector(persistent, vision.ObjectRuleDetectorOptions{
			ClassMap:            detectorCfg.ClassMap,
			MinObjectConfidence: detectorCfg.MinObjectConfidence,
			Source:              "persistent-yolo-detector",
		})
		motionTypes := []string{}
		if useMotionIntrusion {
			motionTypes = append(motionTypes, vision.DetectionIntrusion)
		}
		if len(motionTypes) == 0 {
			return objectDetector, nil
		}
		return vision.NewDispatchDetector(vision.DispatchDetectorOptions{
			Object:      objectDetector,
			Motion:      motionDetector,
			MotionTypes: motionTypes,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported vision detector mode %q", detectorCfg.Mode)
	}
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
