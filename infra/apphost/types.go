package apphost

import (
	"context"

	"github.com/gorilla/mux"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	applog "github.com/mysayasan/kopiv2/infra/logging"
	"github.com/mysayasan/kopiv2/infra/scheduler"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// ShutdownFunc is called during graceful shutdown when app-specific workers exist.
type ShutdownFunc func(ctx context.Context) error

// Restarter gracefully restarts the running process: it triggers the host's normal
// shutdown sequence (stopping app workers and HTTP servers) and then relaunches a
// fresh instance from the current on-disk executable. It is a general primitive —
// used by the factory reset today and intended for self-update later (where the
// on-disk binary is swapped before calling Restart). Calling it more than once is a
// no-op; the first reason wins.
type Restarter interface {
	Restart(reason string)
}

// Dependencies are shared runtime components available to each app module.
type Dependencies struct {
	Config     *config.AppConfigModel
	ConfigPath string
	// HomeDir is the read-only application root (static assets, bundled scripts,
	// the default config). DataDir is the writable state root (config, database,
	// recordings, logs, keys). In a source/dev checkout both point at the app's
	// BaseDir; a packaged install sets them apart via <APP>_HOME / <APP>_DATA.
	// Apps resolve their own writable paths against DataDir (see ResolveWritablePath).
	HomeDir string
	DataDir string
	Db      dbsql.IDbCrud
	Cache       cache.Store
	Auth *middlewares.AuthMidware
	// Access is the shared accessrbac authorization middleware (single-app, no
	// app_code). apphost builds it with a settable resolver; the app binds its own
	// user store via deps.Access.SetResolver(...) during RegisterAppRoutes. AccessRoles
	// / AccessPerms are the matching role + permission services (same DB).
	Access      *middlewares.AccessSessionMidware
	AccessRoles sharedservices.IAccessRoleService
	AccessPerms sharedservices.IAccessPermissionService
	AppRegistry sharedservices.IAppRegistryService
	Logger      applog.Logger
	Scheduler   *scheduler.Scheduler
	// Metrics records app-defined runtime metrics, exposed on the Prometheus scrape
	// endpoint alongside the shared API/coordination series. It is never nil — when
	// telemetry is disabled it is a no-op recorder, so instrumentation on a hot path
	// never needs a guard.
	Metrics telemetry.Metrics
	// Restarter gracefully restarts the process (factory reset, future self-update).
	Restarter Restarter
}

// SharedAPIConfig controls which shared route groups the host mounts for an app.
type SharedAPIConfig struct {
	Version      bool
	ApiLog       bool
	AppRegistry  bool
	ApiEndpoint  bool
	FileStorage  bool
	CacheService bool
	RuntimeLog   bool
	// AccessRbac makes apphost seed the accessrbac roles (superadmin/viewer) + viewer
	// defaults for this app and authorize the shared admin APIs with deps.Access.
	// The app must include the accessrbac entities in Entities() and bind a resolver.
	AccessRbac bool
}

// DefaultSharedAPIConfig enables the full shared management surface.
func DefaultSharedAPIConfig() SharedAPIConfig {
	return SharedAPIConfig{
		Version:      true,
		ApiLog:       true,
		AppRegistry:  true,
		ApiEndpoint:  true,
		FileStorage:  true,
		CacheService: true,
		RuntimeLog:   true,
		AccessRbac:   true,
	}
}

// SharedAPIConfigurator can be implemented by apps that do not expose every shared route group.
type SharedAPIConfigurator interface {
	SharedAPIs() SharedAPIConfig
}

// WebRouteRegistrar can be implemented by apps that need non-API routes before static assets.
type WebRouteRegistrar interface {
	RegisterWebRoutes(router *mux.Router, deps Dependencies) error
}

// AppConfigDecoder is implemented by an app that owns config blocks of its own.
//
// The shared config.AppConfigModel used to carry every app's blocks — nine of its ~25 were
// mymatasan-only, dead weight for the others — and the host itself resolved YOLO training
// directories, so it carried hardcoded knowledge of a vision feature. Neither survives a
// fourth app.
//
// An app now decodes its own blocks from the SAME raw config document the host parsed
// (config.AppConfigModel.Raw()), and normalizes its own data-relative paths against
// dataDir. Nothing nests under an "app" key: the blocks stay at the top level of
// config.json exactly where they are, so no deployed config file has to change. What moved
// is ownership, not format.
//
// It is called after the shared config is loaded and normalized, and before any route is
// registered. An error here aborts startup — a config the app cannot understand must not
// boot on silent defaults.
type AppConfigDecoder interface {
	DecodeAppConfig(raw []byte, dataDir string) error
}

// ReadinessReporter can be implemented by an app module to add extra status
// fields to the /ready payload (e.g. machine and camera health). The values are
// advisory and do NOT change the ready/not-ready verdict — that stays gated on
// the hard dependencies (database, cache) so a full disk or an offline camera
// never makes an orchestrator stop routing traffic to an otherwise-serving node.
type ReadinessReporter interface {
	ReadinessStatus(ctx context.Context) map[string]string
}

// App defines the contract for a runnable application module.
type App interface {
	Name() string
	BaseDir() string
	Entities() []any
	Seeders(seedStatements []string) []bootstrap.Seeder
	RegisterAppRoutes(api *mux.Router, deps Dependencies) (ShutdownFunc, error)
}
