package apphost

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	sharedEntities "github.com/mysayasan/kopiv2/domain/entities"
	sharedApis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedOutputDtos "github.com/mysayasan/kopiv2/domain/shared/dtos/output"
	sharedServices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	appcache "github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/coordination"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/db/sql/mariadb"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
	"github.com/mysayasan/kopiv2/infra/db/sql/sqlite"
	applog "github.com/mysayasan/kopiv2/infra/logging"
	"github.com/mysayasan/kopiv2/infra/scheduler"
	infraTelemetry "github.com/mysayasan/kopiv2/infra/telemetry"
	promTelemetry "github.com/mysayasan/kopiv2/infra/telemetry/prometheus"
	"github.com/mysayasan/kopiv2/infra/versioning"
)

type spaHandler struct {
	staticPath string
	indexPath  string
}

type listenerSpec struct {
	Addr   string
	UseTLS bool
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(h.staticPath, r.URL.Path)

	fi, err := os.Stat(path)
	if os.IsNotExist(err) || fi.IsDir() {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"alive": true}`)
}

// readinessHolder lets the app's ReadinessReporter be attached after route
// registration (app services are built later in RegisterAppRoutes) while the
// /ready handler is mounted early. Safe for concurrent use.
type readinessHolder struct {
	mu       sync.RWMutex
	reporter ReadinessReporter
}

func (h *readinessHolder) set(r ReadinessReporter) {
	h.mu.Lock()
	h.reporter = r
	h.mu.Unlock()
}

func (h *readinessHolder) get() ReadinessReporter {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.reporter
}

func readinessCheckHandler(db dbsql.IDbCrud, cacheStore appcache.Store, extra *readinessHolder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "db": "down", "cache": "unknown"})
			return
		}

		if err := cacheStore.Ping(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "db": "up", "cache": "down"})
			return
		}

		payload := map[string]any{"ok": true, "db": "up", "cache": "up"}
		// Merge app-provided advisory status (e.g. machine, cameras). These never
		// flip ok/HTTP status — readiness stays gated on db + cache only.
		if extra != nil {
			if reporter := extra.get(); reporter != nil {
				for key, value := range reporter.ReadinessStatus(ctx) {
					if key == "ok" || key == "db" || key == "cache" {
						continue
					}
					payload[key] = value
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(payload)
	}
}

// Run starts a selected app module using shared runtime wiring.
func Run(app App) error {
	godotenv.Load(".env")

	// When relaunched by a previous instance's self-restart (Windows detached child),
	// wait briefly so that instance has fully exited and released the listen port before
	// we try to bind it. Unset so it doesn't propagate to any future restart.
	if ms := strings.TrimSpace(os.Getenv("KOPIV2_RESTART_DELAY_MS")); ms != "" {
		os.Unsetenv("KOPIV2_RESTART_DELAY_MS")
		if d, perr := strconv.Atoi(ms); perr == nil && d > 0 && d <= 30000 {
			time.Sleep(time.Duration(d) * time.Millisecond)
		}
	}

	homeDir := resolveHomeDir(app)
	dataDir := resolveDataDir(app, homeDir)
	log.Printf("paths app=%s home=%s data=%s", app.Name(), homeDir, dataDir)

	appConfig, err := loadConfig(homeDir, dataDir)
	if err != nil {
		return err
	}

	sharedAPIConfig := sharedAPIConfigFor(app)
	if err := applySensitiveConfig(appConfig, configFilePath(dataDir)); err != nil {
		return err
	}
	applyDbConfigFromEnv(appConfig)
	applyCacheConfigFromEnv(appConfig)
	applySSOConfigFromEnv(appConfig)
	applyTransactionConfigFromEnv(appConfig)
	applyLoggingConfigFromEnv(appConfig)
	applyApiLogConfigFromEnv(appConfig)
	applyTelemetryConfigFromEnv(appConfig)
	applyRateLimitConfigFromEnv(appConfig)
	if err := applyServerConfigFromEnv(appConfig); err != nil {
		return err
	}
	normalizePathConfig(dataDir, appConfig)

	runtimeLogger, err := buildRuntimeLogger(app.Name(), dataDir, appConfig)
	if err != nil {
		return err
	}
	defer runtimeLogger.Close()
	log.SetFlags(0)
	log.SetOutput(runtimeLogger)

	bootstrapStatus, err := bootstrap.Ensure(context.Background(), bootstrap.Options{
		AppName: app.Name(),
		Config:  appConfig.Db,
		Bootstrap: bootstrap.BootstrapConfig{
			Enabled:            appConfig.Bootstrap.Enabled,
			AutoCreateDatabase: appConfig.Bootstrap.AutoCreateDatabase,
			AutoCreateSchema:   appConfig.Bootstrap.AutoCreateSchema,
			AutoMigrate:        appConfig.Bootstrap.AutoMigrate,
			AutoSeed:           appConfig.Bootstrap.AutoSeed,
			AllowReset:         appConfig.Bootstrap.AllowReset,
			SetupPath:          appConfig.Bootstrap.SetupPath,
			SeedStatements:     appConfig.Bootstrap.SeedStatements,
		},
		Entities: app.Entities(),
		Seeders:  app.Seeders(appConfig.Bootstrap.SeedStatements),
	})
	if err != nil {
		return err
	}

	log.Printf("bootstrap status app=%s ready=%t drift=%t db_created=%t schema_created=%t schema_updated=%t seeded=%t message=%s", app.Name(), bootstrapStatus.Ready, bootstrapStatus.DriftDetected, bootstrapStatus.DatabaseCreated, bootstrapStatus.SchemaCreated, bootstrapStatus.SchemaUpdated, bootstrapStatus.Seeded, bootstrapStatus.Message)

	setupPath := appConfig.Bootstrap.SetupPath
	if setupPath == "" {
		setupPath = "/setup"
	}

	router := mux.NewRouter()

	greetMidware := middlewares.NewGreet()
	router.Use(greetMidware.GreetHandler)
	corsMidware := middlewares.NewCors(appConfig.AllowOrigin)
	router.Use(corsMidware.CorsHandler)
	requestLogMidware := middlewares.NewRequestLog(runtimeLogger)
	router.Use(requestLogMidware.Middleware)

	router.HandleFunc("/health", healthCheckHandler)
	router.HandleFunc(setupPath, bootstrap.SetupPageHandler(func() bootstrap.Status { return *bootstrapStatus }))
	router.HandleFunc(setupPath+"/status", bootstrap.StatusHandler(func() bootstrap.Status { return *bootstrapStatus }))

	dbCrud, err := newDbCrud(appConfig.Db)
	if err != nil {
		return fmt.Errorf("error connecting to db: %w", err)
	}

	cacheStore, cacheProvider, err := buildCacheStore(appConfig)
	if err != nil {
		return err
	}
	defer cacheStore.Close()

	if err := cacheStore.Ping(context.Background()); err != nil {
		return fmt.Errorf("cache provider %s not reachable: %w", cacheProvider, err)
	}
	log.Printf("cache provider=%s addr=%s", cacheProvider, appConfig.Cache.Redis.Address)

	telemetryRecorder := buildTelemetryRecorder(router, app.Name(), appConfig, runtimeLogger)
	txLocker, txLockProvider, err := buildTransactionLocker(app.Name(), appConfig, telemetryRecorder)
	if err != nil {
		return err
	}
	defer txLocker.Close()
	if err := txLocker.Ping(context.Background()); err != nil {
		return fmt.Errorf("transaction lock provider %s not reachable: %w", txLockProvider, err)
	}
	log.Printf("transaction lock provider=%s waitTimeoutMs=%d leaseMs=%d stuckTimeoutMs=%d", txLockProvider, transactionLockWaitTimeout(appConfig).Milliseconds(), transactionLockLease(appConfig).Milliseconds(), transactionStuckTimeout(appConfig).Milliseconds())

	readyHolder := &readinessHolder{}
	router.HandleFunc("/ready", readinessCheckHandler(dbCrud, cacheStore, readyHolder)).Methods("GET")

	apiLogRepo := dbsql.NewGenericRepo[sharedEntities.ApiLog](dbCrud)
	appRegistryRepo := dbsql.NewGenericRepo[sharedEntities.AppRegistry](dbCrud)
	apiEpRepo := dbsql.NewGenericRepo[sharedEntities.ApiEndpoint](dbCrud)
	fileStorRepo := dbsql.NewGenericRepo[sharedEntities.FileStorage](dbCrud)
	operationJobRepo := dbsql.NewGenericRepo[sharedEntities.OperationJob](dbCrud)

	// Shared accessrbac core (single-app, no app_code): role + permission services
	// and the late-bound authorization middleware. The app binds its own user store
	// via deps.Access.SetResolver(...) in RegisterAppRoutes; seeding is gated so apps
	// that don't opt in (mymatasan) need not carry the accessrbac tables.
	accessRoleService := sharedServices.NewAccessRoleService(dbCrud)
	accessPermService := sharedServices.NewAccessPermissionService(dbCrud)
	accessMidware := middlewares.NewAccessSessionMidware(nil, accessRoleService, accessPermService)
	if sharedAPIConfig.AccessRbac {
		if err := accessRoleService.EnsureBuiltins(context.Background()); err != nil {
			return fmt.Errorf("seed accessrbac roles: %w", err)
		}
		// Enforce least privilege for the viewer role: strip the legacy read-everything
		// GET /api wildcard if a prior build seeded it (viewer now starts with no access).
		if viewer, err := accessRoleService.GetByName(context.Background(), sharedServices.RoleViewer); err == nil && viewer != nil {
			if err := accessPermService.EnsureViewerDefaults(context.Background(), viewer.Id); err != nil {
				return fmt.Errorf("enforce accessrbac viewer least-privilege defaults: %w", err)
			}
		}
	}

	apiLogService := sharedServices.NewApiLogService(apiLogRepo, cacheStore)
	appRegistryService := sharedServices.NewAppRegistryService(appRegistryRepo, cacheStore)
	apiEndpointService := sharedServices.NewApiEndpointService(apiEpRepo, cacheStore)
	fileStorageService := sharedServices.NewFileStorageService(
		fileStorRepo,
		cacheStore,
		sharedServices.WithFileStorageJobRepo(operationJobRepo),
		sharedServices.WithFileStorageAccessRoles(accessRoleService),
		sharedServices.WithFileStorageTransaction(dbCrud),
		sharedServices.WithFileStorageLocker(txLocker),
		sharedServices.WithFileStoragePath(appConfig.FileStorage.Path),
		sharedServices.WithFileStorageOperationTimeout(transactionOperationTimeout(appConfig)),
		sharedServices.WithFileStorageMaxAttempts(transactionMaxAttempts(appConfig)),
	)
	cacheService := sharedServices.NewCacheService(cacheStore)
	runtimeLogService := sharedServices.NewRuntimeLogService(runtimeLogger)
	apiLogDtoService := sharedServices.NewApiLogDtoService[sharedOutputDtos.ApiLogDto](apiLogService)
	appRegistryDtoService := sharedServices.NewAppRegistryDtoService[sharedOutputDtos.AppRegistryDto](appRegistryService)
	apiEndpointDtoService := sharedServices.NewApiEndpointDtoService[sharedOutputDtos.ApiEndpointDto](apiEndpointService)
	fileStorageDtoService := sharedServices.NewFileStorageDtoService[sharedOutputDtos.FileStorageDto, sharedOutputDtos.OperationJobDto](fileStorageService)
	runtimeLogDtoService := sharedServices.NewRuntimeLogDtoService[sharedOutputDtos.RuntimeLogDto](runtimeLogService)
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	runtimeScheduler := scheduler.New(schedulerCtx, runtimeLogger)
	startRuntimeLogCleanupScheduler(runtimeScheduler, appConfig, runtimeLogger, runtimeLogService)
	startApiLogCleanupScheduler(runtimeScheduler, appConfig, runtimeLogger, apiLogService)
	startFileStorageCleanupScheduler(runtimeScheduler, appConfig, runtimeLogger, fileStorageService)
	startFileStorageJobScheduler(runtimeScheduler, appConfig, runtimeLogger, fileStorageService)

	auth := middlewares.NewAuthWithConfig(authMiddlewareConfig(app.Name(), appConfig, cacheStore))
	versionManifest, err := versioning.LoadDefault()
	if err != nil {
		return fmt.Errorf("error loading version manifest: %w", err)
	}
	api := router.PathPrefix("/api").Subrouter()
	apiActivityLogMidware := middlewares.NewApiActivityLog(
		apiLogService,
		auth,
		runtimeLogger,
		middlewares.WithApiActivityAppName(app.Name()),
		middlewares.WithApiActivityTelemetry(telemetryRecorder),
	)
	api.Use(apiActivityLogMidware.Middleware)
	rateLimitMidware := middlewares.NewRateLimit(apiEndpointService, cacheStore, auth, rateLimitMiddlewareConfig(appConfig))
	api.Use(rateLimitMidware.Middleware)

	if sharedAPIConfig.Version {
		sharedApis.NewVersionApi(api, app.Name(), versionManifest)
	}
	// Shared admin APIs are now authorized by the accessrbac middleware (deps.Access);
	// the app binds its user resolver in RegisterAppRoutes. The old app_code RBAC
	// management endpoint (ApiEndpointRbac) is retired in favour of accessrbac roles.
	if sharedAPIConfig.ApiLog {
		sharedApis.NewApiLogApi(api, *auth, accessMidware, apiLogDtoService)
	}
	if sharedAPIConfig.AppRegistry {
		sharedApis.NewAppRegistryApi(api, *auth, accessMidware, appRegistryDtoService)
	}
	if sharedAPIConfig.ApiEndpoint {
		sharedApis.NewApiEndpointApi(api, *auth, accessMidware, apiEndpointDtoService)
	}
	if sharedAPIConfig.FileStorage {
		sharedApis.NewFileStorageApi(api, *auth, accessMidware, fileStorageDtoService, appConfig.FileStorage.Path)
	}
	if sharedAPIConfig.CacheService {
		sharedApis.NewCacheServiceApi(api, *auth, accessMidware, cacheService, apiLogService)
	}
	if sharedAPIConfig.RuntimeLog {
		sharedApis.NewRuntimeLogApi(api, *auth, accessMidware, runtimeLogDtoService)
	}
	if sharedAPIConfig.AccessRbac {
		// The shared "Settings → RBAC" management surface (roles + permission matrix),
		// identical on every app that uses the accessrbac core.
		sharedApis.NewAccessRbacApi(api, *auth, accessMidware, accessRoleService, accessPermService)
	}

	// restarter lets app modules request a graceful restart (factory reset today,
	// self-update later). It cancels restartCtx, which the run loop selects on, then
	// the post-shutdown path relaunches a fresh process from the on-disk executable.
	restartCtx, restartCancel := context.WithCancel(context.Background())
	defer restartCancel()
	restarter := &appRestarter{cancel: restartCancel}

	deps := Dependencies{
		Config:      appConfig,
		ConfigPath:  configFilePath(dataDir),
		HomeDir:     homeDir,
		DataDir:     dataDir,
		Db:          dbCrud,
		Cache:       cacheStore,
		Auth:        auth,
		Access:      accessMidware,
		AccessRoles: accessRoleService,
		AccessPerms: accessPermService,
		AppRegistry: appRegistryService,
		Logger:      runtimeLogger,
		Scheduler:   runtimeScheduler,
		Restarter:   restarter,
	}

	shutdownHook, err := app.RegisterAppRoutes(api, deps)
	if err != nil {
		return err
	}
	// App services (e.g. health monitors) are constructed during RegisterAppRoutes;
	// attach the reporter now so /ready can include their advisory status.
	if reporter, ok := app.(ReadinessReporter); ok {
		readyHolder.set(reporter)
	}
	if webRoutes, ok := app.(WebRouteRegistrar); ok {
		if err := webRoutes.RegisterWebRoutes(router, deps); err != nil {
			return err
		}
	}

	api.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	var docProvider apidocs.Provider
	if provider, ok := app.(apidocs.Provider); ok {
		docProvider = provider
	}
	apidocs.Register(router, app.Name(), docProvider)

	router.PathPrefix("/").Handler(spaHandler{staticPath: filepath.Join(homeDir, "static"), indexPath: "index.html"})

	listeners, err := buildListenerSpecs(appConfig)
	if err != nil {
		return err
	}

	// If any listener serves TLS, make sure a keypair exists — generate a
	// self-signed one on first boot so a fresh install serves HTTPS out of the box.
	for _, listener := range listeners {
		if listener.UseTLS {
			if err := ensureSelfSignedCert(appConfig.Tls.CertPath, appConfig.Tls.KeyPath, appConfig.Server.Hostnames); err != nil {
				return err
			}
			break
		}
	}

	servers := make([]*http.Server, 0, len(listeners))
	errChan := make(chan error, len(listeners))

	for _, listener := range listeners {
		srv := &http.Server{
			Handler:           router,
			Addr:              listener.Addr,
			ReadHeaderTimeout: serverTimeout(appConfig.Server.ReadHeaderTimeoutSeconds, 5),
			ReadTimeout:       serverTimeout(appConfig.Server.ReadTimeoutSeconds, 15),
			WriteTimeout:      serverTimeout(appConfig.Server.WriteTimeoutSeconds, 30),
			IdleTimeout:       serverTimeout(appConfig.Server.IdleTimeoutSeconds, 60),
		}
		servers = append(servers, srv)

		protocol := "http"
		if listener.UseTLS {
			protocol = "https"
		}
		log.Printf("starting %s server on %s", protocol, listener.Addr)

		go func() {
			errChan <- runServer(srv, appConfig, listener.UseTLS)
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	relaunch := false
	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		log.Println("shutdown signal received")
	case <-restartCtx.Done():
		relaunch = true
		log.Printf("restart requested: %s", restarter.Reason())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run the shutdown hook bounded by shutdownCtx, in a goroutine, so a hook step that
	// blocks (e.g. a service touching a just-dropped database during a factory reset, or
	// a stuck stream teardown) can never wedge the shutdown/relaunch. On timeout we log
	// and push on — the process is going down or relaunching regardless.
	if shutdownHook != nil {
		done := make(chan struct{})
		go func() {
			if err := shutdownHook(shutdownCtx); err != nil {
				log.Printf("app shutdown warning: %v", err)
			}
			close(done)
		}()
		select {
		case <-done:
		case <-shutdownCtx.Done():
			log.Printf("app shutdown hook timed out; proceeding with %s", map[bool]string{true: "relaunch", false: "exit"}[relaunch])
		}
	}

	for _, srv := range servers {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown warning on %s: %v", srv.Addr, err)
		}
	}

	if relaunch {
		if supervisedRestart() {
			// Under a process supervisor (systemd, launchd, a Windows service, Docker
			// `restart: unless-stopped`), the most reliable cross-platform restart is to
			// exit and let the supervisor relaunch us. Self-relaunch here would race the
			// supervisor and double-start. Exit NON-ZERO (restartExitCode) so supervisors
			// that only relaunch on failure (e.g. WinSW <onfailure>) restart us, while a
			// normal `systemctl stop` / `docker stop` (exit 0 below) is left stopped.
			log.Printf("restart requested; exiting (code %d) for the process supervisor to relaunch (KOPIV2_SUPERVISED)", restartExitCode)
			os.Exit(restartExitCode)
		}
		// No supervisor (bare/dev run): relaunch ourselves. On Unix this re-execs in
		// place (reliable, no port hand-off race, no double start); on Windows it spawns
		// a detached child and this process exits.
		if err := relaunchSelf(); err != nil {
			log.Printf("relaunch failed (%v); exiting — run under a process supervisor (systemd/launchd/Windows service/Docker) or set KOPIV2_SUPERVISED=1 to restart reliably", err)
		}
	}

	return nil
}

// restartExitCode is the non-zero exit code used for a supervised restart, so a
// supervisor relaunches the app while a normal stop (exit 0) leaves it stopped.
const restartExitCode = 70

// supervisedRestart reports whether the app is managed by a process supervisor that
// will relaunch it on exit, so a restart should be a clean exit rather than a
// self-relaunch. Enabled via KOPIV2_SUPERVISED (1/true/yes).
func supervisedRestart() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("KOPIV2_SUPERVISED")))
	return v == "1" || v == "true" || v == "yes"
}

// appRestarter implements Restarter by cancelling the run loop's restart context.
// The first call wins; later calls are ignored so concurrent triggers can't race.
type appRestarter struct {
	once   sync.Once
	cancel context.CancelFunc
	mu     sync.Mutex
	reason string
}

func (r *appRestarter) Restart(reason string) {
	r.once.Do(func() {
		r.mu.Lock()
		r.reason = reason
		r.mu.Unlock()
		r.cancel()
	})
}

func (r *appRestarter) Reason() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reason == "" {
		return "unspecified"
	}
	return r.reason
}

// configFilePath returns the absolute path of the config file the host loads for
// this baseDir, honouring the dev override. Apps use it (via Dependencies) to
// persist runtime config changes back to the same file.
func configFilePath(baseDir string) string {
	configFile := "config.json"
	if os.Getenv("ENVIRONMENT") == "dev" {
		configFile = "config.dev.json"
	}
	return filepath.Join(baseDir, configFile)
}

// loadConfig loads the app's mutable config from dataDir. On first run in a
// packaged install (dataDir separate from homeDir) it seeds dataDir's copy from
// the read-only default shipped in homeDir so the app has a writable config to
// persist secrets/runtime settings back to. In a dev checkout homeDir==dataDir,
// so the seed is a no-op and behaviour is unchanged.
func loadConfig(homeDir, dataDir string) (*config.AppConfigModel, error) {
	target := configFilePath(dataDir)
	source := configFilePath(homeDir)
	if target != source {
		if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
			if _, serr := os.Stat(source); serr == nil {
				if cerr := copyFile(source, target); cerr != nil {
					return nil, fmt.Errorf("seed config into data dir: %w", cerr)
				}
				log.Printf("seeded default config %s -> %s", source, target)
			}
		}
	}

	appConfig, err := config.LoadAppConfiguration(target)
	if err != nil {
		return nil, err
	}
	if appConfig == nil {
		return nil, errors.New("config file not found")
	}

	return appConfig, nil
}

func normalizePathConfig(dataDir string, appConfig *config.AppConfigModel) {
	appConfig.FileStorage.Path = ResolveWritablePath(dataDir, appConfig.FileStorage.Path)
	appConfig.Logging.Path = ResolveWritablePath(dataDir, appConfig.Logging.Path)
	appConfig.Tls.CertPath = ResolveWritablePath(dataDir, appConfig.Tls.CertPath)
	appConfig.Tls.KeyPath = ResolveWritablePath(dataDir, appConfig.Tls.KeyPath)
	appConfig.SSO.CACertPath = ResolveWritablePath(dataDir, appConfig.SSO.CACertPath)
	if normalizeDbEngine(appConfig.Db.Engine) == "sqlite" && appConfig.Db.DbName != ":memory:" {
		appConfig.Db.DbName = ResolveWritablePath(dataDir, appConfig.Db.DbName)
	}
	// Recordings/snapshots root. Historically defaulted (in the app) to a
	// CWD-relative "recordings"; resolve it here so it lands under the data dir on
	// a packaged install while the legacy fallback keeps existing footage in place.
	snapshotDir := strings.TrimSpace(appConfig.Vision.SnapshotDir)
	if snapshotDir == "" {
		snapshotDir = "recordings"
	}
	appConfig.Vision.SnapshotDir = ResolveWritablePath(dataDir, snapshotDir)
	if td := strings.TrimSpace(appConfig.Vision.Training.DataDir); td != "" {
		appConfig.Vision.Training.DataDir = ResolveWritablePath(dataDir, td)
	}
}

// ResolveWritablePath resolves a data-relative path against dataDir. Absolute
// inputs pass through unchanged. As a one-time migration aid, when the dataDir
// target does not yet exist but a copy exists at the current working directory
// (how pre-packaging builds resolved these paths), the existing legacy location
// is returned so an upgrade never orphans in-place recordings, keys, or a
// database. Installed services set WorkingDirectory=dataDir, which makes the
// legacy path identical to the target and the fallback a no-op.
func ResolveWritablePath(dataDir, target string) string {
	if target == "" || filepath.IsAbs(target) {
		return target
	}
	resolved := filepath.Clean(filepath.Join(dataDir, target))
	if _, err := os.Stat(resolved); err == nil {
		return resolved
	}
	if legacy := filepath.Clean(target); legacy != resolved {
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return resolved
}

// copyFile copies src to dst, creating parent directories as needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// resolveHomeDir locates the read-only application root (static assets, bundled
// scripts, default config). Resolution order: an explicit <APP>_HOME / KOPIV2_HOME
// env override; else the app's declared BaseDir if it exists relative to the
// working directory (source/dev checkout); else the directory of the running
// executable (packaged/portable install).
func resolveHomeDir(app App) string {
	if v := envOverride(app, "HOME"); v != "" {
		return filepath.Clean(v)
	}
	rel := filepath.Clean(app.BaseDir())
	if fi, err := os.Stat(rel); err == nil && fi.IsDir() {
		return rel
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return rel
}

// resolveDataDir locates the writable state root (config, database, recordings,
// logs, keys). An explicit <APP>_DATA / KOPIV2_DATA env override wins; otherwise
// it defaults to homeDir, preserving the single-directory dev layout.
func resolveDataDir(app App, homeDir string) string {
	if v := envOverride(app, "DATA"); v != "" {
		return filepath.Clean(v)
	}
	return homeDir
}

// envOverride reads an app-scoped path override (e.g. MYMATASAN_HOME) with a
// generic KOPIV2_-prefixed fallback shared across apps (handy for containers).
func envOverride(app App, suffix string) string {
	if v := strings.TrimSpace(os.Getenv(strings.ToUpper(app.Name()) + "_" + suffix)); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("KOPIV2_" + suffix))
}

func sharedAPIConfigFor(app App) SharedAPIConfig {
	if provider, ok := app.(SharedAPIConfigurator); ok {
		return provider.SharedAPIs()
	}
	return DefaultSharedAPIConfig()
}

func applySensitiveConfig(appConfig *config.AppConfigModel, configPath string) error {
	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		appConfig.Jwt.Secret = jwtSecret
	} else if isWeakJWTSecret(appConfig.Jwt.Secret) {
		// Refuse to run on an unset/placeholder secret. Generate a strong random
		// one, persist it back to the config file so it survives restarts, and use
		// it for this run regardless of whether the write succeeds.
		generated, err := generateRandomSecret(32)
		if err != nil {
			return fmt.Errorf("generate jwt secret: %w", err)
		}
		old := strings.TrimSpace(appConfig.Jwt.Secret)
		appConfig.Jwt.Secret = generated
		if err := persistJWTSecret(configPath, generated); err != nil {
			log.Printf("WARNING: insecure JWT secret %q replaced with a generated one for this run, but it could not be persisted (%v); it will change on restart — set JWT_SECRET or jwt.secret to a strong value", old, err)
		} else {
			log.Printf("WARNING: insecure JWT secret %q replaced with a strong generated secret and saved to %s", old, configPath)
		}
	}

	if appConfig.Jwt.Secret == "" {
		return errors.New("JWT secret is required")
	}

	if appConfig.Login != nil && appConfig.Login.Google != nil {
		if secret := os.Getenv("GOOGLE_CLIENT_SECRET"); secret != "" {
			appConfig.Login.Google.ClientSecret = secret
		}
		// A provider block with no usable client id/secret (config + env both empty) is
		// treated as not configured: disable it with a warning instead of refusing to
		// boot, so a half-set-up social provider never blocks the whole identity service.
		if strings.TrimSpace(appConfig.Login.Google.ClientId) == "" || strings.TrimSpace(appConfig.Login.Google.ClientSecret) == "" {
			log.Printf("WARNING: google login disabled — set login.google.client_id and a client secret (config or GOOGLE_CLIENT_SECRET env)")
			appConfig.Login.Google = nil
		}
	}
	if appConfig.Login != nil && appConfig.Login.GitHub != nil {
		if secret := os.Getenv("GITHUB_CLIENT_SECRET"); secret != "" {
			appConfig.Login.GitHub.ClientSecret = secret
		}
		if strings.TrimSpace(appConfig.Login.GitHub.ClientId) == "" || strings.TrimSpace(appConfig.Login.GitHub.ClientSecret) == "" {
			log.Printf("WARNING: github login disabled — set login.github.client_id and a client secret (config or GITHUB_CLIENT_SECRET env)")
			appConfig.Login.GitHub = nil
		}
	}

	return nil
}

// weakJWTSecrets are placeholder values shipped in example configs that must
// never reach production. Compared case-insensitively against the trimmed value.
var weakJWTSecrets = map[string]struct{}{
	"change-me":            {},
	"changeme":             {},
	"standalone-change-me": {},
	"please-change-me":     {},
	"your-secret-here":     {},
	"secret":               {},
	"test0123456789":       {},
}

// isWeakJWTSecret reports whether a JWT secret is unsafe to run with: empty, too
// short to resist brute force, or a known placeholder.
func isWeakJWTSecret(secret string) bool {
	s := strings.TrimSpace(secret)
	if len(s) < 16 {
		return true
	}
	_, weak := weakJWTSecrets[strings.ToLower(s)]
	return weak
}

// generateRandomSecret returns a URL-safe base64 string of nBytes of CSPRNG
// entropy (no padding, so it is JSON- and regex-replacement-safe).
func generateRandomSecret(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// jwtSecretPattern captures the value of jwt.secret so it can be rewritten in
// place without reformatting the rest of the config file. The jwt object holds
// only the secret, so a non-greedy scan within its braces is unambiguous.
var jwtSecretPattern = regexp.MustCompile(`("jwt"\s*:\s*\{[^{}]*?"secret"\s*:\s*")([^"]*)(")`)

// persistJWTSecret surgically replaces the jwt.secret value in the config file on
// disk, preserving the file's existing formatting and key order.
func persistJWTSecret(configPath, newSecret string) error {
	if strings.TrimSpace(configPath) == "" {
		return errors.New("no config path")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if !jwtSecretPattern.Match(raw) {
		return errors.New("jwt.secret not found in config file")
	}
	updated := jwtSecretPattern.ReplaceAll(raw, []byte("${1}"+newSecret+"${3}"))
	return os.WriteFile(configPath, updated, 0o600)
}

func applyDbConfigFromEnv(appConfig *config.AppConfigModel) {
	if v := strings.TrimSpace(os.Getenv("DB_ENGINE")); v != "" {
		appConfig.Db.Engine = v
	}

	if v := strings.TrimSpace(os.Getenv("DB_HOST")); v != "" {
		appConfig.Db.Host = v
	}

	if v := strings.TrimSpace(os.Getenv("DB_PORT")); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			appConfig.Db.Port = port
		}
	}

	if v := strings.TrimSpace(os.Getenv("DB_USER")); v != "" {
		appConfig.Db.User = v
	}

	if v := os.Getenv("DB_PASSWORD"); v != "" {
		appConfig.Db.Password = v
	}

	if v := strings.TrimSpace(os.Getenv("DB_NAME")); v != "" {
		appConfig.Db.DbName = v
	}

	if v := strings.TrimSpace(os.Getenv("DB_SSL_MODE")); v != "" {
		appConfig.Db.SslMode = v
	}
}

func applyCacheConfigFromEnv(appConfig *config.AppConfigModel) {
	if v := strings.TrimSpace(os.Getenv("CACHE_PROVIDER")); v != "" {
		appConfig.Cache.Provider = v
	}

	if v := strings.TrimSpace(os.Getenv("CACHE_TTL_SECONDS")); v != "" {
		if ttl, err := strconv.Atoi(v); err == nil && ttl > 0 {
			appConfig.Cache.TTLSeconds = ttl
		}
	}

	if v := strings.TrimSpace(os.Getenv("CACHE_KEY_PREFIX")); v != "" {
		appConfig.Cache.KeyPrefix = v
	}

	if v := strings.TrimSpace(os.Getenv("REDIS_ADDR")); v != "" {
		appConfig.Cache.Redis.Address = v
	}

	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		appConfig.Cache.Redis.Password = v
	}

	if v := strings.TrimSpace(os.Getenv("REDIS_DB")); v != "" {
		if db, err := strconv.Atoi(v); err == nil && db >= 0 {
			appConfig.Cache.Redis.DB = db
		}
	}

	if v := strings.TrimSpace(os.Getenv("REDIS_USE_TLS")); v != "" {
		appConfig.Cache.Redis.UseTLS = getBoolEnv("REDIS_USE_TLS", appConfig.Cache.Redis.UseTLS)
	}

	if v := strings.TrimSpace(os.Getenv("REDIS_CONNECT_TIMEOUT_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			appConfig.Cache.Redis.ConnectTimeoutMs = ms
		}
	}

	if v := strings.TrimSpace(os.Getenv("REDIS_OPERATION_TIMEOUT_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			appConfig.Cache.Redis.OperationTimeoutMs = ms
		}
	}
}

func applySSOConfigFromEnv(appConfig *config.AppConfigModel) {
	if v := strings.TrimSpace(os.Getenv("SSO_ISSUER")); v != "" {
		appConfig.SSO.Issuer = v
	}
	if v := strings.TrimSpace(os.Getenv("SSO_AUDIENCE")); v != "" {
		appConfig.SSO.Audience = v
	}
	if v := strings.TrimSpace(os.Getenv("SSO_SESSION_TTL_SECONDS")); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			appConfig.SSO.SessionTTLSeconds = seconds
		}
	}
	if v := strings.TrimSpace(os.Getenv("SSO_POLICY_CACHE_TTL_SECONDS")); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			appConfig.SSO.PolicyCacheTTLSeconds = seconds
		}
	}
	if v := os.Getenv("SSO_INTERNAL_TOKEN"); v != "" {
		appConfig.SSO.InternalToken = v
	}
	if v := strings.TrimSpace(os.Getenv("SSO_PROVIDER_BASE_URL")); v != "" {
		appConfig.SSO.ProviderBaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("SSO_CA_CERT_PATH")); v != "" {
		appConfig.SSO.CACertPath = v
	}
	if v := strings.TrimSpace(os.Getenv("SSO_CLIENT_ID")); v != "" {
		appConfig.SSO.ClientID = v
	}
	if v := os.Getenv("SSO_CLIENT_SECRET"); v != "" {
		appConfig.SSO.ClientSecret = v
	}
	if v := strings.TrimSpace(os.Getenv("SSO_REDIRECT_BASE_URL")); v != "" {
		appConfig.SSO.RedirectBaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("SSO_REDIRECT_PATH")); v != "" {
		appConfig.SSO.RedirectPath = v
	}
	if v := strings.TrimSpace(os.Getenv("SSO_AUTH_CODE_TTL_SECONDS")); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			appConfig.SSO.AuthCodeTTLSeconds = seconds
		}
	}
	if v := strings.TrimSpace(os.Getenv("SSO_ACCESS_TOKEN_TTL_SECONDS")); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			appConfig.SSO.AccessTokenTTLSeconds = seconds
		}
	}
}

func applyTransactionConfigFromEnv(appConfig *config.AppConfigModel) {
	if v := strings.TrimSpace(os.Getenv("TRANSACTION_LOCK_PROVIDER")); v != "" {
		appConfig.Transaction.LockProvider = v
	}

	if v := strings.TrimSpace(os.Getenv("TRANSACTION_LOCK_WAIT_TIMEOUT_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			appConfig.Transaction.LockWaitTimeoutMs = ms
		}
	}

	if v := strings.TrimSpace(os.Getenv("TRANSACTION_LOCK_LEASE_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			appConfig.Transaction.LockLeaseMs = ms
		}
	}

	if v := strings.TrimSpace(os.Getenv("TRANSACTION_OPERATION_TIMEOUT_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			appConfig.Transaction.OperationTimeoutMs = ms
		}
	}

	if v := strings.TrimSpace(os.Getenv("TRANSACTION_STUCK_TIMEOUT_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			appConfig.Transaction.StuckTimeoutMs = ms
		}
	}

	if v := strings.TrimSpace(os.Getenv("TRANSACTION_JOB_WORKER_ENABLED")); v != "" {
		appConfig.Transaction.JobWorkerEnabled = getBoolEnv("TRANSACTION_JOB_WORKER_ENABLED", appConfig.Transaction.JobWorkerEnabled)
	}

	if v := strings.TrimSpace(os.Getenv("TRANSACTION_JOB_WORKER_FREQUENCY_SECONDS")); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			appConfig.Transaction.JobWorkerFrequencySeconds = seconds
		}
	}

	if v := strings.TrimSpace(os.Getenv("TRANSACTION_MAX_ATTEMPTS")); v != "" {
		if attempts, err := strconv.Atoi(v); err == nil && attempts > 0 {
			appConfig.Transaction.MaxAttempts = attempts
		}
	}
}

func applyLoggingConfigFromEnv(appConfig *config.AppConfigModel) {
	if v := strings.TrimSpace(os.Getenv("LOG_ENABLED")); v != "" {
		appConfig.Logging.Enabled = getBoolEnv("LOG_ENABLED", appConfig.Logging.Enabled)
	}

	if v := strings.TrimSpace(os.Getenv("LOG_PATH")); v != "" {
		appConfig.Logging.Path = v
	}

	if v := strings.TrimSpace(os.Getenv("LOG_MAX_LINE_BYTES")); v != "" {
		if maxLineBytes, err := strconv.Atoi(v); err == nil && maxLineBytes > 0 {
			appConfig.Logging.MaxLineBytes = maxLineBytes
		}
	}

	if v := strings.TrimSpace(os.Getenv("LOG_CLEANUP_ENABLED")); v != "" {
		appConfig.Logging.Cleanup.Enabled = getBoolEnv("LOG_CLEANUP_ENABLED", appConfig.Logging.Cleanup.Enabled)
	}

	if v := strings.TrimSpace(os.Getenv("LOG_MAX_RETENTION_DAYS")); v != "" {
		if days, err := strconv.Atoi(v); err == nil && days > 0 {
			appConfig.Logging.Cleanup.MaxRetentionDays = days
		}
	}

	if v := strings.TrimSpace(os.Getenv("LOG_CLEANUP_FREQUENCY_MINUTES")); v != "" {
		if minutes, err := strconv.Atoi(v); err == nil && minutes > 0 {
			appConfig.Logging.Cleanup.FrequencyMinutes = minutes
		}
	}
}

func applyApiLogConfigFromEnv(appConfig *config.AppConfigModel) {
	if v := strings.TrimSpace(os.Getenv("API_LOG_CLEANUP_ENABLED")); v != "" {
		appConfig.ApiLog.Cleanup.Enabled = getBoolEnv("API_LOG_CLEANUP_ENABLED", appConfig.ApiLog.Cleanup.Enabled)
	}

	if v := strings.TrimSpace(os.Getenv("API_LOG_MAX_RETENTION_DAYS")); v != "" {
		if days, err := strconv.Atoi(v); err == nil && days > 0 {
			appConfig.ApiLog.Cleanup.MaxRetentionDays = days
		}
	}

	if v := strings.TrimSpace(os.Getenv("API_LOG_CLEANUP_FREQUENCY_MINUTES")); v != "" {
		if minutes, err := strconv.Atoi(v); err == nil && minutes > 0 {
			appConfig.ApiLog.Cleanup.FrequencyMinutes = minutes
		}
	}
}

func applyTelemetryConfigFromEnv(appConfig *config.AppConfigModel) {
	if v := strings.TrimSpace(os.Getenv("TELEMETRY_ENABLED")); v != "" {
		appConfig.Telemetry.Enabled = getBoolEnv("TELEMETRY_ENABLED", appConfig.Telemetry.Enabled)
	}

	if v := strings.TrimSpace(os.Getenv("PROMETHEUS_ENABLED")); v != "" {
		appConfig.Telemetry.Prometheus.Enabled = getBoolEnv("PROMETHEUS_ENABLED", appConfig.Telemetry.Prometheus.Enabled)
	}

	if v := strings.TrimSpace(os.Getenv("PROMETHEUS_METRICS_PATH")); v != "" {
		appConfig.Telemetry.Prometheus.MetricsPath = v
	}

	if v := strings.TrimSpace(os.Getenv("PROMETHEUS_API_DURATION_THRESHOLD_MS")); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms >= 0 {
			appConfig.Telemetry.Prometheus.ApiDurationThresholdMs = ms
		}
	}
}

func applyRateLimitConfigFromEnv(appConfig *config.AppConfigModel) {
	if v := strings.TrimSpace(os.Getenv("RATE_LIMIT_ENABLED")); v != "" {
		appConfig.RateLimit.Enabled = getBoolEnv("RATE_LIMIT_ENABLED", appConfig.RateLimit.Enabled)
	}
}

func rateLimitMiddlewareConfig(appConfig *config.AppConfigModel) middlewares.RateLimitConfig {
	defaultWindow := time.Duration(appConfig.RateLimit.DefaultWindowSeconds) * time.Second
	if defaultWindow <= 0 {
		defaultWindow = time.Minute
	}
	endpointCacheTTL := time.Duration(appConfig.RateLimit.EndpointCacheTTLSeconds) * time.Second
	if endpointCacheTTL <= 0 {
		endpointCacheTTL = time.Duration(appConfig.Cache.TTLSeconds) * time.Second
	}
	if endpointCacheTTL <= 0 {
		endpointCacheTTL = 30 * time.Second
	}

	return middlewares.RateLimitConfig{
		Enabled:          appConfig.RateLimit.Enabled,
		EndpointCacheTTL: endpointCacheTTL,
		DevOnly:          rateLimitTierMiddlewareConfig(appConfig.RateLimit.DevOnly, defaultWindow),
		AuthOnly:         rateLimitTierMiddlewareConfig(appConfig.RateLimit.AuthOnly, defaultWindow),
		Public:           rateLimitTierMiddlewareConfig(appConfig.RateLimit.Public, defaultWindow),
	}
}

func authMiddlewareConfig(appName string, appConfig *config.AppConfigModel, cacheStore appcache.Store) middlewares.AuthConfig {
	issuer := strings.TrimSpace(appConfig.SSO.Issuer)
	if issuer == "" {
		issuer = appName
	}
	audience := strings.TrimSpace(appConfig.SSO.Audience)
	if audience == "" {
		audience = appName
	}
	sessionTTL := time.Duration(appConfig.SSO.SessionTTLSeconds) * time.Second
	if sessionTTL <= 0 {
		sessionTTL = 72 * time.Hour
	}

	return middlewares.AuthConfig{
		Secret:        appConfig.Jwt.Secret,
		Issuer:        issuer,
		Audience:      audience,
		AppCode:       appName,
		SessionCache:  cacheStore,
		SessionTTL:    sessionTTL,
		PolicyVersion: 1,
	}
}

func rateLimitTierMiddlewareConfig(tier config.RateLimitTierConfigModel, defaultWindow time.Duration) middlewares.RateLimitTierConfig {
	window := time.Duration(tier.WindowSeconds) * time.Second
	if window <= 0 {
		window = defaultWindow
	}
	return middlewares.RateLimitTierConfig{
		Enabled:  tier.Enabled,
		Requests: int64(tier.Requests),
		Window:   window,
	}
}

func buildRuntimeLogger(appName string, baseDir string, appConfig *config.AppConfigModel) (applog.Logger, error) {
	if strings.TrimSpace(appConfig.Logging.Path) == "" {
		appConfig.Logging.Path = filepath.Join(baseDir, "logs", appName+".log")
	}

	return applog.NewFileLogger(applog.Config{
		Enabled:      appConfig.Logging.Enabled,
		Path:         appConfig.Logging.Path,
		MaxLineBytes: appConfig.Logging.MaxLineBytes,
	})
}

func buildTelemetryRecorder(router *mux.Router, appName string, appConfig *config.AppConfigModel, logger applog.Logger) infraTelemetry.Recorder {
	if !appConfig.Telemetry.Enabled || !appConfig.Telemetry.Prometheus.Enabled {
		return infraTelemetry.NewNoopRecorder()
	}

	metricsPath := normalizeMetricsPath(appConfig.Telemetry.Prometheus.MetricsPath)
	thresholdMs := appConfig.Telemetry.Prometheus.ApiDurationThresholdMs
	recorder := promTelemetry.NewRecorder(promTelemetry.Config{
		SlowThresholdMs: thresholdMs,
	})
	router.Handle(metricsPath, recorder.Handler()).Methods("GET")
	if logger != nil {
		logger.Infof("telemetry", "prometheus enabled metricsPath=%s apiDurationThresholdMs=%d", metricsPath, thresholdMs)
	}
	return recorder
}

func normalizeMetricsPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/metrics"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.ReplaceAll(path, "//", "/")
}

func startRuntimeLogCleanupScheduler(runtimeScheduler *scheduler.Scheduler, appConfig *config.AppConfigModel, logger applog.Logger, runtimeLogService sharedServices.IRuntimeLogService) {
	if !appConfig.Logging.Cleanup.Enabled || appConfig.Logging.Cleanup.MaxRetentionDays <= 0 {
		return
	}

	frequencyMinutes := appConfig.Logging.Cleanup.FrequencyMinutes
	if frequencyMinutes <= 0 {
		frequencyMinutes = 60
	}

	maxRetentionDays := appConfig.Logging.Cleanup.MaxRetentionDays
	runtimeScheduler.StartPeriodic("runtime-log-cleanup", time.Duration(frequencyMinutes)*time.Minute, func(taskCtx context.Context) error {
		deleted, err := runtimeLogService.DeleteOlderThan(taskCtx, maxRetentionDays)
		if err != nil {
			return err
		}
		if deleted > 0 && logger != nil {
			logger.Infof("runtime-log-cleanup", "deleted=%d maxRetentionDays=%d", deleted, maxRetentionDays)
		}
		return nil
	})
}

func startApiLogCleanupScheduler(runtimeScheduler *scheduler.Scheduler, appConfig *config.AppConfigModel, logger applog.Logger, apiLogService sharedServices.IApiLogService) {
	if !appConfig.ApiLog.Cleanup.Enabled || appConfig.ApiLog.Cleanup.MaxRetentionDays <= 0 {
		return
	}

	frequencyMinutes := appConfig.ApiLog.Cleanup.FrequencyMinutes
	if frequencyMinutes <= 0 {
		frequencyMinutes = 60
	}

	maxRetentionDays := appConfig.ApiLog.Cleanup.MaxRetentionDays
	runtimeScheduler.StartPeriodic("api-log-cleanup", time.Duration(frequencyMinutes)*time.Minute, func(taskCtx context.Context) error {
		deleted, err := apiLogService.DeleteOlderThan(taskCtx, maxRetentionDays)
		if err != nil {
			return err
		}
		if deleted > 0 && logger != nil {
			logger.Infof("api-log-cleanup", "deleted=%d maxRetentionDays=%d", deleted, maxRetentionDays)
		}
		return nil
	})
}

func startFileStorageJobScheduler(runtimeScheduler *scheduler.Scheduler, appConfig *config.AppConfigModel, logger applog.Logger, fileStorageService sharedServices.IFileStorageService) {
	if !appConfig.Transaction.JobWorkerEnabled {
		return
	}

	frequencySeconds := appConfig.Transaction.JobWorkerFrequencySeconds
	if frequencySeconds <= 0 {
		frequencySeconds = 5
	}

	runtimeScheduler.StartPeriodic("file-storage-job-worker", time.Duration(frequencySeconds)*time.Second, func(taskCtx context.Context) error {
		recovered, err := fileStorageService.RecoverStaleUploadJobs(taskCtx)
		if err != nil {
			return err
		}
		processed, err := fileStorageService.ProcessUploadJobs(taskCtx, 10)
		if err != nil {
			return err
		}
		if logger != nil && (recovered > 0 || processed > 0) {
			logger.Infof("file-storage-job-worker", "recovered=%d processed=%d", recovered, processed)
		}
		return nil
	})
}

func startFileStorageCleanupScheduler(runtimeScheduler *scheduler.Scheduler, appConfig *config.AppConfigModel, logger applog.Logger, fileStorageService sharedServices.IFileStorageService) {
	if !appConfig.FileStorage.Cleanup.Enabled {
		return
	}

	frequencySeconds := appConfig.FileStorage.Cleanup.FrequencySeconds
	if frequencySeconds <= 0 {
		frequencySeconds = 60
	}
	batchSize := uint64(appConfig.FileStorage.Cleanup.BatchSize)
	if batchSize == 0 {
		batchSize = 100
	}

	runtimeScheduler.StartPeriodic("file-storage-expiry-cleanup", time.Duration(frequencySeconds)*time.Second, func(taskCtx context.Context) error {
		deleted, err := fileStorageService.SweepExpiredFiles(taskCtx, time.Now().UTC().Unix(), batchSize)
		if err != nil {
			return err
		}
		if logger != nil && deleted > 0 {
			logger.Infof("file-storage-expiry-cleanup", "deleted=%d", deleted)
		}
		return nil
	})
}

func buildCacheStore(appConfig *config.AppConfigModel) (appcache.Store, string, error) {
	provider := strings.TrimSpace(strings.ToLower(appConfig.Cache.Provider))
	if provider == "" {
		provider = "inmemory"
	}

	defaultTTL := time.Duration(appConfig.Cache.TTLSeconds) * time.Second
	if defaultTTL <= 0 {
		defaultTTL = 10 * time.Second
	}

	switch provider {
	case "default", "inmemory", "memory":
		return appcache.NewMemoryStore(defaultTTL, defaultTTL), "inmemory", nil
	case "redis":
		connectTimeout := time.Duration(appConfig.Cache.Redis.ConnectTimeoutMs) * time.Millisecond
		if connectTimeout <= 0 {
			connectTimeout = 2 * time.Second
		}

		opTimeout := time.Duration(appConfig.Cache.Redis.OperationTimeoutMs) * time.Millisecond
		if opTimeout <= 0 {
			opTimeout = 2 * time.Second
		}

		store := appcache.NewRedisStore(appcache.RedisConfig{
			Address:          appConfig.Cache.Redis.Address,
			Password:         appConfig.Cache.Redis.Password,
			DB:               appConfig.Cache.Redis.DB,
			UseTLS:           appConfig.Cache.Redis.UseTLS,
			KeyPrefix:        appConfig.Cache.KeyPrefix,
			ConnectTimeout:   connectTimeout,
			OperationTimeout: opTimeout,
		})
		return store, "redis", nil
	default:
		return nil, "", fmt.Errorf("unsupported cache provider %q", provider)
	}
}

func buildTransactionLocker(appName string, appConfig *config.AppConfigModel, recorder infraTelemetry.Recorder) (coordination.Locker, string, error) {
	provider := strings.TrimSpace(strings.ToLower(appConfig.Transaction.LockProvider))
	if provider == "" {
		provider = strings.TrimSpace(strings.ToLower(appConfig.Cache.Provider))
	}
	if provider == "" || provider == "default" {
		provider = "inmemory"
	}

	cfg := coordination.Config{
		AppName:        appName,
		Provider:       provider,
		KeyPrefix:      appConfig.Cache.KeyPrefix,
		WaitTimeout:    transactionLockWaitTimeout(appConfig),
		LeaseTTL:       transactionLockLease(appConfig),
		StuckTimeout:   transactionStuckTimeout(appConfig),
		RedisAddress:   appConfig.Cache.Redis.Address,
		RedisPassword:  appConfig.Cache.Redis.Password,
		RedisDB:        appConfig.Cache.Redis.DB,
		RedisUseTLS:    appConfig.Cache.Redis.UseTLS,
		ConnectTimeout: time.Duration(appConfig.Cache.Redis.ConnectTimeoutMs) * time.Millisecond,
		CommandTimeout: time.Duration(appConfig.Cache.Redis.OperationTimeoutMs) * time.Millisecond,
	}

	switch provider {
	case "inmemory", "memory":
		cfg.Provider = "memory"
		return coordination.NewMemoryLocker(cfg, recorder), "memory", nil
	case "redis":
		return coordination.NewRedisLocker(cfg, recorder), "redis", nil
	default:
		return nil, "", fmt.Errorf("unsupported transaction lock provider %q", provider)
	}
}

func transactionLockWaitTimeout(appConfig *config.AppConfigModel) time.Duration {
	return durationFromMs(appConfig.Transaction.LockWaitTimeoutMs, 30*time.Second)
}

func transactionLockLease(appConfig *config.AppConfigModel) time.Duration {
	return durationFromMs(appConfig.Transaction.LockLeaseMs, 10*time.Second)
}

func transactionOperationTimeout(appConfig *config.AppConfigModel) time.Duration {
	return durationFromMs(appConfig.Transaction.OperationTimeoutMs, 30*time.Second)
}

func transactionStuckTimeout(appConfig *config.AppConfigModel) time.Duration {
	return durationFromMs(appConfig.Transaction.StuckTimeoutMs, 30*time.Second)
}

func transactionMaxAttempts(appConfig *config.AppConfigModel) int64 {
	if appConfig.Transaction.MaxAttempts <= 0 {
		return 3
	}
	return int64(appConfig.Transaction.MaxAttempts)
}

func durationFromMs(value int, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return time.Duration(value) * time.Millisecond
}

func newDbCrud(cfg dbsql.DbConfigModel) (dbsql.IDbCrud, error) {
	engine := normalizeDbEngine(cfg.Engine)

	switch engine {
	case "postgres":
		return postgres.NewDbCrud(cfg)
	case "mariadb":
		return mariadb.NewDbCrud(cfg)
	case "sqlite":
		return sqlite.NewDbCrud(cfg)
	default:
		return nil, fmt.Errorf("unsupported db engine %q", engine)
	}
}

func normalizeDbEngine(engine string) string {
	value := strings.TrimSpace(strings.ToLower(engine))
	if value == "" {
		return "postgres"
	}
	return value
}

func applyServerConfigFromEnv(appConfig *config.AppConfigModel) error {
	if v := strings.TrimSpace(os.Getenv("SERVER_ADDR")); v != "" {
		host, portStr, err := net.SplitHostPort(v)
		if err != nil {
			return fmt.Errorf("invalid SERVER_ADDR %q: %w", v, err)
		}

		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 {
			return fmt.Errorf("invalid SERVER_ADDR port in %q", v)
		}

		if strings.TrimSpace(host) == "" {
			appConfig.Server.Hostnames = []string{"*"}
		} else {
			appConfig.Server.Hostnames = []string{host}
		}
		appConfig.Server.Ports = []int{port}
	}

	if v := strings.TrimSpace(os.Getenv("SERVER_HOSTNAMES")); v != "" {
		rawHosts := strings.Split(v, ",")
		hosts := make([]string, 0, len(rawHosts))
		for _, raw := range rawHosts {
			h := strings.TrimSpace(raw)
			if h == "" {
				continue
			}
			hosts = append(hosts, h)
		}

		if len(hosts) == 0 {
			return errors.New("SERVER_HOSTNAMES provided but empty")
		}

		appConfig.Server.Hostnames = hosts
	}

	if v := strings.TrimSpace(os.Getenv("SERVER_PORTS")); v != "" {
		ports, err := parsePortEnv("SERVER_PORTS", v)
		if err != nil {
			return err
		}
		appConfig.Server.Ports = ports
	}

	if v := strings.TrimSpace(os.Getenv("SERVER_TLS_PORTS")); v != "" {
		ports, err := parsePortEnv("SERVER_TLS_PORTS", v)
		if err != nil {
			return err
		}
		appConfig.Server.TLSPorts = ports
	}

	if v := strings.TrimSpace(os.Getenv("SERVER_NON_TLS_PORTS")); v != "" {
		ports, err := parsePortEnv("SERVER_NON_TLS_PORTS", v)
		if err != nil {
			return err
		}
		appConfig.Server.NonTLSPorts = ports
	}

	if v := strings.TrimSpace(os.Getenv("SERVER_ENABLE_TLS")); v != "" {
		value := getBoolEnv("SERVER_ENABLE_TLS", true)
		appConfig.Server.EnableTLS = &value
	}

	if v := strings.TrimSpace(os.Getenv("SERVER_ENABLE_NON_TLS")); v != "" {
		value := getBoolEnv("SERVER_ENABLE_NON_TLS", false)
		appConfig.Server.EnableNonTLS = &value
	}

	return nil
}

func parsePortEnv(key string, value string) ([]int, error) {
	rawPorts := strings.Split(value, ",")
	ports := make([]int, 0, len(rawPorts))
	for _, raw := range rawPorts {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}

		port, err := strconv.Atoi(p)
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("invalid %s value %q", key, p)
		}
		ports = append(ports, port)
	}

	if len(ports) == 0 {
		return nil, fmt.Errorf("%s provided but empty", key)
	}

	return ports, nil
}

func runServer(srv *http.Server, appConfig *config.AppConfigModel, useTLS bool) error {
	if useTLS {
		if appConfig.Tls.CertPath == "" || appConfig.Tls.KeyPath == "" {
			return errors.New("tls cert or key path is empty")
		}

		err := srv.ListenAndServeTLS(appConfig.Tls.CertPath, appConfig.Tls.KeyPath)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}

	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func serverTimeout(value *int, defaultSeconds int) time.Duration {
	if value == nil {
		return time.Duration(defaultSeconds) * time.Second
	}
	if *value <= 0 {
		return 0
	}
	return time.Duration(*value) * time.Second
}

func buildListenerSpecs(appConfig *config.AppConfigModel) ([]listenerSpec, error) {
	hostnames := normalizeHostnames(appConfig.Server.Hostnames)
	tlsPorts, nonTLSPorts, err := resolveListenerPorts(appConfig)
	if err != nil {
		return nil, err
	}

	specs := make([]listenerSpec, 0, len(hostnames)*(len(tlsPorts)+len(nonTLSPorts)))
	seen := make(map[string]struct{})

	for _, host := range hostnames {
		for _, port := range tlsPorts {
			addr := buildAddr(host, port)
			k := "tls|" + addr
			if _, ok := seen[k]; !ok {
				specs = append(specs, listenerSpec{Addr: addr, UseTLS: true})
				seen[k] = struct{}{}
			}
		}

		for _, port := range nonTLSPorts {
			addr := buildAddr(host, port)
			k := "notls|" + addr
			if _, ok := seen[k]; !ok {
				specs = append(specs, listenerSpec{Addr: addr, UseTLS: false})
				seen[k] = struct{}{}
			}
		}
	}

	return specs, nil
}

func resolveListenerPorts(appConfig *config.AppConfigModel) ([]int, []int, error) {
	if len(appConfig.Server.TLSPorts) > 0 || len(appConfig.Server.NonTLSPorts) > 0 {
		tlsPorts, err := normalizeOptionalPorts("server.tlsPorts", appConfig.Server.TLSPorts)
		if err != nil {
			return nil, nil, err
		}
		nonTLSPorts, err := normalizeOptionalPorts("server.nonTlsPorts", appConfig.Server.NonTLSPorts)
		if err != nil {
			return nil, nil, err
		}
		if len(tlsPorts) == 0 && len(nonTLSPorts) == 0 {
			return nil, nil, errors.New("server.tlsPorts or server.nonTlsPorts must contain at least one port")
		}
		if overlap := overlappingPort(tlsPorts, nonTLSPorts); overlap != 0 {
			return nil, nil, fmt.Errorf("server.tlsPorts and server.nonTlsPorts cannot both include port %d", overlap)
		}
		return tlsPorts, nonTLSPorts, nil
	}

	ports, err := normalizeRequiredPorts("server.ports", appConfig.Server.Ports)
	if err != nil {
		return nil, nil, err
	}

	enableTLS, enableNonTLS := resolveServerModes(appConfig)
	if !enableTLS && !enableNonTLS {
		return nil, nil, errors.New("at least one server mode must be enabled: tls or non-tls")
	}
	if enableTLS && enableNonTLS {
		return nil, nil, errors.New("server.enableTls and server.enableNonTls cannot both be true with the same legacy server.ports list; use server.tlsPorts and server.nonTlsPorts instead")
	}
	if enableTLS {
		return ports, nil, nil
	}
	return nil, ports, nil
}

func normalizeHostnames(hosts []string) []string {
	if len(hosts) == 0 {
		return []string{""}
	}

	normalized := make([]string, 0, len(hosts))
	for _, raw := range hosts {
		h := strings.TrimSpace(raw)
		if h == "" || h == "*" {
			h = ""
		}
		normalized = append(normalized, h)
	}

	if len(normalized) == 0 {
		return []string{""}
	}

	return normalized
}

func normalizeRequiredPorts(name string, ports []int) ([]int, error) {
	if len(ports) == 0 {
		return nil, fmt.Errorf("%s is required and must contain at least one port", name)
	}

	normalized, err := normalizeOptionalPorts(name, ports)
	if err != nil {
		return nil, err
	}

	if len(normalized) == 0 {
		return nil, fmt.Errorf("%s is required and must contain at least one valid port", name)
	}

	return normalized, nil
}

func normalizeOptionalPorts(name string, ports []int) ([]int, error) {
	seen := make(map[int]struct{})
	normalized := make([]int, 0, len(ports))
	for _, port := range ports {
		if port <= 0 || port > 65535 {
			return nil, fmt.Errorf("invalid %s value: %d", name, port)
		}
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		normalized = append(normalized, port)
	}

	return normalized, nil
}

func overlappingPort(left []int, right []int) int {
	seen := make(map[int]struct{}, len(left))
	for _, port := range left {
		seen[port] = struct{}{}
	}
	for _, port := range right {
		if _, ok := seen[port]; ok {
			return port
		}
	}
	return 0
}

func resolveServerModes(appConfig *config.AppConfigModel) (bool, bool) {
	enableTLS := true
	enableNonTLS := false

	if appConfig.Server.EnableTLS != nil {
		enableTLS = *appConfig.Server.EnableTLS
	}

	if appConfig.Server.EnableNonTLS != nil {
		enableNonTLS = *appConfig.Server.EnableNonTLS
	}

	legacyTLS := strings.TrimSpace(os.Getenv("SERVER_USE_TLS"))
	if legacyTLS != "" {
		enableTLS = getBoolEnv("SERVER_USE_TLS", enableTLS)
		enableNonTLS = false
	}

	if strings.TrimSpace(os.Getenv("SERVER_ENABLE_TLS")) != "" {
		enableTLS = getBoolEnv("SERVER_ENABLE_TLS", enableTLS)
	}

	if strings.TrimSpace(os.Getenv("SERVER_ENABLE_NON_TLS")) != "" {
		enableNonTLS = getBoolEnv("SERVER_ENABLE_NON_TLS", enableNonTLS)
	}

	return enableTLS, enableNonTLS
}

func buildAddr(host string, port int) string {
	portStr := strconv.Itoa(port)
	if strings.TrimSpace(host) == "" {
		return ":" + portStr
	}
	return net.JoinHostPort(host, portStr)
}

func getBoolEnv(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return fallback
	}

	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
