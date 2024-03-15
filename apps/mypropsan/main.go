package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/mysayasan/kopiv2/apps/mypropsan/controllers"
	"github.com/mysayasan/kopiv2/infra/login"
	"github.com/mysayasan/kopiv2/infra/middlewares"
)

func main() {
	app := fiber.New()
	// app.Use(helmet.New(helmet.Config{
	// 	ContentTypeNosniff: "nosniff",
	// 	XSSProtection:      "0",
	// }))

	login.GoogleConfig()
	login.GithubConfig()

	app.Use(cors.New(cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			return os.Getenv("ENVIRONMENT") == "dev"
		},
		AllowOrigins:  "https://mypropsan.com, https://mypropsan.com.my",
		AllowHeaders:  "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization",
		ExposeHeaders: "X-Cursor",
		AllowMethods:  "POST, GET, OPTIONS, PUT, DELETE",
	}))

	greetMidware := middlewares.NewGreet()

	// http middleware -> fiber.Handler
	app.Use(adaptor.HTTPMiddleware(greetMidware.Greet))

	// start auth middleware
	authMidware := middlewares.NewAuth(os.Getenv("SECRET"))
	googleLogin := login.NewGoogleLogin(*authMidware)
	githubLogin := login.NewGithubLogin(*authMidware)

	app.Get("/google_login", googleLogin.Login)
	app.Get("/google_callback", googleLogin.Callback)
	app.Get("/github_login", githubLogin.Login)
	app.Get("/github_callback", githubLogin.Callback)

	// Restricted Routes

	controllers.NewAdminApi(app, *authMidware)

	app.Listen(":3000")

}
