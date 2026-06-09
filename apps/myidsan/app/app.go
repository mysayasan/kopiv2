package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

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
		sharedentities.AppAuthConfig{},
		sharedentities.AppRedirectUri{},
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
		Metadata    string
		AccessTier  apiaccessenums.AccessTier
		SeedRbac    bool
	}

	type menuItem struct {
		Enabled bool   `json:"enabled"`
		Id      string `json:"id"`
		Label   string `json:"label"`
		Group   string `json:"group"`
		Order   int    `json:"order"`
		Summary string `json:"summary"`
		Tone    string `json:"tone"`
	}

	menuMetadata := func(items ...menuItem) string {
		if len(items) == 0 {
			return ""
		}
		if len(items) == 1 {
			payload := struct {
				Menu menuItem `json:"menu"`
			}{Menu: items[0]}
			data, _ := json.Marshal(payload)
			return string(data)
		}
		payload := struct {
			Menus []menuItem `json:"menus"`
		}{Menus: items}
		data, _ := json.Marshal(payload)
		return string(data)
	}
	sqlString := func(value string) string {
		return strings.ReplaceAll(value, "'", "''")
	}

	endpoints := []endpointSeed{
		{AppCode: "myidsan", Title: "API Health", Description: "api namespace health", Path: "/api/health", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "Runtime Version", Description: "runtime version access", Path: "/api/version", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "Login", Description: "local and OAuth login access", Path: "/api/login", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "Federated Auth", Description: "cross-app authorization code login access", Path: "/api/auth", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "OAuth Callback", Description: "OAuth callback access", Path: "/api/callback", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "File Storage Download", Description: "public file download access", Path: "/api/file-storage/download", AccessTier: apiaccessenums.Public},
		{AppCode: "myidsan", Title: "File Storage", Description: "identity file storage access", Path: "/api/file-storage", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Logs", Description: "api log access", Path: "/api/log", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Runtime Logs", Description: "runtime log access", Path: "/api/log-service", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Endpoints", Description: "endpoint catalog access", Path: "/api/endpoint", Metadata: menuMetadata(menuItem{Enabled: true, Id: "endpoints", Label: "Endpoints", Group: "Access Control", Order: 50, Summary: "Maintain the protected endpoint catalog and menu metadata.", Tone: "steel"}), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Endpoint RBAC", Description: "endpoint access control access", Path: "/api/endpoint-rbac", Metadata: menuMetadata(menuItem{Enabled: true, Id: "rbac", Label: "RBAC", Group: "Access Control", Order: 60, Summary: "Map endpoint permissions to role-specific HTTP methods.", Tone: "green"}), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Cache Service", Description: "cache administration access", Path: "/api/cache-service", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "App Registry", Description: "registered SSO app management", Path: "/api/app-registry", Metadata: menuMetadata(menuItem{Enabled: true, Id: "apps", Label: "Apps", Group: "Federation", Order: 40, Summary: "Manage relying apps, audiences, and SSO registration.", Tone: "indigo"}), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "App Auth Config", Description: "registered SSO client auth policy management", Path: "/api/app-auth-config", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "App Redirect URI", Description: "registered SSO client callback URL management", Path: "/api/app-redirect-uri", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "SSO Introspection", Description: "internal token introspection access", Path: "/api/sso/introspect", AccessTier: apiaccessenums.DevOnly},
		{AppCode: "myidsan", Title: "SSO Authorization", Description: "internal authorization decision access", Path: "/api/sso/authorize", AccessTier: apiaccessenums.DevOnly},
		{AppCode: "myidsan", Title: "User Group", Description: "user group module access", Path: "/api/user-group", Metadata: menuMetadata(menuItem{Enabled: true, Id: "groups", Label: "Groups", Group: "Identity", Order: 20, Summary: "Organize identity ownership and hierarchy roots.", Tone: "teal"}), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "User Credential", Description: "user login and role access", Path: "/api/user-credential", Metadata: menuMetadata(
			menuItem{Enabled: true, Id: "users", Label: "Users", Group: "Identity", Order: 10, Summary: "Maintain credentials, profile details, and role assignment.", Tone: "blue"},
			menuItem{Enabled: true, Id: "roles", Label: "Roles", Group: "Identity", Order: 30, Summary: "Create group-scoped roles and parent role chains.", Tone: "violet"},
		), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myseliasan", Title: "myseliasan Session", Description: "myseliasan session metadata access", Path: "/api/session", AccessTier: apiaccessenums.AuthOnly, SeedRbac: true},
		{AppCode: "myseliasan", Title: "myseliasan Auth", Description: "myseliasan relying-app auth callback access", Path: "/api/auth", AccessTier: apiaccessenums.Public},
		{AppCode: "myseliasan", Title: "myseliasan Version", Description: "myseliasan runtime version access", Path: "/api/version", AccessTier: apiaccessenums.Public},
	}

	coreDefaults := []string{
		`INSERT INTO app_registry (code, title, description, base_url, audience, client_secret, is_active, created_by, created_at, updated_by, updated_at)
SELECT 'myidsan', 'myidsan', 'Identity and SSO authority', 'http://localhost:3001', 'myidsan', '', TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM app_registry WHERE code = 'myidsan');`,
		`UPDATE app_registry
SET title = 'myidsan', description = 'Identity and SSO authority', base_url = 'http://localhost:3001', audience = 'myidsan', is_active = TRUE, updated_at = 0
WHERE code = 'myidsan';`,
		`INSERT INTO app_registry (code, title, description, base_url, audience, client_secret, is_active, created_by, created_at, updated_by, updated_at)
SELECT 'mymatasan', 'mymatasan', 'Standalone ONVIF monitoring device app', 'http://localhost:3000', 'mymatasan', '', TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM app_registry WHERE code = 'mymatasan');`,
		`UPDATE app_registry
SET title = 'mymatasan', description = 'Standalone ONVIF monitoring device app', base_url = 'http://localhost:3000', audience = 'mymatasan', is_active = TRUE, updated_at = 0
WHERE code = 'mymatasan';`,
		`INSERT INTO app_registry (code, title, description, base_url, audience, client_secret, is_active, created_by, created_at, updated_by, updated_at)
SELECT 'myseliasan', 'myseliasan', 'Control plane for mymatasan', 'http://localhost:3002', 'myseliasan', '', TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM app_registry WHERE code = 'myseliasan');`,
		`UPDATE app_registry
SET title = 'myseliasan', description = 'Control plane for mymatasan', base_url = 'http://localhost:3002', audience = 'myseliasan', is_active = TRUE, updated_at = 0
WHERE code = 'myseliasan';`,
		`INSERT INTO app_auth_config (app_registry_id, client_id, client_secret_hash, auth_code_ttl_seconds, access_token_ttl_seconds, session_ttl_seconds, refresh_token_ttl_seconds, require_pkce, allow_refresh_token, is_active, created_by, created_at, updated_by, updated_at)
SELECT ar.id, 'myseliasan', '736c6859eceedb2db6b79b2f96d8e53a714ac644d83ee1dd3b52f89ae55cc274', 300, 900, 259200, 0, FALSE, FALSE, TRUE, 0, 0, 0, 0
FROM app_registry ar
WHERE ar.code = 'myseliasan'
AND NOT EXISTS (SELECT 1 FROM app_auth_config ac WHERE ac.client_id = 'myseliasan');`,
		`UPDATE app_auth_config
SET app_registry_id = (SELECT id FROM app_registry WHERE code = 'myseliasan'), client_secret_hash = '736c6859eceedb2db6b79b2f96d8e53a714ac644d83ee1dd3b52f89ae55cc274', auth_code_ttl_seconds = 300, access_token_ttl_seconds = 900, session_ttl_seconds = 259200, refresh_token_ttl_seconds = 0, require_pkce = FALSE, allow_refresh_token = FALSE, is_active = TRUE, updated_at = 0
WHERE client_id = 'myseliasan';`,
		`INSERT INTO app_redirect_uri (app_auth_config_id, redirect_uri, is_active, created_by, created_at, updated_by, updated_at)
SELECT ac.id, 'http://localhost:3002/api/auth/callback', TRUE, 0, 0, 0, 0
FROM app_auth_config ac
WHERE ac.client_id = 'myseliasan'
AND NOT EXISTS (SELECT 1 FROM app_redirect_uri aru WHERE aru.app_auth_config_id = ac.id AND aru.redirect_uri = 'http://localhost:3002/api/auth/callback');`,
		`UPDATE app_redirect_uri
SET is_active = TRUE, updated_at = 0
WHERE app_auth_config_id = (SELECT id FROM app_auth_config WHERE client_id = 'myseliasan')
AND redirect_uri = 'http://localhost:3002/api/auth/callback';`,
		`INSERT INTO app_redirect_uri (app_auth_config_id, redirect_uri, is_active, created_by, created_at, updated_by, updated_at)
SELECT ac.id, 'https://localhost:3002/api/auth/callback', TRUE, 0, 0, 0, 0
FROM app_auth_config ac
WHERE ac.client_id = 'myseliasan'
AND NOT EXISTS (SELECT 1 FROM app_redirect_uri aru WHERE aru.app_auth_config_id = ac.id AND aru.redirect_uri = 'https://localhost:3002/api/auth/callback');`,
		`UPDATE app_redirect_uri
SET is_active = TRUE, updated_at = 0
WHERE app_auth_config_id = (SELECT id FROM app_auth_config WHERE client_id = 'myseliasan')
AND redirect_uri = 'https://localhost:3002/api/auth/callback';`,
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
			fmt.Sprintf(`INSERT INTO api_endpoint (title, description, metadata, app_code, host, path, access_tier, is_active, created_by, created_at, updated_by, updated_at)
SELECT '%s', '%s', '%s', '%s', '*', '%s', %d, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM api_endpoint WHERE app_code = '%s' AND host = '*' AND path = '%s');`, sqlString(endpoint.Title), sqlString(endpoint.Description), sqlString(endpoint.Metadata), sqlString(endpoint.AppCode), sqlString(endpoint.Path), endpoint.AccessTier, sqlString(endpoint.AppCode), sqlString(endpoint.Path)),
			fmt.Sprintf(`UPDATE api_endpoint SET app_code = '%s', access_tier = %d, metadata = '%s' WHERE host = '*' AND path = '%s' AND ((access_tier IS NULL OR access_tier <> %d) OR app_code IS NULL OR app_code = '' OR metadata IS NULL OR metadata <> '%s');`, sqlString(endpoint.AppCode), endpoint.AccessTier, sqlString(endpoint.Metadata), sqlString(endpoint.Path), endpoint.AccessTier, sqlString(endpoint.Metadata)),
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
	appRegistryRepo := dbsql.NewGenericRepo[sharedentities.AppRegistry](deps.Db)
	appAuthConfigRepo := dbsql.NewGenericRepo[sharedentities.AppAuthConfig](deps.Db)
	appRedirectUriRepo := dbsql.NewGenericRepo[sharedentities.AppRedirectUri](deps.Db)

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
	apis.NewFederatedAuthApi(api, deps.Config, deps.Auth, userLoginService, appRegistryRepo, appAuthConfigRepo, appRedirectUriRepo, deps.Cache)
	apis.NewAppAuthConfigApi(api, *deps.Auth, *deps.Rbac, appAuthConfigRepo)
	apis.NewAppRedirectUriApi(api, *deps.Auth, *deps.Rbac, appRedirectUriRepo)
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
			"GET /api/auth/authorize": {
				Summary:     "Authorize relying app",
				Description: "Starts browser authorization-code login for a registered relying app.",
				Tags:        []string{"federated-auth"},
			},
			"GET /api/auth/login": {
				Summary:     "Federated login page",
				Description: "Serves MyIDSan login page used during relying-app authorization.",
				Tags:        []string{"federated-auth"},
			},
			"POST /api/auth/login": {
				Summary:     "Federated login submit",
				Description: "Authenticates local credentials and resumes relying-app authorization.",
				Tags:        []string{"federated-auth"},
			},
			"POST /api/auth/token": {
				Summary:     "Exchange authorization code",
				Description: "Exchanges a one-time authorization code for relying-app token claims.",
				Tags:        []string{"federated-auth"},
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
			"GET /api/app-auth-config": {
				Summary:     "List app auth configs",
				Description: "Returns relying-app SSO auth policy with secret hashes redacted.",
				Tags:        []string{"app-registry"},
			},
			"POST /api/app-auth-config": {
				Summary:     "Create app auth config",
				Description: "Creates relying-app SSO auth policy and hashes the supplied client secret.",
				Tags:        []string{"app-registry"},
			},
			"PUT /api/app-auth-config": {
				Summary:     "Update app auth config",
				Description: "Updates relying-app SSO auth policy.",
				Tags:        []string{"app-registry"},
			},
			"DELETE /api/app-auth-config/{id}": {
				Summary:     "Delete app auth config",
				Description: "Deletes relying-app SSO auth policy by ID.",
				Tags:        []string{"app-registry"},
			},
			"GET /api/app-redirect-uri": {
				Summary:     "List app redirect URIs",
				Description: "Returns relying-app callback URL allow-list rows.",
				Tags:        []string{"app-registry"},
			},
			"POST /api/app-redirect-uri": {
				Summary:     "Create app redirect URI",
				Description: "Creates a relying-app callback URL allow-list row.",
				Tags:        []string{"app-registry"},
			},
			"PUT /api/app-redirect-uri": {
				Summary:     "Update app redirect URI",
				Description: "Updates a relying-app callback URL allow-list row.",
				Tags:        []string{"app-registry"},
			},
			"DELETE /api/app-redirect-uri/{id}": {
				Summary:     "Delete app redirect URI",
				Description: "Deletes a relying-app callback URL allow-list row by ID.",
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
