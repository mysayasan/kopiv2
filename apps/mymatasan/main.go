package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"

	// "github.com/gofiber/fileStorage/sqlite3"

	"github.com/joho/godotenv"
	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	"github.com/mysayasan/kopiv2/apps/mymatasan/models"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	sharedApis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedServices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	ffmpegCam "github.com/mysayasan/kopiv2/infra/camera/ffmpeg"
	"github.com/mysayasan/kopiv2/infra/config"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
	goCache "github.com/patrickmn/go-cache"
)

// spaHandler implements the http.Handler interface, so we can use it
// to respond to HTTP requests. The path to the static directory and
// path to the index file within that static directory are used to
// serve the SPA in the given static directory.
type spaHandler struct {
	staticPath string
	indexPath  string
}

// ServeHTTP inspects the URL path to locate a file within the static dir
// on the SPA handler. If a file is found, it will be served. If not, the
// file located at the index path on the SPA handler will be served. This
// is suitable behavior for serving an SPA (single page application).
func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Join internally call path.Clean to prevent directory traversal
	path := filepath.Join(h.staticPath, r.URL.Path)

	// check whether a file exists or is a directory at the given path
	fi, err := os.Stat(path)
	if os.IsNotExist(err) || fi.IsDir() {
		// file does not exist or path is a directory, serve index.html
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	}

	if err != nil {
		// if we got an error (that wasn't that the file doesn't exist) stating the
		// file, return a 500 internal server error and stop
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// otherwise, use http.FileServer to serve the static file
	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	// A very simple health check.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// In the future we could report back on the status of our DB, or our cache
	// (e.g. Redis) by performing a simple PING, and include them in the response.
	io.WriteString(w, `{"alive": true}`)
}

func main() {
	godotenv.Load(".env")
	env := os.Getenv("ENVIRONMENT")

	var appConfig *config.AppConfigModel

	if env == "dev" {
		appConfig, _ = config.LoadAppConfiguration("./config.dev.json")
	} else {
		appConfig, _ = config.LoadAppConfiguration("./config.json")
	}

	if appConfig == nil {
		panic("config file not found")
	}

	// app := fiber.New()
	router := mux.NewRouter()

	// app.Use(cors.New(cors.Config{
	// 	AllowOriginsFunc: func(origin string) bool {
	// 		return os.Getenv("ENVIRONMENT") == "dev"
	// 	},
	// 	AllowOrigins:  appConfig.AllowOrigin,
	// 	AllowHeaders:  "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization",
	// 	ExposeHeaders: "X-Cursor",
	// 	AllowMethods:  "POST, GET, OPTIONS, PUT, DELETE",
	// }))

	fmt.Printf("Running condition = %s", os.Getenv("ENVIRONMENT"))

	// Implement middleware
	greetMidware := middlewares.NewGreet()
	router.Use(greetMidware.GreetHandler)
	corsMidware := middlewares.NewCors()
	router.Use(corsMidware.CorsHandler)

	// Implement healthcheck
	router.HandleFunc("/health", HealthCheckHandler)

	// Create db instance
	postgresDb, err := postgres.NewDbCrud(appConfig.Db)
	if err != nil {
		panic("error connecting to db")
	}

	// Create cache
	memCache := goCache.New(10*time.Second, 10*time.Second)

	// Create Repo
	userLoginRepo := dbsql.NewGenericRepo[entities.UserLogin](postgresDb)
	userGroupRepo := dbsql.NewGenericRepo[entities.UserGroup](postgresDb)
	userRoleRepo := dbsql.NewGenericRepo[entities.UserRole](postgresDb)
	apiLogRepo := dbsql.NewGenericRepo[entities.ApiLog](postgresDb)
	apiEpRepo := dbsql.NewGenericRepo[entities.ApiEndpoint](postgresDb)
	apiEpRbacRepo := dbsql.NewGenericRepo[entities.ApiEndpointRbac](postgresDb)
	residentPropRepo := dbsql.NewGenericRepo[models.ResidentProp](postgresDb)
	fileStorRepo := dbsql.NewGenericRepo[entities.FileStorage](postgresDb)

	// Shared services Modules
	userLoginService := sharedServices.NewUserLoginService(userLoginRepo, memCache)
	userGroupService := sharedServices.NewUserGroupService(userGroupRepo, memCache)
	userRoleService := sharedServices.NewUserRoleService(userRoleRepo, memCache)
	apiLogService := sharedServices.NewApiLogService(apiLogRepo, memCache)
	apiEndpointService := sharedServices.NewApiEndpointService(apiEpRepo, memCache)
	apiEndpointRbacService := sharedServices.NewApiEndpointRbacService(apiEpRbacRepo, userLoginRepo, apiEpRepo, memCache)
	fileStorageService := sharedServices.NewFileStorageService(fileStorRepo, memCache)

	// App services Modules
	homeService := services.NewHomeService(residentPropRepo)

	// start rbac middleware
	rbac := middlewares.NewRbac(apiEndpointRbacService, memCache)

	// start auth middleware
	auth := middlewares.NewAuth(appConfig.Jwt.Secret)

	// Create api sub-router
	api := router.PathPrefix("/api").Subrouter()
	// api.Use(auth.Middleware)

	// Login module
	if appConfig.Login.Google != nil {
		sharedApis.NewLoginApi(api, appConfig.Login.Google, *auth, userLoginService)
	}
	// User Login Module
	sharedApis.NewUserLoginApi(api, *auth, *rbac, userLoginService)
	// User Group Module
	sharedApis.NewUserGroupApi(api, *auth, *rbac, userGroupService)
	// User Role Module
	sharedApis.NewUserRoleApi(api, *auth, *rbac, userRoleService)
	// Api Log module
	sharedApis.NewApiLogApi(api, *auth, *rbac, apiLogService)
	// Api Endpoint module
	sharedApis.NewApiEndpointApi(api, *auth, *rbac, apiEndpointService)
	// Api Endpoint RBAC module
	sharedApis.NewApiEndpointRbacApi(api, *auth, *rbac, apiEndpointRbacService)
	// FileStorage Api
	sharedApis.NewFileStorageApi(api, *auth, *rbac, fileStorageService, appConfig.FileStorage.Path)
	// Admin Api
	apis.NewAdminApi(api, *auth, *rbac)
	//Home Api
	apis.NewHomeApi(api, *auth, *rbac, homeService)

	newCam := ffmpegCam.NewNetCam("rtsp://admin:Aziandi220%40@192.168.1.148:554/cam/realmonitor?channel=1&subtype=0&unicast=true&proto=Onvif")
	camService := services.NewCameraService(newCam)
	apis.NewCameraApi(api, *auth, *rbac, camService)

	// // Callback after log is written
	// app.Use(logger.New(logger.Config{
	// 	TimeFormat: time.RFC3339Nano,
	// 	TimeZone:   "Asia/Singapore",
	// 	Done: func(c *fiber.Ctx, logString []byte) {
	// 		if c.Response().StatusCode() != fiber.StatusOK {
	// 			// fmt.Println(string(logString))
	// 			apiLogModel := &entities.ApiLog{}
	// 			apiLogModel.StatsCode = c.Response().StatusCode()
	// 			apiLogModel.LogMsg = string(logString)
	// 			apiLogModel.ClientIpAddrV4 = r.Context().RemoteIP().String()
	// 			apiLogModel.RequestUrl = string(c.Request().URI().FullURI())
	// 			_, err := apiLogService.Create(r.Context(), *apiLogModel)
	// 			if err != nil {
	// 				log.Error(err.Error())
	// 			}
	// 		}
	// 	},
	// }))

	api.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// an example API handler
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	spa := spaHandler{staticPath: "static", indexPath: "index.html"}
	router.PathPrefix("/").Handler(spa)

	http.Handle("/", router)

	srv := &http.Server{
		Handler: router,
		Addr:    ":3000",
		// Good practice: enforce timeouts for servers you create!
		// WriteTimeout: 15 * time.Second,
		// ReadTimeout:  15 * time.Second,
	}

	panic(srv.ListenAndServeTLS(appConfig.Tls.CertPath, appConfig.Tls.KeyPath))

	// panic(http.ListenAndServe(":3333", nil))

	// // panic(app.Listen(":3000"))
	// panic(app.ListenTLS(":3000", appConfig.Tls.CertPath, appConfig.Tls.KeyPath))
}
