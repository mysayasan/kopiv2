package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"

	// "github.com/gofiber/fileStorage/sqlite3"
	"github.com/joho/godotenv"
	"github.com/mysayasan/kopiv2/apps/mypropsan/controllers"
	"github.com/mysayasan/kopiv2/apps/mypropsan/repos"
	"github.com/mysayasan/kopiv2/apps/mypropsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

func main() {
	godotenv.Load(".env")

	appConfig, err := config.LoadAppConfiguration("./config_prod.json")
	if err != nil {
		appConfig, err = config.LoadAppConfiguration("./config_dev.json")
		os.Setenv("ENVIRONMENT", "dev")
		if err != nil {
			log.Fatal("no config file found")
		}
	}

	// fileStorage := sqlite3.New()

	app := fiber.New()
	// Recover from panic
	app.Use(recover.New())
	app.Get("/panic", func(c *fiber.Ctx) error {
		panic("I'm an error")
	})

	// Limiter
	app.Use(limiter.New(limiter.Config{
		Max:               20,
		Expiration:        30 * time.Second,
		LimiterMiddleware: limiter.SlidingWindow{},
		// FileStorage:           fileStorage,
	}))

	// app.Use(helmet.New(helmet.Config{
	// 	ContentTypeNosniff: "nosniff",
	// 	XSSProtection:      "0",
	// }))

	app.Use(cors.New(cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			return os.Getenv("ENVIRONMENT") == "dev"
		},
		// AllowOrigins:  "https://mypropsan.com, https://mypropsan.com.my",
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

	// Repo modules
	userRepo := repos.NewUserRepo(postgresDb)
	residentPropRepo := repos.NewResidentPropRepo(postgresDb)
	fileStorageRepo := repos.NewFileStorageRepo(postgresDb)

	// Page Modules
	userService := services.NewUserService(userRepo)
	homeService := services.NewHomeService(residentPropRepo)
	fileStorageService := services.NewFileStorageService(fileStorageRepo)

	// Login module
	controllers.NewLoginApi(api, appConfig.Login.Google, *auth, userService)
	// Admin Api
	controllers.NewAdminApi(api, *auth)
	//Home Api
	controllers.NewHomeApi(api, *auth, homeService)
	// FileStorage Api
	controllers.NewFileStorageApi(api, *auth, fileStorageService, appConfig.FileStorage.Path)

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
