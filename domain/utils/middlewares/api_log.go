package middlewares

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// ApiLogMiddleware struct
type ApiLogMiddleware struct {
}

// Create NewApiLog
func NewApiLog() *ApiLogMiddleware {
	return &ApiLogMiddleware{}
}

// Logger Handler
func (m *ApiLogMiddleware) LoggerHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		log.Info(c.Method())
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

				model["created_by"] = claims.Id
				model["created_on"] = time.Now().Unix()

				log.Info(fmt.Sprintf("%s", model))
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

				model["updated_by"] = claims.Id
				model["updated_on"] = time.Now().Unix()

				log.Info("%s", model)
				break
			}
		}
		log.Info("here")
		return c.Next()
	}
}
