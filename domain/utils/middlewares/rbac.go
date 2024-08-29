package middlewares

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	memcacheenums "github.com/mysayasan/kopiv2/domain/enums/memcache"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	goCache "github.com/patrickmn/go-cache"
)

// RbacMidware struct
type RbacMidware struct {
	apiEpServ services.IApiEndpointRbacService
	memCache  *goCache.Cache
}

// Create NewRbac
func NewRbac(
	apiEpServ services.IApiEndpointRbacService,
	memCache *goCache.Cache,
) *RbacMidware {
	return &RbacMidware{
		apiEpServ: apiEpServ,
		memCache:  memCache,
	}
}

// Logger Handler
func (m *RbacMidware) RbacHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
		host := string(r.Host)
		path := string(r.URL.Path)

		var userAccs []*entities.ApiEndpointRbacJoinModel

		res, found := m.memCache.Get(memcacheenums.GetString(memcacheenums.Mware_Rbac_GetApiEpByUserRole_Result))
		if found {
			userAccs = res.([]*entities.ApiEndpointRbacJoinModel)
		} else {
			userAccs, _, _ = m.apiEpServ.GetApiEpByUserRole(r.Context(), uint64(claims.RoleId))
			m.memCache.Set(memcacheenums.GetString(memcacheenums.Mware_Rbac_GetApiEpByUserRole_Result), userAccs, goCache.DefaultExpiration)
		}

		if len(userAccs) == 0 {
			controllers.SendError(w, controllers.ErrPermission, "limited access to resources")
			return
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
			controllers.SendError(w, controllers.ErrPermission, "limited access to resources")
			return
		}

		switch r.Method {
		case "GET":
			{
				if !userAccess.CanGet {
					controllers.SendError(w, controllers.ErrPermission, "cant read due to limited access")
					return
				}
				break
			}
		case "POST":
			{
				if !userAccess.CanPost {
					controllers.SendError(w, controllers.ErrPermission, "cant create due to limited access")
					return
				}

				var body map[string]interface{}
				// err := c.BodyParser(&model)

				r.Body = http.MaxBytesReader(w, r.Body, 1048576)
				dec := json.NewDecoder(r.Body)
				dec.DisallowUnknownFields()
				err := dec.Decode(&body)

				if err != nil {
					controllers.SendError(w, controllers.ErrBadRequest, "wrong payload")
					return
				}
				body["createdBy"] = claims.Id
				body["createdAt"] = time.Now().Unix()

				modres, _ := json.Marshal(body)
				fmt.Println(string(modres))
				// c.Request().SwapBody(modres)
				r.Body = io.NopCloser(strings.NewReader(string(modres)))
				break
			}
		case "PUT":
			{
				if !userAccess.CanPut {
					controllers.SendError(w, controllers.ErrPermission, "cant update due to limited access")
					return
				}

				var body map[string]interface{}
				// err := c.BodyParser(&model)
				r.Body = http.MaxBytesReader(w, r.Body, 1048576)
				dec := json.NewDecoder(r.Body)
				dec.DisallowUnknownFields()
				err := dec.Decode(&body)
				if err != nil {
					controllers.SendError(w, controllers.ErrBadRequest, "wrong payload")
					return
				}
				body["updatedBy"] = claims.Id
				body["updatedAt"] = time.Now().Unix()

				modres, _ := json.Marshal(body)
				fmt.Println(string(modres))
				// c.Request().SwapBody(modres)
				r.Body = io.NopCloser(strings.NewReader(string(modres)))
				break
			}
		case "DELETE":
			{
				if !userAccess.CanDelete {
					controllers.SendError(w, controllers.ErrPermission, "cant delete due to limited access")
					return
				}
				break
			}
		}

		next(w, r)
	}
}
