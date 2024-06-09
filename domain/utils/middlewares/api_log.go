package middlewares

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// ApiLogMiddleware struct
type ApiLogMiddleware struct {
}

// Create NewApiLog
func NewApiLog(secret string) *AuthMiddleware {
	return &AuthMiddleware{}
}

// Jwt Handler
func (m *AuthMiddleware) LoggerHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Method() == "POST" {
			var model map[string]interface{}
			err := c.BodyParser(&model)
			if err != nil {
				return controllers.SendError(c, controllers.ErrBadRequest, "wrong payload")
			}

			user := c.Locals("user").(*jwt.Token)

			claims := &JwtCustomClaimsModel{}
			tmp, _ := json.Marshal(user.Claims)
			_ = json.Unmarshal(tmp, claims)

			model["created_by"] = claims.Id
			model["created_on"] = time.Now().Unix()

			log.Printf("%s", model)
		}
		return nil
	}
}
