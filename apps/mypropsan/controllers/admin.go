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
	router fiber.Router,
	auth middlewares.AuthMiddleware) {
	handler := &adminApi{
		auth: auth,
	}

	group := router.Group("admin")
	group.Get("/test", auth.JwtHandler(), handler.restricted).Name("test")
}

func (m *adminApi) restricted(c *fiber.Ctx) error {
	user := c.Locals("user").(*jwt.Token)

	claims := &middlewares.JwtCustomClaimsModel{}
	tmp, _ := json.Marshal(user.Claims)
	_ = json.Unmarshal(tmp, claims)

	name := claims.Name
	return c.SendString("Welcome " + name)
}