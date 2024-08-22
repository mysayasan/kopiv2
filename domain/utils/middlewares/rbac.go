package middlewares

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/domain/entities"
	memcacheenums "github.com/mysayasan/kopiv2/domain/enums/memcache"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	goCache "github.com/patrickmn/go-cache"
)

// RbacMiddleware struct
type RbacMiddleware struct {
	apiEpServ services.IApiEndpointRbacService
	memCache  *goCache.Cache
}

// Create NewRbac
func NewRbac(
	apiEpServ services.IApiEndpointRbacService,
	memCache *goCache.Cache,
) *RbacMiddleware {
	return &RbacMiddleware{
		apiEpServ: apiEpServ,
		memCache:  memCache,
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

		var userAccs []*entities.ApiEndpointRbacJoinModel

		res, found := m.memCache.Get(memcacheenums.GetString(memcacheenums.Mware_Rbac_GetApiEpByUserRole_Result))
		if found {
			userAccs = res.([]*entities.ApiEndpointRbacJoinModel)
		} else {
			userAccs, _, _ = m.apiEpServ.GetApiEpByUserRole(c.Context(), uint64(claims.RoleId))
			m.memCache.Set(memcacheenums.GetString(memcacheenums.Mware_Rbac_GetApiEpByUserRole_Result), userAccs, goCache.DefaultExpiration)
		}

		if len(userAccs) == 0 {
			return controllers.SendError(c, controllers.ErrPermission, "limited access to resources")
		}

		isValid := false
		validPath := ""
		var userAccess *entities.ApiEndpointRbacJoinModel = nil

		for _, userAcc := range userAccs {
			userAcc := userAcc
			if userAcc.Host != host || userAcc.Path == "" || len(path) < len(userAcc.Path) {
				continue
			}

			validPath = path[0:len(userAcc.Path)]
			if validPath == userAcc.Path {
				isValid = true
				userAccess = userAcc
				break
			}
		}

		if !isValid {
			return controllers.SendError(c, controllers.ErrPermission, "limited access to resources")
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
