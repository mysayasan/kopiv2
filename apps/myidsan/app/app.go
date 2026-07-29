package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/apis"
	outputdtos "github.com/mysayasan/kopiv2/apps/myidsan/dtos/output"
	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	logininfra "github.com/mysayasan/kopiv2/infra/login"
	"github.com/mysayasan/kopiv2/infra/mailer"
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
		sharedentities.ApiLog{},
		sharedentities.FileStorage{},
		sharedentities.OperationJob{},
		sharedentities.UserGroup{},
		sharedentities.UserLogin{},
		sharedentities.UserSession{},
		sharedentities.AccessRole{},
		sharedentities.AccessRolePermission{},
		sharedentities.RuntimeSetting{},
		myidsanentities.SsoCa{},
		myidsanentities.DirectoryConfig{},
		myidsanentities.FederatedGroupMapping{},
		myidsanentities.UserMfaFactor{},
		myidsanentities.UserMfaRecoveryCode{},
		myidsanentities.PasswordResetRequest{},
		myidsanentities.UserAvatar{},
		myidsanentities.AuditLog{},
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
		{AppCode: "myidsan", Title: "RBAC", Description: "shared accessrbac role + permission management", Path: "/api/access-rbac", Metadata: menuMetadata(menuItem{Enabled: true, Id: "rbac", Label: "RBAC", Group: "Access Control", Order: 60, Summary: "Manage roles and the per-endpoint permission matrix.", Tone: "green"}), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Cache Service", Description: "cache administration access", Path: "/api/cache-service", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "App Registry", Description: "registered SSO app management", Path: "/api/app-registry", Metadata: menuMetadata(menuItem{Enabled: true, Id: "apps", Label: "Apps", Group: "Federation", Order: 40, Summary: "Manage relying apps, audiences, and SSO registration.", Tone: "indigo"}), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "App Auth Config", Description: "registered SSO client auth policy management", Path: "/api/app-auth-config", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Directory", Description: "LDAP/Active Directory login configuration", Path: "/api/directory-config", Metadata: menuMetadata(menuItem{Enabled: true, Id: "directory", Label: "Directory", Group: "Federation", Order: 45, Summary: "Connect an LDAP/Active Directory server and map groups to roles.", Tone: "amber"}), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Federated Group Mapping", Description: "directory/IdP group to role mapping management", Path: "/api/federated-group-mapping", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "App Redirect URI", Description: "registered SSO client callback URL management", Path: "/api/app-redirect-uri", AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "SSO Introspection", Description: "internal token introspection access", Path: "/api/sso/introspect", AccessTier: apiaccessenums.DevOnly},
		{AppCode: "myidsan", Title: "Setup Wizard", Description: "first-run setup state and completion", Path: "/api/setup", AccessTier: apiaccessenums.DevOnly},
		{AppCode: "myidsan", Title: "User Group", Description: "user group module access", Path: "/api/user-group", Metadata: menuMetadata(menuItem{Enabled: true, Id: "groups", Label: "Groups", Group: "Identity", Order: 20, Summary: "Organize identity ownership and hierarchy roots.", Tone: "teal"}), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		{AppCode: "myidsan", Title: "Password Reset Requests", Description: "account-recovery request queue", Path: "/api/password-reset", Metadata: menuMetadata(menuItem{Enabled: true, Id: "resetRequests", Label: "Reset requests", Group: "Identity", Order: 15, Summary: "Review and resolve forgotten-password requests from local accounts.", Tone: "amber"}), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		// Superadmin-only in the API regardless of matrix grants (see apis/backup.go):
		// an export is the whole identity store in one file and a restore rewrites every
		// account. SeedRbac is deliberately omitted so it is never delegated by default.
		{AppCode: "myidsan", Title: "Step-up Re-authentication", Description: "short-lived credential re-check for sensitive actions", Path: "/api/step-up", AccessTier: apiaccessenums.AuthOnly},
		{AppCode: "myidsan", Title: "Sessions", Description: "self-service session listing and revocation", Path: "/api/session", AccessTier: apiaccessenums.AuthOnly},
		{AppCode: "myidsan", Title: "Session Administration", Description: "cross-account session listing and revocation", Path: "/api/session-admin", AccessTier: apiaccessenums.DevOnly},
		{AppCode: "myidsan", Title: "Audit Log", Description: "append-only security event trail", Path: "/api/audit", Metadata: menuMetadata(menuItem{Enabled: true, Id: "audit", Label: "Audit log", Group: "System", Order: 5, Summary: "Who signed in, what changed, and from where — exportable for compliance review.", Tone: "steel"}), AccessTier: apiaccessenums.DevOnly},
		{AppCode: "myidsan", Title: "Backup & Restore", Description: "encrypted identity-store export and disaster recovery", Path: "/api/backup", Metadata: menuMetadata(menuItem{Enabled: true, Id: "backup", Label: "Backup & restore", Group: "System", Order: 10, Summary: "Export an encrypted copy of users, roles and SSO clients — or rebuild this server from one.", Tone: "amber"}), AccessTier: apiaccessenums.DevOnly},
		{AppCode: "myidsan", Title: "User Credential", Description: "user login and role access", Path: "/api/user-credential", Metadata: menuMetadata(
			menuItem{Enabled: true, Id: "users", Label: "Users", Group: "Identity", Order: 10, Summary: "Maintain credentials, profile details, and role assignment.", Tone: "blue"},
			menuItem{Enabled: true, Id: "roles", Label: "Roles", Group: "Identity", Order: 30, Summary: "Create group-scoped roles and parent role chains.", Tone: "violet"},
		), AccessTier: apiaccessenums.DevOnly, SeedRbac: true},
		// The endpoint catalog is app-local now: authorization is the per-app accessrbac
		// matrix and the rate limiter matches by host+path only (never app_code), so
		// myidsan no longer seeds other apps' endpoints into its own database. Legacy
		// cross-app rows are purged in coreDefaults below.
	}

	coreDefaults := []string{
		// Relying apps are NOT auto-registered. Following the standard OAuth /
		// Google-console model, an app (myidsan itself, mymatasan, myseliasan, …) can
		// only obtain an authorization code once an operator has explicitly registered
		// it under "myidsan → Apps": an app_registry row, an app_auth_config (client_id
		// + client secret), and at least one app_redirect_uri. Until then the federated
		// auth flow rejects it with "client is not registered" (see federated_auth.go
		// loadClient / validateRedirectURI). This is deliberate — auto-seeding these
		// rows would let an unregistered app keep working after a database drop, which
		// is exactly the security hole we are closing.
		`INSERT INTO user_group (title, description, parent_id, is_active, created_by, created_at, updated_by, updated_at)
SELECT 'system', 'core system group', 0, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM user_group WHERE title = 'system' AND parent_id = 0);`,
		// The stock superadmin is no longer seeded here — it is created/refreshed from
		// config.localAuth at startup by EnsureStockSuperadmin (RegisterAppRoutes), with
		// a forced first-login password change.
		// Endpoints are app-local: drop any legacy rows seeded for other apps so this
		// database's catalog only describes myidsan's own endpoints.
		`DELETE FROM api_endpoint WHERE app_code <> 'myidsan';`,
	}

	coreRbac := make([]string, 0, len(endpoints)*2)
	for _, endpoint := range endpoints {
		coreRbac = append(coreRbac,
			fmt.Sprintf(`INSERT INTO api_endpoint (title, description, metadata, app_code, host, path, access_tier, is_active, created_by, created_at, updated_by, updated_at)
SELECT '%s', '%s', '%s', '%s', '*', '%s', %d, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM api_endpoint WHERE app_code = '%s' AND host = '*' AND path = '%s');`, sqlString(endpoint.Title), sqlString(endpoint.Description), sqlString(endpoint.Metadata), sqlString(endpoint.AppCode), sqlString(endpoint.Path), endpoint.AccessTier, sqlString(endpoint.AppCode), sqlString(endpoint.Path)),
			fmt.Sprintf(`UPDATE api_endpoint SET app_code = '%s', access_tier = %d, metadata = '%s' WHERE host = '*' AND path = '%s' AND ((access_tier IS NULL OR access_tier <> %d) OR app_code IS NULL OR app_code = '' OR metadata IS NULL OR metadata <> '%s');`, sqlString(endpoint.AppCode), endpoint.AccessTier, sqlString(endpoint.Metadata), sqlString(endpoint.Path), endpoint.AccessTier, sqlString(endpoint.Metadata)),
		)
		// Per-endpoint permissions are no longer seeded here: authorization is the shared
		// accessrbac matrix (managed via /api/access-rbac), and the bootstrap superadmin
		// bypasses the matrix entirely. The SeedRbac flag now only marks an endpoint as
		// part of the protected admin surface for catalog/menu purposes.
	}

	seeders := []bootstrap.Seeder{
		bootstrap.NewSQLSeeder("core-defaults", coreDefaults),
		bootstrap.NewSQLSeeder("myidsan-endpoints", coreRbac),
	}

	if len(seedStatements) > 0 {
		seeders = append(seeders, bootstrap.NewSQLSeeder("config", seedStatements))
	}

	return seeders
}

func (m *module) RegisterAppRoutes(api *mux.Router, deps apphost.Dependencies) (apphost.ShutdownFunc, error) {
	userLoginRepo := dbsql.NewGenericRepo[sharedentities.UserLogin](deps.Db)
	userGroupRepo := dbsql.NewGenericRepo[sharedentities.UserGroup](deps.Db)
	appRegistryRepo := dbsql.NewGenericRepo[sharedentities.AppRegistry](deps.Db)
	appAuthConfigRepo := dbsql.NewGenericRepo[sharedentities.AppAuthConfig](deps.Db)
	appRedirectUriRepo := dbsql.NewGenericRepo[sharedentities.AppRedirectUri](deps.Db)

	userLoginService := services.NewUserLoginService(userLoginRepo, deps.Cache)
	userGroupService := services.NewUserGroupService(userGroupRepo, deps.Cache)
	userLoginDtoService := services.NewUserLoginDtoService[outputdtos.UserLoginDto](userLoginService)
	userGroupDtoService := services.NewUserGroupDtoService[outputdtos.UserGroupDto](userGroupService)

	// myidsan now uses the shared accessrbac core for its own admin authorization.
	// Bind the user_login store as the resolver, and map the bootstrap 'superadmin'
	// account onto the accessrbac superadmin role (apphost seeded the roles).
	ctx := context.Background()
	// Seed (or refresh) the stock superadmin from config.localAuth, pinned to the
	// accessrbac superadmin role and forced to change its password on first login.
	// A RESET_ADMIN marker in the data dir (dropped by the installer's "reset the
	// admin login" option, or by hand) is the lock-out recovery path — consume it
	// BEFORE seeding, and it deletes itself so a restart never re-runs the reset.
	// A fresh seed (or a reset) is announced: console banner + recovery file.
	if sa, err := deps.AccessRoles.GetByName(ctx, sharedservices.RoleSuperadmin); err == nil && sa != nil {
		seed, err := consumeAdminResetMarker(deps, userLoginService, sa.Id)
		if err != nil {
			return nil, fmt.Errorf("reset stock superadmin: %w", err)
		}
		if seed == nil {
			established, serr := userLoginService.EnsureStockSuperadmin(ctx, deps.Config.LocalAuth.Username, deps.Config.LocalAuth.Password, sa.Id)
			if serr != nil {
				return nil, fmt.Errorf("seed stock superadmin: %w", serr)
			}
			seed = &established
		}
		if seed.Seeded {
			announceFirstRunAdmin(deps, *seed)
		}
	}
	deps.Access.SetResolver(&userLoginResolver{repo: userLoginRepo})

	// First-run setup wizard completion flag (shared runtime-setting row).
	runtimeSettingRepo := dbsql.NewGenericRepo[sharedentities.RuntimeSetting](deps.Db)
	setupStateService := services.NewSetupStateService(runtimeSettingRepo)
	apis.NewSetupApi(api, *deps.Auth, deps.Access, setupStateService)

	// Directory (LDAP/AD) login: the bind password is encrypted at rest, so the
	// secret cipher must resolve before the service exists. Recovery-pending fails
	// closed like myseliasan's fleet secrets.
	secretCipher, err := openSecretCipher(deps)
	if err != nil {
		return nil, err
	}
	directoryConfigRepo := dbsql.NewGenericRepo[myidsanentities.DirectoryConfig](deps.Db)
	groupMappingRepo := dbsql.NewGenericRepo[myidsanentities.FederatedGroupMapping](deps.Db)
	directoryService := services.NewDirectoryService(directoryConfigRepo, groupMappingRepo, userLoginService, secretCipher)

	// TOTP second factor for myidsan-verified credentials. Constructed before the
	// login API so the two password paths can gate on it; the shared secret is sealed
	// with the same at-rest cipher as the directory bind password.
	mfaFactorRepo := dbsql.NewGenericRepo[myidsanentities.UserMfaFactor](deps.Db)
	mfaRecoveryRepo := dbsql.NewGenericRepo[myidsanentities.UserMfaRecoveryCode](deps.Db)
	mfaService := services.NewMfaService(mfaFactorRepo, mfaRecoveryRepo, secretCipher, "myidsan")
	deps.Metrics.Describe(apis.MetricMfaChallengeTotal, "Second-factor verification outcomes (result=issued|success|failed|expired): a spike in failures flags online guessing against a known password.")
	// Escape hatch for a sole-superadmin lost-device lockout: a RESET_MFA marker in
	// the data dir clears the stock superadmin's second factor on boot.
	if err := consumeMfaResetMarker(deps, mfaService, userLoginService); err != nil {
		return nil, err
	}

	// Account recovery: an always-on operator queue plus an OPTIONAL internal-SMTP
	// self-service link. The mailer stays disabled unless config.smtp.enabled — an
	// air-gapped install never reaches for a network; the queue covers it regardless.
	resetRequestRepo := dbsql.NewGenericRepo[myidsanentities.PasswordResetRequest](deps.Db)
	resetMailer := mailer.New(mailer.Config{
		Enabled:     deps.Config.Smtp.Enabled,
		Host:        deps.Config.Smtp.Host,
		Port:        deps.Config.Smtp.Port,
		From:        deps.Config.Smtp.From,
		Username:    deps.Config.Smtp.Username,
		Password:    deps.Config.Smtp.Password,
		UseStartTls: deps.Config.Smtp.UseStartTls,
	})
	if resetMailer.Enabled() {
		log.Printf("myidsan self-service password reset enabled (smtp %s:%d)", deps.Config.Smtp.Host, deps.Config.Smtp.Port)
	}
	passwordResetService := services.NewPasswordResetService(resetRequestRepo, userLoginService, resetMailer, deps.Cache)

	// One LoginGuard covers every interactive credential surface (local JSON login,
	// LDAP login, the server-rendered federated login page): to a password sprayer
	// they are the same door, so they share the per-IP counters.
	loginGuard := sharedapis.NewLoginGuard(loginGuardConfig(deps))
	deps.Metrics.Describe(apis.MetricFederatedLoginTotal, "Federated login outcomes by provider and result (LDAP/Kerberos failures are otherwise invisible in aggregate).")

	// Kerberos SPNEGO: a bad keytab degrades to "not offered" with a warning
	// (mirroring half-configured OAuth), never a failed boot.
	var kerberosAuth *logininfra.KerberosAuthenticator
	kerberosLabel := ""
	if deps.Config.Kerberos.Enabled {
		a, err := logininfra.NewKerberosAuthenticator(logininfra.KerberosSettings{
			KeytabPath:       deps.Config.Kerberos.KeytabPath,
			ServicePrincipal: deps.Config.Kerberos.ServicePrincipal,
			OnlyRealms:       deps.Config.Kerberos.OnlyRealms,
		})
		if err != nil {
			log.Printf("WARNING: kerberos login disabled — %v", err)
		} else {
			kerberosAuth = a
			kerberosLabel = strings.TrimSpace(deps.Config.Kerberos.DisplayLabel)
			if kerberosLabel == "" {
				kerberosLabel = "Windows (SSO)"
			}
			log.Printf("kerberos SSO enabled spn=%s keytab=%s", deps.Config.Kerberos.ServicePrincipal, deps.Config.Kerberos.KeytabPath)
		}
	}

	// Step-up re-authentication. A 72-hour superadmin session can assign roles, clear
	// anyone's second factor and export the whole identity store; the most dangerous of
	// those now require proving the credential again within a short window.
	stepUpService := services.NewStepUpService(userLoginService, mfaService, deps.Cache)

	// Append-only security trail. Constructed before the login API because authentication
	// events are the bulk of what it records, and it is passed by value into every handler
	// that performs a sensitive action rather than being reached through a global.
	auditService := services.NewAuditService(
		dbsql.NewGenericRepo[myidsanentities.AuditLog](deps.Db),
		log.Printf,
	)

	// Session index. The cache entry remains the authority on whether a session is valid;
	// this table exists so sessions can be LISTED per user, which the cache cannot answer.
	sessionService := services.NewSessionService(
		dbsql.NewGenericRepo[sharedentities.UserSession](deps.Db),
		deps.Cache,
		log.Printf,
	)

	providerRegistry := apis.NewLoginApi(api, deps.Config.Login, *deps.Auth, userLoginService, apis.LoginApiOptions{
		Directory:     directoryService,
		Kerberos:      kerberosAuth,
		KerberosLabel: kerberosLabel,
		Guard:         loginGuard,
		Metrics:       deps.Metrics,
		Mfa:           mfaService,
		Store:         deps.Cache,
		Reset:         passwordResetService,
		Audit:         auditService,
		Sessions:      sessionService,
		// Only a configured reverse proxy may declare the client address recorded
		// against an event; otherwise the peer address is used. Sharing the rate
		// limiter's list keeps one answer to "who is the caller".
		TrustedProxies: deps.Config.RateLimit.TrustedProxies,
	})
	apis.NewUserLoginApi(api, *deps.Auth, deps.Access, userLoginDtoService, sessionService, auditService, deps.Config.RateLimit.TrustedProxies)
	apis.NewUserGroupApi(api, *deps.Auth, deps.Access, userGroupDtoService)
	// Role + permission management is the shared accessrbac surface (/api/access-rbac),
	// mounted by apphost. The old per-app user_role admin endpoint is retired.
	apis.NewSSOApi(api, deps.Config, deps.Auth)
	apis.NewFederatedAuthApi(api, deps.Config, deps.Auth, providerRegistry, directoryService, kerberosLabel, loginGuard, userLoginService, appRegistryRepo, appAuthConfigRepo, appRedirectUriRepo, deps.Cache, mfaService, passwordResetService)
	apis.NewDirectoryConfigApi(api, *deps.Auth, deps.Access, directoryService, groupMappingRepo, auditService, deps.Config.RateLimit.TrustedProxies)
	apis.NewAppAuthConfigApi(api, *deps.Auth, deps.Access, appAuthConfigRepo)
	apis.NewAppRedirectUriApi(api, *deps.Auth, deps.Access, appRedirectUriRepo)
	// SSO certificate authority: issues relying-app client certificates for mTLS.
	ssoCaRepo := dbsql.NewGenericRepo[myidsanentities.SsoCa](deps.Db)
	apis.NewSsoCertApi(api, *deps.Auth, deps.Access, services.NewSsoCaService(ssoCaRepo), appAuthConfigRepo)

	// Backup & restore. myidsan holds every user, role, SSO client, the SSO CA private
	// key, all TOTP secrets and the LDAP bind password, so losing this database locks the
	// whole suite out at once — and the previous recovery story was "copy the files
	// yourself". The service takes secretCipher because the sealed columns are unsealed
	// on export and re-sealed with the destination host's key on restore; carrying the
	// sealed bytes would restore cleanly and then fail every second-factor check.
	// deps.Cache is passed so a restore can drop every live session.
	avatarRepo := dbsql.NewGenericRepo[myidsanentities.UserAvatar](deps.Db)
	backupService := services.NewBackupService(
		dbsql.NewGenericRepo[sharedentities.AccessRole](deps.Db),
		dbsql.NewGenericRepo[sharedentities.AccessRolePermission](deps.Db),
		userLoginRepo,
		userGroupRepo,
		avatarRepo,
		mfaFactorRepo,
		mfaRecoveryRepo,
		appRegistryRepo,
		appAuthConfigRepo,
		appRedirectUriRepo,
		directoryConfigRepo,
		groupMappingRepo,
		ssoCaRepo,
		secretCipher,
		deps.Cache,
		setupStateService,
		moduleAppVersion(m),
	)
	apis.NewBackupApi(api, *deps.Auth, deps.Access, backupService, auditService, stepUpService, deps.Config.RateLimit.TrustedProxies)
	// Read-only append-only security trail. Superadmin-only: it names who did what
	// from where, and on an identity server it also reveals which usernames exist.
	apis.NewStepUpApi(api, *deps.Auth, stepUpService, auditService, deps.Config.RateLimit.TrustedProxies)
	apis.NewAuditApi(api, *deps.Auth, deps.Access, auditService)
	// Session listing and revocation. Self-service routes are auth-only (ending your own
	// session is not privileged); cross-account routes are superadmin.
	apis.NewSessionApi(api, *deps.Auth, deps.Access, sessionService, auditService, deps.Config.RateLimit.TrustedProxies)
	// Self-service + admin MFA management surface. The login-time challenge is wired
	// into the login/federated-auth APIs above via the same mfaService.
	apis.NewMfaApi(api, *deps.Auth, deps.Access, mfaService, userLoginService, deps.Metrics, auditService, stepUpService, deps.Config.RateLimit.TrustedProxies)
	// Superadmin account-recovery queue (the public request endpoint is on the login
	// API; the self-service email flow is on the federated-auth API).
	apis.NewPasswordResetApi(api, *deps.Auth, deps.Access, passwordResetService, auditService, stepUpService, deps.Config.RateLimit.TrustedProxies)
	// Profile avatars: self-service (own, auth-only) + admin-by-id (superadmin) for
	// the Users management page.
	apis.NewProfileApi(api, *deps.Auth, deps.Access, avatarRepo)
	// Superadmin-handoff status drives the "disable the stock superadmin" banner. The
	// stock account's email is the configured localAuth.username.
	apis.NewIdentityStatusApi(api, *deps.Auth, deps.Access, userLoginRepo, deps.AccessRoles, deps.Config.LocalAuth.Username)
	return nil, nil
}

// openSecretCipher resolves the at-rest master key for myidsan's stored secrets
// (today: the directory bind password), mirroring myseliasan's fleet-secret boot.
// Returns nil (no encryption) when the feature is disabled. A key that existed here
// before but is now missing FAILS CLOSED — refusing to boot rather than minting a
// fresh key and silently orphaning the encrypted secrets.
func openSecretCipher(deps apphost.Dependencies) (*atrest.Cipher, error) {
	enabled := true
	if deps.Config.Security.EncryptAtRest != nil {
		enabled = *deps.Config.Security.EncryptAtRest
	}
	if !enabled {
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
	protectorCfg := atrest.ProtectorConfig{
		Name:           deps.Config.Security.KeyProtector,
		Passphrase:     deps.Config.Security.Passphrase,
		PassphraseFile: deps.Config.Security.PassphraseFile,
		PassphraseEnv:  deps.Config.Security.PassphraseEnv,
	}
	outcome, err := atrest.OpenForStartup(keyPath, recoveryPath, protectorCfg)
	if err != nil {
		return nil, fmt.Errorf("secret encryption key: %w", err)
	}
	if outcome.Mode == atrest.ModeRecoveryPending {
		return nil, fmt.Errorf("secret encryption key missing (id %s): restore %s or set security.recoveryPath, then restart", outcome.KeyId, keyPath)
	}
	log.Printf("myidsan secret encryption enabled (key %s, mode %s, id %s)", keyPath, outcome.Mode, outcome.KeyId)
	return outcome.KeyStore.Cipher(), nil
}

// loginGuardConfig maps the shared LoginSecurity config block onto the login guard.
func loginGuardConfig(deps apphost.Dependencies) sharedapis.LoginGuardConfig {
	ls := deps.Config.LoginSecurity.Effective()
	return sharedapis.LoginGuardConfig{
		Enabled:     ls.Enabled,
		MaxAttempts: ls.MaxAttempts,
		Window:      time.Duration(ls.WindowSeconds) * time.Second,
		BaseLockout: time.Duration(ls.LockoutSeconds) * time.Second,
		MaxLockout:  time.Duration(ls.LockoutMaxSeconds) * time.Second,
		FailedDelay: time.Duration(ls.FailedDelayMs) * time.Millisecond,
	}
}

// userLoginResolver adapts the myidsan user_login store to an accessrbac principal:
// the user's UserRoleId is its accessrbac role, and an inactive account is disabled.
type userLoginResolver struct {
	repo dbsql.IGenericRepo[sharedentities.UserLogin]
}

func (r *userLoginResolver) ResolveAccessUser(ctx context.Context, userId int64) (*sharedservices.AccessPrincipal, error) {
	// Look up by PRIMARY key — GetByUnique keyed on "id" matches no field (no ukey:"id")
	// and would return the FIRST user, making every session resolve to that user's role.
	u, err := r.repo.GetById(ctx, "", uint64(userId))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no result found") {
			return nil, nil
		}
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	return &sharedservices.AccessPrincipal{RoleId: u.UserRoleId, Disabled: !u.IsActive, MustChangePassword: u.MustChangePassword}, nil
}

// moduleAppVersion reports this app's released version from the shared version manifest,
// falling back to a placeholder when the manifest is unreadable. It is stamped into a
// backup manifest so a restore can warn about a version mismatch.
func moduleAppVersion(m *module) string {
	if manifest, err := versioning.LoadDefault(); err == nil {
		if info, err := manifest.InfoForApp(m.Name()); err == nil {
			return info.AppVersion
		}
	}
	return "1.0.0"
}

func (m *module) APIDocs() apidocs.SpecConfig {
	docVersion := moduleAppVersion(m)

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
		},
	}
}
