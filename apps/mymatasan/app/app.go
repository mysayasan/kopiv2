package app

import (
	"context"
	"fmt"
	"path/filepath"

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
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/versioning"
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
		appentities.OnvifDevice{},
		appentities.DetectionRule{},
		appentities.AlertEvent{},
		appentities.RuntimeSetting{},
		appentities.LocalUser{},
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
	onvifDeviceRepo := dbsql.NewGenericRepo[appentities.OnvifDevice](deps.Db)
	detectionRuleRepo := dbsql.NewGenericRepo[appentities.DetectionRule](deps.Db)
	alertEventRepo := dbsql.NewGenericRepo[appentities.AlertEvent](deps.Db)
	runtimeSettingsRepo := dbsql.NewGenericRepo[appentities.RuntimeSetting](deps.Db)
	localUserRepo := dbsql.NewGenericRepo[appentities.LocalUser](deps.Db)

	onvifService := services.NewOnvifDeviceService(onvifDeviceRepo, onvif.NewClient(), rtsp.NewClient())
	visionService := services.NewVisionService(detectionRuleRepo, alertEventRepo)
	settingsService := services.NewRuntimeSettingsService(runtimeSettingsRepo, runtimeSettingsFromAppConfig(deps.Config))
	localUserService := services.NewLocalUserService(localUserRepo)
	if err := localUserService.EnsureDefaultAdmin(context.Background()); err != nil {
		return nil, fmt.Errorf("seed local admin user failed: %w", err)
	}
	streamManager := stream.NewManager()

	protected := api.PathPrefix("").Subrouter()
	protected.Use(apis.NewLocalBasicAuth(localUserService))
	apis.NewOnvifApi(protected, onvifService, settingsService, streamManager)
	apis.NewVisionApi(protected, visionService)
	apis.NewSettingsApi(protected, settingsService, localUserService)

	monitorCtx, stopMonitor := context.WithCancel(context.Background())
	services.NewVisionMonitor(onvifService, visionService, settingsService).Start(monitorCtx)

	return func(ctx context.Context) error {
		stopMonitor()
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
			"POST /api/settings/runtime/reset": {
				Summary:     "Reset runtime settings",
				Description: "Resets runtime settings to the startup config defaults.",
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
				Description: "Creates or updates a detection rule with target camera, detection type, zone polygon, rule-level schedule policy, threshold, cooldown, and alert options.",
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
				Description: "Uses ONVIF media services to resolve a saved device profile to an RTSP URI.",
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
