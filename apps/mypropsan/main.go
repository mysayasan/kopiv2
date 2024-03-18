package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/joho/godotenv"
	"github.com/mysayasan/kopiv2/apps/mypropsan/controllers"
	"github.com/mysayasan/kopiv2/apps/mypropsan/repos"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/db/postgres"
	"github.com/mysayasan/kopiv2/infra/middlewares"
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

	app := fiber.New()
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

	homeRepo := repos.NewHomeRepo(postgresDb)

	// start auth middleware
	auth := middlewares.NewAuth(appConfig.Jwt.Secret)
	api := app.Group("api")

	// Login routes
	controllers.NewLoginApi(api, appConfig.Login.Google, *auth)

	// Home routes
	controllers.NewHomeApi(api, *auth, homeRepo)

	// Restricted Routes
	controllers.NewAdminApi(api, *auth)

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