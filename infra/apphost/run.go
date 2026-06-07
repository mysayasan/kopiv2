package apphost

import (
	"context"
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	sharedEntities "github.com/mysayasan/kopiv2/domain/entities"
	sharedApis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedServices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	appcache "github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/db/sql/mariadb"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
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

func readinessCheckHandler(db dbsql.IDbCrud, cacheStore appcache.Store) http.HandlerFunc {
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

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "db": "up", "cache": "up"})
	}
}

// Run starts a selected app module using shared runtime wiring.
func Run(app App) error {
	godotenv.Load(".env")

	baseDir := filepath.Clean(app.BaseDir())
	appConfig, err := loadConfig(baseDir)
	if err != nil {
		return err
	}

	if err := applySensitiveConfig(appConfig); err != nil {
		return err
	}
	applyDbConfigFromEnv(appConfig)
	applyCacheConfigFromEnv(appConfig)
	applyLoggingConfigFromEnv(appConfig)
	applyApiLogConfigFromEnv(appConfig)
	applyTelemetryConfigFromEnv(appConfig)
	if err := applyServerConfigFromEnv(appConfig); err != nil {
		return err
	}
	normalizePathConfig(baseDir, appConfig)

	runtimeLogger, err := buildRuntimeLogger(app.Name(), baseDir, appConfig)
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

	router.HandleFunc("/ready", readinessCheckHandler(dbCrud, cacheStore)).Methods("GET")

	userLoginRepo := dbsql.NewGenericRepo[sharedEntities.UserLogin](dbCrud)
	userGroupRepo := dbsql.NewGenericRepo[sharedEntities.UserGroup](dbCrud)
	userRoleRepo := dbsql.NewGenericRepo[sharedEntities.UserRole](dbCrud)
	apiLogRepo := dbsql.NewGenericRepo[sharedEntities.ApiLog](dbCrud)
	apiEpRepo := dbsql.NewGenericRepo[sharedEntities.ApiEndpoint](dbCrud)
	apiEpRbacRepo := dbsql.NewGenericRepo[sharedEntities.ApiEndpointRbac](dbCrud)
	fileStorRepo := dbsql.NewGenericRepo[sharedEntities.FileStorage](dbCrud)

	userLoginService := sharedServices.NewUserLoginService(userLoginRepo, cacheStore)
	userGroupService := sharedServices.NewUserGroupService(userGroupRepo, cacheStore)
	userRoleService := sharedServices.NewUserRoleService(userRoleRepo, cacheStore)
	apiLogService := sharedServices.NewApiLogService(apiLogRepo, cacheStore)
	apiEndpointService := sharedServices.NewApiEndpointService(apiEpRepo, cacheStore)
	apiEndpointRbacService := sharedServices.NewApiEndpointRbacService(apiEpRbacRepo, userLoginRepo, apiEpRepo, cacheStore)
	fileStorageService := sharedServices.NewFileStorageService(fileStorRepo, cacheStore)
	cacheService := sharedServices.NewCacheService(cacheStore)
	runtimeLogService := sharedServices.NewRuntimeLogService(runtimeLogger)
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	runtimeScheduler := scheduler.New(schedulerCtx, runtimeLogger)
	startRuntimeLogCleanupScheduler(runtimeScheduler, appConfig, runtimeLogger, runtimeLogService)
	startApiLogCleanupScheduler(runtimeScheduler, appConfig, runtimeLogger, apiLogService)

	rbacCacheTTL := time.Duration(appConfig.Cache.TTLSeconds) * time.Second
	rbac := middlewares.NewRbac(apiEndpointRbacService, cacheStore, rbacCacheTTL)
	auth := middlewares.NewAuth(appConfig.Jwt.Secret)
	versionManifest, err := versioning.LoadDefault()
	if err != nil {
		return fmt.Errorf("error loading version manifest: %w", err)
	}
	telemetryRecorder := buildTelemetryRecorder(router, app.Name(), appConfig, runtimeLogger)

	api := router.PathPrefix("/api").Subrouter()
	apiActivityLogMidware := middlewares.NewApiActivityLog(
		apiLogService,
		auth,
		runtimeLogger,
		middlewares.WithApiActivityAppName(app.Name()),
		middlewares.WithApiActivityTelemetry(telemetryRecorder),
	)
	api.Use(apiActivityLogMidware.Middleware)

	sharedApis.NewVersionApi(api, app.Name(), versionManifest)
	sharedApis.NewLoginApi(api, appConfig.Login, *auth, userLoginService)
	sharedApis.NewUserLoginApi(api, *auth, *rbac, userLoginService)
	sharedApis.NewUserGroupApi(api, *auth, *rbac, userGroupService)
	sharedApis.NewUserRoleApi(api, *auth, *rbac, userRoleService)
	sharedApis.NewApiLogApi(api, *auth, *rbac, apiLogService)
	sharedApis.NewApiEndpointApi(api, *auth, *rbac, apiEndpointService)
	sharedApis.NewApiEndpointRbacApi(api, *auth, *rbac, apiEndpointRbacService)
	sharedApis.NewFileStorageApi(api, *auth, *rbac, fileStorageService, appConfig.FileStorage.Path)
	sharedApis.NewCacheServiceApi(api, *auth, *rbac, cacheService, apiLogService)
	sharedApis.NewRuntimeLogApi(api, *auth, *rbac, runtimeLogService)

	shutdownHook, err := app.RegisterAppRoutes(api, Dependencies{
		Config:    appConfig,
		Db:        dbCrud,
		Cache:     cacheStore,
		Auth:      auth,
		Rbac:      rbac,
		Logger:    runtimeLogger,
		Scheduler: runtimeScheduler,
	})
	if err != nil {
		return err
	}

	api.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	var docProvider apidocs.Provider
	if provider, ok := app.(apidocs.Provider); ok {
		docProvider = provider
	}
	apidocs.Register(router, app.Name(), docProvider)

	router.PathPrefix("/").Handler(spaHandler{staticPath: filepath.Join(baseDir, "static"), indexPath: "index.html"})

	listeners, err := buildListenerSpecs(appConfig)
	if err != nil {
		return err
	}

	servers := make([]*http.Server, 0, len(listeners))
	errChan := make(chan error, len(listeners))

	for _, listener := range listeners {
		listener := listener
		srv := &http.Server{
			Handler:           router,
			Addr:              listener.Addr,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
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

	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		log.Println("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if shutdownHook != nil {
		if err := shutdownHook(shutdownCtx); err != nil {
			log.Printf("app shutdown warning: %v", err)
		}
	}

	for _, srv := range servers {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown warning on %s: %v", srv.Addr, err)
		}
	}

	return nil
}

func loadConfig(baseDir string) (*config.AppConfigModel, error) {
	configFile := "config.json"
	if os.Getenv("ENVIRONMENT") == "dev" {
		configFile = "config.dev.json"
	}

	appConfig, err := config.LoadAppConfiguration(filepath.Join(baseDir, configFile))
	if err != nil {
		return nil, err
	}
	if appConfig == nil {
		return nil, errors.New("config file not found")
	}

	return appConfig, nil
}

func normalizePathConfig(baseDir string, appConfig *config.AppConfigModel) {
	appConfig.FileStorage.Path = resolvePath(baseDir, appConfig.FileStorage.Path)
	appConfig.Logging.Path = resolvePath(baseDir, appConfig.Logging.Path)
	appConfig.Tls.CertPath = resolvePath(baseDir, appConfig.Tls.CertPath)
	appConfig.Tls.KeyPath = resolvePath(baseDir, appConfig.Tls.KeyPath)
}

func resolvePath(baseDir string, target string) string {
	if target == "" || filepath.IsAbs(target) {
		return target
	}
	return filepath.Clean(filepath.Join(baseDir, target))
}

func applySensitiveConfig(appConfig *config.AppConfigModel) error {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret != "" {
		appConfig.Jwt.Secret = jwtSecret
	}

	if appConfig.Jwt.Secret == "" {
		return errors.New("JWT secret is required")
	}

	if appConfig.Login != nil && appConfig.Login.Google != nil {
		googleClientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
		if googleClientSecret != "" {
			appConfig.Login.Google.ClientSecret = googleClientSecret
		}

		if appConfig.Login.Google.ClientSecret == "" {
			return errors.New("google client secret is required")
		}
	}
	if appConfig.Login != nil && appConfig.Login.GitHub != nil {
		githubClientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
		if githubClientSecret != "" {
			appConfig.Login.GitHub.ClientSecret = githubClientSecret
		}

		if appConfig.Login.GitHub.ClientSecret == "" {
			return errors.New("github client secret is required")
		}
	}

	return nil
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

func buildTelemetryRecorder(router *mux.Router, appName string, appConfig *config.AppConfigModel, logger applog.Logger) infraTelemetry.APIRecorder {
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

func newDbCrud(cfg dbsql.DbConfigModel) (dbsql.IDbCrud, error) {
	engine := normalizeDbEngine(cfg.Engine)

	switch engine {
	case "postgres":
		return postgres.NewDbCrud(cfg)
	case "mariadb":
		return mariadb.NewDbCrud(cfg)
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
