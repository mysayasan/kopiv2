package controllers

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/login"
)

// LoginApi struct
type loginApi struct {
	auth       middlewares.AuthMiddleware
	googleAuth *login.GoogleLogin
	githubAuth *login.GithubLogin
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

	handler := &loginApi{
		auth:       auth,
		googleAuth: googleLogin,
		githubAuth: githubLogin,
	}

	group := router.Group("login")

	group.Get("/google_login", handler.google_login).Name("google_login")
	group.Get("/google_callback", handler.google_callback).Name("google_callback")
	group.Get("/github_login", handler.github_login).Name("github_login")
	group.Get("/github_callback", handler.github_callback).Name("github_callback")
}

func (m *loginApi) google_login(c *fiber.Ctx) error {
	return m.googleAuth.Login(c)
}

func (m *loginApi) google_callback(c *fiber.Ctx) error {
	userJson, err := m.googleAuth.Callback(c)
	if err != nil {
		return c.SendString(err.Error())
	}

	// Create the Claims
	claims := &middlewares.JwtCustomClaimsModel{
		Name:          userJson.Name,
		Email:         userJson.Email,
		VerifiedEmail: true,
		FamilyName:    userJson.FamilyName,
		Picture:       userJson.Picture,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 72)),
		},
	}

	t, err := m.auth.JwtToken(*claims)
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(fiber.Map{"token": t})
}

func (m *loginApi) github_login(c *fiber.Ctx) error {
	return m.githubAuth.Login(c)
}

func (m *loginApi) github_callback(c *fiber.Ctx) error {
	return m.githubAuth.Callback(c)
}
