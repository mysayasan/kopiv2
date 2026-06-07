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

// Dependencies are shared runtime components available to each app module.
type Dependencies struct {
	Config      *config.AppConfigModel
	Db          dbsql.IDbCrud
	Cache       cache.Store
	Auth        *middlewares.AuthMidware
	Rbac        *middlewares.RbacMidware
	AppRegistry sharedservices.IAppRegistryService
	Logger      applog.Logger
	Scheduler   *scheduler.Scheduler
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

// App defines the contract for a runnable application module.
type App interface {
	Name() string
	BaseDir() string
	Entities() []any
	Seeders(seedStatements []string) []bootstrap.Seeder
	RegisterAppRoutes(api *mux.Router, deps Dependencies) (ShutdownFunc, error)
}
