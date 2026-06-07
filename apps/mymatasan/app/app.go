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

func (m *module) Entities() []any {
	return []any{
		sharedentities.ApiEndpoint{},
		sharedentities.ApiEndpointRbac{},
		sharedentities.ApiLog{},
		sharedentities.FileStorage{},
		sharedentities.UserGroup{},
		sharedentities.UserLogin{},
		sharedentities.UserRole{},
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
	}

	endpoints := []endpointSeed{
		{Title: "Admin", Description: "admin module access", Path: "/api/admin"},
		{Title: "Home", Description: "home module access", Path: "/api/home"},
		{Title: "Camera Stream", Description: "camera stream module access", Path: "/api/camera/stream"},
		{Title: "File Storage", Description: "file storage module access", Path: "/api/file-storage"},
		{Title: "Logs", Description: "api log access", Path: "/api/log"},
		{Title: "Runtime Logs", Description: "runtime log access", Path: "/api/log-service"},
		{Title: "Endpoints", Description: "endpoint catalog access", Path: "/api/endpoint"},
		{Title: "Endpoint RBAC", Description: "endpoint access control access", Path: "/api/endpoint-rbac"},
		{Title: "Cache Service", Description: "cache administration access", Path: "/api/cache-service"},
		{Title: "User Group", Description: "user group module access", Path: "/api/user-group"},
		{Title: "User Credential", Description: "user login and role access", Path: "/api/user-credential"},
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
			fmt.Sprintf(`INSERT INTO api_endpoint (title, description, host, path, is_active, created_by, created_at, updated_by, updated_at)
SELECT '%s', '%s', '*', '%s', TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM api_endpoint WHERE host = '*' AND path = '%s');`, endpoint.Title, endpoint.Description, endpoint.Path, endpoint.Path),
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
			"GET /api/login/google": {
				Summary:     "Google login start",
				Description: "Starts OAuth2 login flow for Google provider.",
				Tags:        []string{"login"},
			},
			"POST /api/login/default": {
				Summary:     "Default login",
				Description: "Performs local username/password login and sets the session cookies.",
				Tags:        []string{"login"},
			},
			"POST /api/login/default/register": {
				Summary:     "Default register",
				Description: "Creates a local username/password account and sets the session cookies.",
				Tags:        []string{"login"},
			},
			"POST /api/login/default/logout": {
				Summary:     "Default logout",
				Description: "Clears the session cookies.",
				Tags:        []string{"login"},
			},
			"GET /api/login/github": {
				Summary:     "GitHub login start",
				Description: "Starts OAuth2 login flow for GitHub provider.",
				Tags:        []string{"login"},
			},
			"GET /api/callback/google": {
				Summary:     "Google login callback",
				Description: "OAuth2 callback endpoint for Google provider.",
				Tags:        []string{"login"},
			},
			"GET /api/callback/github": {
				Summary:     "GitHub login callback",
				Description: "OAuth2 callback endpoint for GitHub provider.",
				Tags:        []string{"login"},
			},
			"GET /api/user-credential": {
				Summary:     "List user credentials and roles",
				Description: "Returns paginated user credentials and role records.",
				Tags:        []string{"user-credential"},
			},
			"GET /api/user-credential/email": {
				Summary:     "Get user by email",
				Description: "Returns one user credential by email query parameter.",
				Tags:        []string{"user-credential"},
			},
			"GET /api/user-credential/group/{id}": {
				Summary:     "List roles by group",
				Description: "Returns user roles for selected group ID.",
				Tags:        []string{"user-credential"},
			},
			"GET /api/user-group": {
				Summary:     "List user groups",
				Description: "Returns paginated user groups.",
				Tags:        []string{"user-group"},
			},
			"POST /api/user-group": {
				Summary:     "Create user group",
				Description: "Creates a new user group.",
				Tags:        []string{"user-group"},
			},
			"PUT /api/user-group": {
				Summary:     "Update user group",
				Description: "Updates an existing user group.",
				Tags:        []string{"user-group"},
			},
			"DELETE /api/user-group/{id}": {
				Summary:     "Delete user group",
				Description: "Deletes a user group by ID.",
				Tags:        []string{"user-group"},
			},
			"POST /api/user-credential": {
				Summary:     "Create user role",
				Description: "Creates a user role under a group.",
				Tags:        []string{"user-credential"},
			},
			"PUT /api/user-credential": {
				Summary:     "Update user credential or role",
				Description: "Updates user credential or user role payload by model type.",
				Tags:        []string{"user-credential"},
			},
			"DELETE /api/user-credential/{id}": {
				Summary:     "Delete user credential",
				Description: "Deletes a user credential by ID.",
				Tags:        []string{"user-credential"},
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
			"GET /api/endpoint": {
				Summary:     "List endpoints",
				Description: "Returns RBAC endpoint catalog.",
				Tags:        []string{"endpoint"},
			},
			"POST /api/endpoint": {
				Summary:     "Create endpoint",
				Description: "Creates a new RBAC endpoint entry.",
				Tags:        []string{"endpoint"},
			},
			"PUT /api/endpoint": {
				Summary:     "Update endpoint",
				Description: "Updates an RBAC endpoint entry.",
				Tags:        []string{"endpoint"},
			},
			"DELETE /api/endpoint/{id}": {
				Summary:     "Delete endpoint",
				Description: "Deletes an RBAC endpoint entry by ID.",
				Tags:        []string{"endpoint"},
			},
			"GET /api/endpoint-rbac": {
				Summary:     "List endpoint RBAC",
				Description: "Returns endpoint RBAC rules.",
				Tags:        []string{"endpoint-rbac"},
			},
			"GET /api/endpoint-rbac/validate/me": {
				Summary:     "Validate current access",
				Description: "Validates current user access for endpoint/method query.",
				Tags:        []string{"endpoint-rbac"},
			},
			"GET /api/endpoint-rbac/ep/me": {
				Summary:     "List current user endpoints",
				Description: "Returns endpoints available for current user role.",
				Tags:        []string{"endpoint-rbac"},
			},
			"POST /api/endpoint-rbac": {
				Summary:     "Create endpoint RBAC",
				Description: "Creates endpoint access rule.",
				Tags:        []string{"endpoint-rbac"},
			},
			"PUT /api/endpoint-rbac": {
				Summary:     "Update endpoint RBAC",
				Description: "Updates endpoint access rule.",
				Tags:        []string{"endpoint-rbac"},
			},
			"DELETE /api/endpoint-rbac/{id}": {
				Summary:     "Delete endpoint RBAC",
				Description: "Deletes endpoint access rule by ID.",
				Tags:        []string{"endpoint-rbac"},
			},
			"POST /api/file-storage/upload": {
				Summary:     "Upload file",
				Description: "Uploads and stores file metadata.",
				Tags:        []string{"file-storage"},
			},
			"GET /api/file-storage/download": {
				Summary:     "Download file",
				Description: "Downloads a stored file by guid query parameter.",
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
