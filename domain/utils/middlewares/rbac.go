package middlewares

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// RbacMiddleware struct
type RbacMiddleware struct {
}

// Create NewRbac
func NewRbac() *RbacMiddleware {
	return &RbacMiddleware{}
}

// Logger Handler
func (m *RbacMiddleware) ApiHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		switch c.Method() {
		case "POST":
			{
				var model map[string]interface{}
				err := c.BodyParser(&model)
				if err != nil {
					return controllers.SendError(c, controllers.ErrBadRequest, "wrong payload")
				}

				user := c.Locals("user").(*jwt.Token)

				claims := &JwtCustomClaimsModel{}
				tmp, _ := json.Marshal(user.Claims)
				_ = json.Unmarshal(tmp, claims)

				model["createdBy"] = claims.Id
				model["createdAt"] = time.Now().Unix()

				modres, _ := json.Marshal(model)
				log.Info(string(modres))
				c.Request().SwapBody(modres)
				break
			}
		case "PUT":
			{
				var model map[string]interface{}
				err := c.BodyParser(&model)
				if err != nil {
					return controllers.SendError(c, controllers.ErrBadRequest, "wrong payload")
				}

				user := c.Locals("user").(*jwt.Token)

				claims := &JwtCustomClaimsModel{}
				tmp, _ := json.Marshal(user.Claims)
				_ = json.Unmarshal(tmp, claims)

				model["updatedBy"] = claims.Id
				model["updatedAt"] = time.Now().Unix()

				modres, _ := json.Marshal(model)
				log.Info(string(modres))
				c.Request().SwapBody(modres)
				break
			}
		}

		return c.Next()
	}
}
