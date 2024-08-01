package apis

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/login"
)

// LoginApi struct
type loginApi struct {
	auth        middlewares.AuthMiddleware
	userService services.IUserLoginService
	googleAuth  *login.GoogleLogin
	githubAuth  *login.GithubLogin
}

// Create LoginApi
func NewLoginApi(
	router fiber.Router,
	oAuth2Conf *login.OAuth2ConfigModel,
	auth middlewares.AuthMiddleware,
	userService services.IUserLoginService) {

	login.GoogleConfig(*oAuth2Conf)
	login.GithubConfig(*oAuth2Conf)

	googleLogin := login.NewGoogleLogin(*oAuth2Conf, auth)
	githubLogin := login.NewGithubLogin(*oAuth2Conf, auth)

	handler := &loginApi{
		auth:        auth,
		userService: userService,
		googleAuth:  googleLogin,
		githubAuth:  githubLogin,
	}

	groupLogin := router.Group("login")
	callbackLogin := router.Group("callback")

	groupLogin.Post("/default", handler.defaultLogin).Name("default_login")
	groupLogin.Get("/google", handler.googleLogin).Name("google_login")
	callbackLogin.Get("/google", handler.googleCallback).Name("google_callback")
	groupLogin.Get("/github", handler.githubLogin).Name("github_login")
	callbackLogin.Get("/github", handler.githubCallback).Name("github_callback")
}

func (m *loginApi) defaultLogin(c *fiber.Ctx) error {
	var model map[string]interface{}

	err := c.BodyParser(&model)
	if err != nil {
		return controllers.SendError(c, controllers.ErrParseFailed, err.Error())
	}

	return c.SendString("ok")
}

func (m *loginApi) googleLogin(c *fiber.Ctx) error {
	return m.googleAuth.Login(c)
}

func (m *loginApi) googleCallback(c *fiber.Ctx) error {
	userG, err := m.googleAuth.Callback(c)
	if err != nil {
		return c.SendString(err.Error())
	}

	user, err := m.userService.GetByEmail(c.Context(), userG.Email)
	if err != nil {
		log.Printf("%s", err.Error())
	}

	if user == nil {
		user = &entities.UserLogin{
			Email:       userG.Email,
			FirstName:   userG.GivenName,
			LastName:    userG.FamilyName,
			PicUrl:      userG.Picture,
			UserGroupId: 0,
			UserRoleId:  0,
			IsActive:    true,
			CreatedBy:   0,
			CreatedAt:   time.Now().Unix(),
		}

		res, err := m.userService.Create(c.Context(), *user)
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
		RoleId:        user.UserRoleId,
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

func (m *loginApi) githubLogin(c *fiber.Ctx) error {
	return m.githubAuth.Login(c)
}

func (m *loginApi) githubCallback(c *fiber.Ctx) error {
	return m.githubAuth.Callback(c)
}
