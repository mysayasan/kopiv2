package login

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/infra/middlewares"
)

// GoogleLogin struct
type GoogleLogin struct {
	conf OAuth2ConfigModel
	auth middlewares.AuthMiddleware
}

// Create GoogleLogin
func NewGoogleLogin(conf OAuth2ConfigModel, auth middlewares.AuthMiddleware) *GoogleLogin {
	return &GoogleLogin{
		conf: conf,
		auth: auth,
	}
}

func (m *GoogleLogin) Login(c *fiber.Ctx) error {

	url := AppConfig.GoogleLoginConfig.AuthCodeURL("randomstate")

	c.Status(fiber.StatusSeeOther)
	c.Redirect(url)
	return c.JSON(url)
}

func (m *GoogleLogin) Callback(c *fiber.Ctx) error {
	state := c.Query("state")
	if state != "randomstate" {
		return c.SendString("States don't Match!!")
	}

	code := c.Query("code")

	googlecon := GoogleConfig(m.conf)

	token, err := googlecon.Exchange(context.Background(), code)
	if err != nil {
		return c.SendString("Code-Token Exchange Failed")
	}

	resp, err := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + token.AccessToken)
	if err != nil {
		return c.SendString("User Data Fetch Failed")
	}

	userData, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.SendString("JSON Parsing Failed")
	}

	var userJson GoogleUserInfoModel
	json.Unmarshal(userData, &userJson)

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
