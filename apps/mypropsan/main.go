package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"

	// "github.com/gofiber/fileStorage/sqlite3"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"
	"github.com/mysayasan/kopiv2/apps/mypropsan/apis"
	"github.com/mysayasan/kopiv2/apps/mypropsan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	sharedApis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedServices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

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
		log.Fatal("config file not found")
	}

	// fileStorage := sqlite3.New()

	app := fiber.New()
	// Recover from panic
	app.Use(recover.New())
	app.Get("/panic", func(c *fiber.Ctx) error {
		panic("I'm an error")
	})

	// // Limiter
	// app.Use(limiter.New(limiter.Config{
	// 	Max:               30,
	// 	Expiration:        1 * time.Second,
	// 	LimiterMiddleware: limiter.SlidingWindow{},
	// }))

	// app.Use(helmet.New(helmet.Config{
	// 	ContentTypeNosniff: "nosniff",
	// 	XSSProtection:      "0",
	// }))

	app.Use(cors.New(cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			return os.Getenv("ENVIRONMENT") == "dev"
		},
		AllowOrigins:  appConfig.AllowOrigin,
		AllowHeaders:  "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization",
		ExposeHeaders: "X-Cursor",
		AllowMethods:  "POST, GET, OPTIONS, PUT, DELETE",
	}))

	log.Info(fmt.Sprintf("Running condition = %s", os.Getenv("ENVIRONMENT")))

	// Implement middleware
	greetMidware := middlewares.NewGreet()
	app.Use(greetMidware.Greet)

	// Create db instance
	postgresDb, err := postgres.NewDbCrud(appConfig.Db)
	if err != nil {
		log.Fatal("error connecting to db")
	}

	// start auth middleware
	auth := middlewares.NewAuth(appConfig.Jwt.Secret)
	api := app.Group("api")
	api.Use(func(c *fiber.Ctx) error {
		return c.Next()
	})

	// Page Modules
	userLoginService := sharedServices.NewUserLoginService(postgresDb)
	userGroupService := sharedServices.NewUserGroupService(postgresDb)
	userRoleService := sharedServices.NewUserRoleService(postgresDb)
	apiLogService := sharedServices.NewApiLogService(postgresDb)
	apiEndpointService := sharedServices.NewApiEndpointService(postgresDb)
	apiEndpointRbacService := sharedServices.NewApiEndpointRbacService(postgresDb)
	homeService := services.NewHomeService(postgresDb)
	fileStorageService := services.NewFileStorageService(postgresDb)

	// start rbac middleware
	rbac := middlewares.NewRbac(apiEndpointRbacService)

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
	// Admin Api
	apis.NewAdminApi(api, *auth, *rbac)
	//Home Api
	apis.NewHomeApi(api, *auth, *rbac, homeService)
	// FileStorage Api
	apis.NewFileStorageApi(api, *auth, *rbac, fileStorageService, appConfig.FileStorage.Path)

	// Callback after log is written
	app.Use(logger.New(logger.Config{
		TimeFormat: time.RFC3339Nano,
		TimeZone:   "Asia/Singapore",
		Done: func(c *fiber.Ctx, logString []byte) {
			if c.Response().StatusCode() != fiber.StatusOK {
				// log.Info(string(logString))
				apiLogModel := &entities.ApiLog{}
				apiLogModel.StatsCode = c.Response().StatusCode()
				apiLogModel.LogMsg = string(logString)
				apiLogModel.ClientIpAddrV4 = c.Context().RemoteIP().String()
				apiLogModel.RequestUrl = string(c.Request().URI().FullURI())
				_, err := apiLogService.Create(c.Context(), *apiLogModel)
				if err != nil {
					log.Error(err.Error())
				}
			}
		},
	}))

	// Get api routes
	api.Get("/routes", auth.JwtHandler(), func(c *fiber.Ctx) error {
		data, _ := json.Marshal(app.GetRoutes(true))
		return c.JSON(string(data))
	}).Name("routes")

	// Serve static file
	app.Static("/", "./static")

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./static/index.html")
	}).Name("index")

	// log.Fatal(app.Listen(":3000"))
	log.Fatal(app.ListenTLS(":3000", appConfig.Tls.CertPath, appConfig.Tls.KeyPath))

}
