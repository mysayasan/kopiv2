package app

import (
	"fmt"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	appentities "github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	appmodels "github.com/mysayasan/kopiv2/apps/mymatasan/models"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	ffmpegCam "github.com/mysayasan/kopiv2/infra/camera/ffmpeg"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
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
	cfg := apphost.DefaultSharedAPIConfig()
	cfg.AppRegistry = false
	cfg.ApiEndpoint = false
	cfg.ApiEndpointRbac = false
	return cfg
}

func (m *module) Entities() []any {
	return []any{
		sharedentities.ApiEndpoint{},
		sharedentities.ApiEndpointRbac{},
		sharedentities.ApiLog{},
		sharedentities.FileStorage{},
		sharedentities.OperationJob{},
		sharedentities.UserGroup{},
		sharedentities.UserLogin{},
		sharedentities.UserRole{},
		sharedentities.UserSession{},
		appentities.CameraStream{},
		appentities.ResidentPropPic{},
		appmodels.ResidentProp{},
	}
}

func (m *module) Seeders(seedStatements []string) []bootstrap.Seeder {
	type endpointSeed struct {
		Title       string
		Description string
		Path        string
		AccessTier  apiaccessenums.AccessTier
		SeedRbac    bool
	}

	endpoints := []endpointSeed{
		{Title: "API Health", Description: "api namespace health", Path: "/api/health", AccessTier: apiaccessenums.Public},
		{Title: "Runtime Version", Description: "runtime version access", Path: "/api/version", AccessTier: apiaccessenums.Public},
		{Title: "Admin", Description: "admin module access", Path: "/api/admin", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{Title: "Home", Description: "home module access", Path: "/api/home", AccessTier: apiaccessenums.AuthOnly, SeedRbac: true},
		{Title: "Camera Stream", Description: "camera stream module access", Path: "/api/camera/stream", AccessTier: apiaccessenums.AuthOnly, SeedRbac: true},
		{Title: "File Storage", Description: "file storage module access", Path: "/api/file-storage", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{Title: "File Storage Download", Description: "public file download access", Path: "/api/file-storage/download", AccessTier: apiaccessenums.Public},
		{Title: "Logs", Description: "api log access", Path: "/api/log", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{Title: "Runtime Logs", Description: "runtime log access", Path: "/api/log-service", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{Title: "Cache Service", Description: "cache administration access", Path: "/api/cache-service", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
	}

	coreDefaults := []string{
		`INSERT INTO user_group (title, description, parent_id, is_active, created_by, created_at, updated_by, updated_at)
SELECT 'system', 'core system group', 0, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM user_group WHERE title = 'system' AND parent_id = 0);`,
		`INSERT INTO user_role (title, description, parent_id, group_id, is_active, created_by, created_at, updated_by, updated_at)
SELECT 'superadmin', 'core administrator role', 0, ug.id, TRUE, 0, 0, 0, 0
FROM user_group ug
WHERE ug.title = 'system' AND ug.parent_id = 0
AND NOT EXISTS (SELECT 1 FROM user_role ur WHERE ur.title = 'superadmin' AND ur.group_id = ug.id);`,
		`INSERT INTO user_login (email, userpwd, first_name, last_name, user_role_id, is_active, created_by, created_at, updated_by, updated_at)
SELECT 'superadmin', '$2a$10$ZNX/d.rXCH5QkWkmMdA0jepur7CEEriX3zTiSnSeYr1txq9GIMAou', 'Super', 'Admin', ur.id, TRUE, 0, 0, 0, 0
FROM user_role ur
JOIN user_group ug ON ug.id = ur.group_id
WHERE ur.title = 'superadmin' AND ug.title = 'system'
AND NOT EXISTS (SELECT 1 FROM user_login ul WHERE ul.email = 'superadmin');`,
	}

	coreRbac := make([]string, 0, len(endpoints)*2)
	for _, endpoint := range endpoints {
		coreRbac = append(coreRbac,
			fmt.Sprintf(`INSERT INTO api_endpoint (title, description, app_code, host, path, access_tier, is_active, created_by, created_at, updated_by, updated_at)
SELECT '%s', '%s', 'mymatasan', '*', '%s', %d, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM api_endpoint WHERE app_code = 'mymatasan' AND host = '*' AND path = '%s');`, endpoint.Title, endpoint.Description, endpoint.Path, endpoint.AccessTier, endpoint.Path),
			fmt.Sprintf(`UPDATE api_endpoint SET app_code = 'mymatasan', access_tier = %d WHERE host = '*' AND path = '%s' AND ((access_tier IS NULL OR access_tier <> %d) OR app_code IS NULL OR app_code = '');`, endpoint.AccessTier, endpoint.Path, endpoint.AccessTier),
		)
		if endpoint.SeedRbac {
			coreRbac = append(coreRbac,
				fmt.Sprintf(`INSERT INTO api_endpoint_rbac (api_endpoint_id, user_role_id, can_get, can_post, can_put, can_delete, is_active, created_by, created_at, updated_by, updated_at)
SELECT ae.id, ur.id, TRUE, TRUE, TRUE, TRUE, TRUE, 0, 0, 0, 0
FROM api_endpoint ae
JOIN user_role ur ON ur.title = 'superadmin'
JOIN user_group ug ON ug.id = ur.group_id AND ug.title = 'system'
WHERE ae.host = '*' AND ae.path = '%s'
AND NOT EXISTS (
	SELECT 1 FROM api_endpoint_rbac aep
	WHERE aep.api_endpoint_id = ae.id AND aep.user_role_id = ur.id
);`, endpoint.Path),
			)
		}
	}

	seeders := []bootstrap.Seeder{
		bootstrap.NewSQLSeeder("core-defaults", coreDefaults),
		bootstrap.NewSQLSeeder("core-rbac", coreRbac),
	}

	if len(seedStatements) > 0 {
		seeders = append(seeders, bootstrap.NewSQLSeeder("config", seedStatements))
	}

	return seeders
}

func (m *module) RegisterAppRoutes(api *mux.Router, deps apphost.Dependencies) (apphost.ShutdownFunc, error) {
	residentPropRepo := dbsql.NewGenericRepo[appmodels.ResidentProp](deps.Db)
	camStreamRepo := dbsql.NewGenericRepo[appentities.CameraStream](deps.Db)

	homeService := services.NewHomeService(residentPropRepo)
	newCam := ffmpegCam.NewNetCam()
	camService := services.NewCameraStreamService(camStreamRepo, deps.Cache, newCam, deps.Logger)

	if err := camService.StartAllMjpegStream(); err != nil {
		return nil, err
	}

	apis.NewAdminApi(api, *deps.Auth, *deps.Rbac)
	apis.NewHomeApi(api, *deps.Auth, *deps.Rbac, homeService)
	apis.NewCameraApi(api, *deps.Auth, *deps.Rbac, camService)

	return camService.Shutdown, nil
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
			"GET /api/log": {
				Summary:     "List API logs",
				Description: "Returns API access logs.",
				Tags:        []string{"log"},
			},
			"DELETE /api/log": {
				Summary:     "Delete API logs by month",
				Description: "Deletes database-backed API activity logs for the requested year and month.",
				Tags:        []string{"log"},
			},
			"GET /api/log-service": {
				Summary:     "List runtime logs",
				Description: "Returns runtime log entries from the configured cross-platform log file.",
				Tags:        []string{"log-service"},
			},
			"DELETE /api/log-service": {
				Summary:     "Delete runtime logs by month",
				Description: "Deletes dated runtime log files for the requested year and month.",
				Tags:        []string{"log-service"},
			},
			"GET /api/cache-service": {
				Summary:     "List cache keys",
				Description: "Returns paginated cache keys with optional prefix filter.",
				Tags:        []string{"cache-service"},
			},
			"GET /api/cache-service/health": {
				Summary:     "Cache health check",
				Description: "Checks cache provider connectivity.",
				Tags:        []string{"cache-service"},
			},
			"DELETE /api/cache-service": {
				Summary:     "Wipe cache",
				Description: "Deletes cache entries by key, prefix, or all with explicit wipeAll=true.",
				Tags:        []string{"cache-service"},
			},
			"POST /api/cache-service/wipe": {
				Summary:     "Wipe cache by payload",
				Description: "Deletes cache entries using JSON payload with key, prefix, or wipeAll=true.",
				Tags:        []string{"cache-service"},
			},
			"POST /api/file-storage/upload": {
				Summary:     "Upload file",
				Description: "Uploads files with metadata, security level, and optional absolute or countdown expiry.",
				Tags:        []string{"file-storage"},
			},
			"POST /api/file-storage/upload-async": {
				Summary:     "Queue file upload",
				Description: "Stages upload files with security level and optional absolute or countdown expiry, then creates a durable backend job for asynchronous storage.",
				Tags:        []string{"file-storage"},
			},
			"GET /api/file-storage/job": {
				Summary:     "Get file upload job",
				Description: "Returns durable upload job status by id query parameter.",
				Tags:        []string{"file-storage"},
			},
			"GET /api/file-storage/download": {
				Summary:     "Download file",
				Description: "Downloads one stored file by metadata id, renders one file inline with view=true, or returns a ZIP archive by comma-separated metadata ids.",
				Tags:        []string{"file-storage"},
			},
			"GET /api/home/latest": {
				Summary:     "Get latest residents",
				Description: "Returns latest resident data with pagination.",
				Tags:        []string{"home"},
			},
			"POST /api/home/new": {
				Summary:     "Create home record",
				Description: "Creates a new home entry.",
				Tags:        []string{"home"},
			},
			"GET /api/admin/test": {
				Summary:     "Admin test endpoint",
				Description: "Simple authenticated admin test response.",
				Tags:        []string{"admin"},
			},
			"GET /api/camera/stream": {
				Summary:     "List camera streams",
				Description: "Returns camera streams with pagination.",
				Tags:        []string{"camera"},
			},
			"POST /api/camera/stream": {
				Summary:     "Create camera stream",
				Description: "Creates a camera stream configuration.",
				Tags:        []string{"camera"},
			},
			"PUT /api/camera/stream": {
				Summary:     "Update camera stream",
				Description: "Updates camera stream configuration.",
				Tags:        []string{"camera"},
			},
			"DELETE /api/camera/stream/{id}": {
				Summary:     "Delete camera stream",
				Description: "Deletes camera stream configuration by ID.",
				Tags:        []string{"camera"},
			},
			"GET /api/camera/stream/mjpeg/{id}": {
				Summary:     "MJPEG stream",
				Description: "Streams multipart MJPEG output for selected camera ID.",
				Tags:        []string{"camera"},
			},
		},
	}
}
