package app

import (
	"fmt"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/apis"
	outputdtos "github.com/mysayasan/kopiv2/apps/myidsan/dtos/output"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/versioning"
)

type module struct{}

func New() apphost.App {
	return &module{}
}

func (m *module) Name() string {
	return "myidsan"
}

func (m *module) BaseDir() string {
	return filepath.Join("apps", "myidsan")
}

func (m *module) Entities() []any {
	return []any{
		sharedentities.AppRegistry{},
		sharedentities.ApiEndpoint{},
		sharedentities.ApiEndpointRbac{},
		sharedentities.ApiLog{},
		sharedentities.FileStorage{},
		sharedentities.OperationJob{},
		sharedentities.UserGroup{},
		sharedentities.UserLogin{},
		sharedentities.UserRole{},
		sharedentities.UserSession{},
	}
}

func (m *module) Seeders(seedStatements []string) []bootstrap.Seeder {
	type endpointSeed struct {
		AppCode     string
		Title       string
		Description string
		Path        string
		AccessTier  apiaccessenums.AccessTier
		SeedRbac    bool
	}

	endpoints := []endpointSeed{
		{AppCode: "myidsan", Title: "API Health", Description: "api namespace health", Path: "/api/health", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "Runtime Version", Description: "runtime version access", Path: "/api/version", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "Login", Description: "local and OAuth login access", Path: "/api/login", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "OAuth Callback", Description: "OAuth callback access", Path: "/api/callback", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "File Storage Download", Description: "public file download access", Path: "/api/file-storage/download", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "File Storage", Description: "identity file storage access", Path: "/api/file-storage", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Logs", Description: "api log access", Path: "/api/log", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Runtime Logs", Description: "runtime log access", Path: "/api/log-service", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Endpoints", Description: "endpoint catalog access", Path: "/api/endpoint", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Endpoint RBAC", Description: "endpoint access control access", Path: "/api/endpoint-rbac", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Cache Service", Description: "cache administration access", Path: "/api/cache-service", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "App Registry", Description: "registered SSO app management", Path: "/api/app-registry", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "SSO Introspection", Description: "internal token introspection access", Path: "/api/sso/introspect", AccessTier: apiaccessenums.DevOnly},
		{AppCode: "myidsan", Title: "SSO Authorization", Description: "internal authorization decision access", Path: "/api/sso/authorize", AccessTier: apiaccessenums.DevOnly},
		{AppCode: "myidsan", Title: "User Group", Description: "user group module access", Path: "/api/user-group", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "User Credential", Description: "user login and role access", Path: "/api/user-credential", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "mymatasan", Title: "mymatasan Admin", Description: "mymatasan admin module access", Path: "/api/admin", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "mymatasan", Title: "mymatasan Home", Description: "mymatasan home module access", Path: "/api/home", AccessTier: apiaccessenums.AuthOnly, SeedRbac: true},
		{AppCode: "mymatasan", Title: "mymatasan Camera Stream", Description: "mymatasan camera stream module access", Path: "/api/camera/stream", AccessTier: apiaccessenums.AuthOnly, SeedRbac: true},
		{AppCode: "mymatasan", Title: "mymatasan User Login", Description: "mymatasan app user login access", Path: "/api/user-login", AccessTier: apiaccessenums.AuthOnly, SeedRbac: true},
	}

	coreDefaults := []string{
		`INSERT INTO app_registry (code, title, description, base_url, audience, client_secret, is_active, created_by, created_at, updated_by, updated_at)
SELECT 'myidsan', 'myidsan', 'Identity and SSO authority', 'http://localhost:3001', 'myidsan', '', TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM app_registry WHERE code = 'myidsan');`,
		`INSERT INTO app_registry (code, title, description, base_url, audience, client_secret, is_active, created_by, created_at, updated_by, updated_at)
SELECT 'mymatasan', 'mymatasan', 'Camera and VLMS application', 'http://localhost:3000', 'mymatasan', '', TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM app_registry WHERE code = 'mymatasan');`,
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
SELECT '%s', '%s', '%s', '*', '%s', %d, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM api_endpoint WHERE app_code = '%s' AND host = '*' AND path = '%s');`, endpoint.Title, endpoint.Description, endpoint.AppCode, endpoint.Path, endpoint.AccessTier, endpoint.AppCode, endpoint.Path),
			fmt.Sprintf(`UPDATE api_endpoint SET app_code = '%s', access_tier = %d WHERE host = '*' AND path = '%s' AND ((access_tier IS NULL OR access_tier <> %d) OR app_code IS NULL OR app_code = '');`, endpoint.AppCode, endpoint.AccessTier, endpoint.Path, endpoint.AccessTier),
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
		bootstrap.NewSQLSeeder("myidsan-rbac", coreRbac),
	}

	if len(seedStatements) > 0 {
		seeders = append(seeders, bootstrap.NewSQLSeeder("config", seedStatements))
	}

	return seeders
}

func (m *module) RegisterAppRoutes(api *mux.Router, deps apphost.Dependencies) (apphost.ShutdownFunc, error) {
	userLoginRepo := dbsql.NewGenericRepo[sharedentities.UserLogin](deps.Db)
	userGroupRepo := dbsql.NewGenericRepo[sharedentities.UserGroup](deps.Db)
	userRoleRepo := dbsql.NewGenericRepo[sharedentities.UserRole](deps.Db)

	userLoginService := services.NewUserLoginService(userLoginRepo, deps.Cache)
	userGroupService := services.NewUserGroupService(userGroupRepo, deps.Cache)
	userRoleService := services.NewUserRoleService(userRoleRepo, deps.Cache)
	userLoginDtoService := services.NewUserLoginDtoService[outputdtos.UserLoginDto](userLoginService)
	userGroupDtoService := services.NewUserGroupDtoService[outputdtos.UserGroupDto](userGroupService)
	userRoleDtoService := services.NewUserRoleDtoService[outputdtos.UserRoleDto](userRoleService)

	apis.NewLoginApi(api, deps.Config.Login, *deps.Auth, userLoginService)
	apis.NewUserLoginApi(api, *deps.Auth, *deps.Rbac, userLoginDtoService)
	apis.NewUserGroupApi(api, *deps.Auth, *deps.Rbac, userGroupDtoService)
	apis.NewUserRoleApi(api, *deps.Auth, *deps.Rbac, userRoleDtoService)
	apis.NewSSOApi(api, deps.Config, deps.Auth, deps.Rbac)
	return nil, nil
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
			Title:       "myidsan API",
			Version:     docVersion,
			Description: "Identity, user management, and RBAC administration app for kopiv2.",
		},
		Endpoints: map[string]apidocs.EndpointDoc{
			"GET /health": {
				Summary:     "Service liveness",
				Description: "Returns service alive status.",
				Tags:        []string{"system"},
			},
			"GET /ready": {
				Summary:     "Service readiness",
				Description: "Checks database and cache connectivity.",
				Tags:        []string{"system"},
			},
			"GET /api/version": {
				Summary:     "Runtime version",
				Description: "Returns the running app version and shared core version.",
				Tags:        []string{"system"},
			},
			"POST /api/login/default": {
				Summary:     "Default login",
				Description: "Performs local username/password login and sets session cookies.",
				Tags:        []string{"login"},
			},
			"POST /api/login/default/register": {
				Summary:     "Default register",
				Description: "Creates a local username/password account and sets session cookies.",
				Tags:        []string{"login"},
			},
			"POST /api/login/default/logout": {
				Summary:     "Default logout",
				Description: "Clears session cookies.",
				Tags:        []string{"login"},
			},
			"GET /api/user-group": {
				Summary:     "List user groups",
				Description: "Returns paginated user groups for identity administration.",
				Tags:        []string{"identity"},
			},
			"GET /api/user-credential": {
				Summary:     "List user credentials and roles",
				Description: "Returns paginated user credentials and role records.",
				Tags:        []string{"identity"},
			},
			"GET /api/endpoint": {
				Summary:     "List endpoints",
				Description: "Returns the endpoint catalog used by RBAC policy administration.",
				Tags:        []string{"rbac"},
			},
			"GET /api/endpoint-rbac": {
				Summary:     "List endpoint RBAC",
				Description: "Returns endpoint RBAC rules.",
				Tags:        []string{"rbac"},
			},
			"GET /api/endpoint-rbac/ep/me": {
				Summary:     "List current user endpoints",
				Description: "Returns endpoints available for the current user role.",
				Tags:        []string{"rbac"},
			},
			"GET /api/endpoint-rbac/validate/me": {
				Summary:     "Validate current access",
				Description: "Validates current user access for endpoint and method query values.",
				Tags:        []string{"rbac"},
			},
			"GET /api/cache-service": {
				Summary:     "List cache keys",
				Description: "Returns paginated cache keys with optional prefix filter.",
				Tags:        []string{"cache-service"},
			},
			"GET /api/app-registry": {
				Summary:     "List registered apps",
				Description: "Returns apps registered for SSO audience and relying-app management.",
				Tags:        []string{"app-registry"},
			},
			"POST /api/sso/introspect": {
				Summary:     "Introspect SSO token",
				Description: "Internal API for relying apps to validate token/session state when they cannot share Redis cache.",
				Tags:        []string{"sso"},
			},
			"POST /api/sso/authorize": {
				Summary:     "Authorize SSO token",
				Description: "Internal API for relying apps to ask myidsan for an RBAC decision when local memory cache is isolated.",
				Tags:        []string{"sso"},
			},
		},
	}
}
