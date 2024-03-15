package controllers

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/infra/middlewares"
)

// AdminApi struct
type adminApi struct {
	auth middlewares.AuthMiddleware
}

// Create AdminApi
func NewAdminApi(
	app *fiber.App,
	auth middlewares.AuthMiddleware) {
	handler := &adminApi{
		auth: auth,
	}

	app.Get("/restricted", auth.JwtHandler(), handler.restricted)
}

func (m *adminApi) restricted(c *fiber.Ctx) error {
	user := c.Locals("user").(*jwt.Token)

	claims := &middlewares.JwtCustomClaims{}
	tmp, _ := json.Marshal(user.Claims)
	_ = json.Unmarshal(tmp, claims)

	name := claims.Name
	return c.SendString("Welcome " + name)
}
