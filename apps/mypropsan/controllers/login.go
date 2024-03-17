package controllers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mysayasan/kopiv2/infra/login"
	"github.com/mysayasan/kopiv2/infra/middlewares"
)

// LoginApi struct
type loginApi struct {
	oAuth2Conf login.OAuth2ConfigModel
	auth       middlewares.AuthMiddleware
}

// Create LoginApi
func NewLoginApi(
	router fiber.Router,
	oAuth2Conf login.OAuth2ConfigModel,
	auth middlewares.AuthMiddleware) {

	login.GoogleConfig(oAuth2Conf)
	login.GithubConfig(oAuth2Conf)

	googleLogin := login.NewGoogleLogin(oAuth2Conf, auth)
	githubLogin := login.NewGithubLogin(oAuth2Conf, auth)

	group := router.Group("login")

	group.Get("/google_login", googleLogin.Login).Name("google_login")
	group.Get("/google_callback", googleLogin.Callback).Name("google_callback")
	group.Get("/github_login", githubLogin.Login).Name("github_login")
	group.Get("/github_callback", githubLogin.Callback).Name("github_login")
}
