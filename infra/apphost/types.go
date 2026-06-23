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
	Config      *config.AppConfigModel
	ConfigPath  string
	Db          dbsql.IDbCrud
	Cache       cache.Store
	Auth        *middlewares.AuthMidware
	Rbac        *middlewares.RbacMidware
	AppRegistry sharedservices.IAppRegistryService
	Logger      applog.Logger
	Scheduler   *scheduler.Scheduler
	// Restarter gracefully restarts the process (factory reset, future self-update).
	Restarter Restarter
}

// SharedAPIConfig controls which shared route groups the host mounts for an app.
type SharedAPIConfig struct {
	Version         bool
	ApiLog          bool
	AppRegistry     bool
	ApiEndpoint     bool
	ApiEndpointRbac bool
	FileStorage     bool
	CacheService    bool
	RuntimeLog      bool
}

// DefaultSharedAPIConfig enables the full shared management surface.
func DefaultSharedAPIConfig() SharedAPIConfig {
	return SharedAPIConfig{
		Version:         true,
		ApiLog:          true,
		AppRegistry:     true,
		ApiEndpoint:     true,
		ApiEndpointRbac: true,
		FileStorage:     true,
		CacheService:    true,
		RuntimeLog:      true,
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
