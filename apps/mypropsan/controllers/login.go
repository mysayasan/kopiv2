package controllers

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/login"
)

// LoginApi struct
type loginApi struct {
	auth        middlewares.AuthMiddleware
	userService services.IUserService
	googleAuth  *login.GoogleLogin
	githubAuth  *login.GithubLogin
}

// Create LoginApi
func NewLoginApi(
	router fiber.Router,
	oAuth2Conf login.OAuth2ConfigModel,
	auth middlewares.AuthMiddleware,
	userService services.IUserService) {

	login.GoogleConfig(oAuth2Conf)
	login.GithubConfig(oAuth2Conf)

	googleLogin := login.NewGoogleLogin(oAuth2Conf, auth)
	githubLogin := login.NewGithubLogin(oAuth2Conf, auth)

	handler := &loginApi{
		auth:        auth,
		userService: userService,
		googleAuth:  googleLogin,
		githubAuth:  githubLogin,
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
	userG, err := m.googleAuth.Callback(c)
	if err != nil {
		return c.SendString(err.Error())
	}

	user, err := m.userService.GetByEmail(c.Context(), userG.Email)
	if err != nil {
		log.Printf("%s", err.Error())
	}

	if user == nil {
		user = &entity.UserLoginEntity{
			Email:     userG.Email,
			FirstName: userG.GivenName,
			LastName:  userG.FamilyName,
			PicUrl:    userG.Picture,
			GroupId:   0,
			RoleId:    0,
			IsActive:  true,
			CreatedBy: 0,
			CreatedOn: time.Now().Unix(),
		}

		res, err := m.userService.Add(c.Context(), *user)
		if err != nil {
			log.Printf("%s", err.Error())
		}

		user.Id = int64(res)
		log.Printf("new user id : %d", res)
	}

	b, err := json.Marshal(user)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("%s", b)

	// Create the Claims
	claims := &middlewares.JwtCustomClaimsModel{
		Id:            user.Id,
		Name:          userG.Name,
		Email:         userG.Email,
		VerifiedEmail: true,
		FamilyName:    userG.FamilyName,
		Picture:       userG.Picture,
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
