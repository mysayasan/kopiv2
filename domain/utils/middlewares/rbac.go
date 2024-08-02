package middlewares

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// RbacMiddleware struct
type RbacMiddleware struct {
	apiEpServ services.IApiEndpointRbacService
}

// Create NewRbac
func NewRbac(apiEpServ services.IApiEndpointRbacService) *RbacMiddleware {
	return &RbacMiddleware{
		apiEpServ: apiEpServ,
	}
}

// Logger Handler
func (m *RbacMiddleware) ApiHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(*jwt.Token)
		tmp, _ := json.Marshal(user.Claims)
		claims := &JwtCustomClaimsModel{}
		_ = json.Unmarshal(tmp, claims)

		c.Locals("claims", claims)

		host := string(c.Request().Host())
		path := string(c.Request().URI().Path())

		userAccs, err := m.apiEpServ.GetApiEpByUserRole(c.Context(), uint64(claims.RoleId))
		if err != nil {
			return controllers.SendError(c, controllers.ErrPermission, err.Error())
		}

		isValid := false
		validPath := ""

		for _, userAcc := range userAccs {
			userAcc := userAcc
			if userAcc.Host != host || userAcc.Path == "" || len(path) < len(userAcc.Path) {
				continue
			}

			validPath = path[0:len(userAcc.Path)]
			if validPath == userAcc.Path {
				isValid = true
				break
			}
		}

		if !isValid {
			return controllers.SendError(c, controllers.ErrPermission, "cant proceed limited access to api")
		}

		userAccess, err := m.apiEpServ.Validate(c.Context(), host, validPath, uint64(claims.RoleId))
		if err != nil {
			return controllers.SendError(c, controllers.ErrPermission, err.Error())
		}

		switch c.Method() {
		case "GET":
			{
				if !userAccess.CanGet {
					return controllers.SendError(c, controllers.ErrPermission, "cant read due to limited access")
				}
				break
			}
		case "POST":
			{
				if !userAccess.CanPost {
					return controllers.SendError(c, controllers.ErrPermission, "cant create due to limited access")
				}

				var model map[string]interface{}
				err := c.BodyParser(&model)
				if err != nil {
					return controllers.SendError(c, controllers.ErrBadRequest, "wrong payload")
				}
				model["createdBy"] = claims.Id
				model["createdAt"] = time.Now().Unix()

				modres, _ := json.Marshal(model)
				log.Info(string(modres))
				c.Request().SwapBody(modres)
				break
			}
		case "PUT":
			{
				if !userAccess.CanPut {
					return controllers.SendError(c, controllers.ErrPermission, "cant update due to limited access")
				}

				var model map[string]interface{}
				err := c.BodyParser(&model)
				if err != nil {
					return controllers.SendError(c, controllers.ErrBadRequest, "wrong payload")
				}
				model["updatedBy"] = claims.Id
				model["updatedAt"] = time.Now().Unix()

				modres, _ := json.Marshal(model)
				log.Info(string(modres))
				c.Request().SwapBody(modres)
				break
			}
		case "DELETE":
			{
				if !userAccess.CanDelete {
					return controllers.SendError(c, controllers.ErrPermission, "cant delete due to limited access")
				}
				break
			}
		}

		return c.Next()
	}
}
