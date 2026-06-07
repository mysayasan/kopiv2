package middlewares

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	memcacheenums "github.com/mysayasan/kopiv2/domain/enums/memcache"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/cache"
)

// RbacMidware struct
type RbacMidware struct {
	apiEpServ services.IApiEndpointRbacService
	cache     cache.Store
	cacheTTL  time.Duration
}

// Create NewRbac
func NewRbac(
	apiEpServ services.IApiEndpointRbacService,
	cacheStore cache.Store,
	cacheTTL time.Duration,
) *RbacMidware {
	if cacheTTL <= 0 {
		cacheTTL = 10 * time.Second
	}

	return &RbacMidware{
		apiEpServ: apiEpServ,
		cache:     cacheStore,
		cacheTTL:  cacheTTL,
	}
}

// Logger Handler
func (m *RbacMidware) RbacHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
		if !ok || claims == nil || claims.RoleId < 1 {
			controllers.SendError(w, controllers.ErrPermission, "token not valid")
			return
		}

		host := string(r.Host)
		path := string(r.URL.Path)
		cacheKey := memcacheenums.GetString(memcacheenums.Mware_Rbac_GetApiEpByUserRole_Result) + ":" + strconv.FormatInt(claims.RoleId, 10)

		var userAccs []*entities.ApiEndpointRbacJoinModel

		found, err := m.cache.Get(r.Context(), cacheKey, &userAccs)
		if err != nil {
			log.Printf("rbac cache get warning key=%s err=%v", cacheKey, err)
			found = false
		}

		if !found {
			var err error
			userAccs, _, err = m.apiEpServ.GetApiEpByUserRole(r.Context(), uint64(claims.Id))
			if err != nil {
				controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
				return
			}

			if err := m.cache.Set(r.Context(), cacheKey, userAccs, m.cacheTTL); err != nil {
				log.Printf("rbac cache set warning key=%s err=%v", cacheKey, err)
			}
		}

		if len(userAccs) == 0 {
			controllers.SendError(w, controllers.ErrPermission, "limited access to resources")
			return
		}

		isValid := false
		var userAccess *entities.ApiEndpointRbacJoinModel = nil

		for _, userAcc := range userAccs {
			if !hostMatches(userAcc.Host, host) || !pathMatches(userAcc.Path, path) {
				continue
			}

			isValid = true
			userAccess = userAcc
			break
		}

		if !isValid {
			controllers.SendError(w, controllers.ErrPermission, "limited access to resources")
			return
		}

		switch r.Method {
		case "GET":
			if !userAccess.CanGet {
				controllers.SendError(w, controllers.ErrPermission, "cant read due to limited access")
				return
			}
		case "POST":
			if !userAccess.CanPost {
				controllers.SendError(w, controllers.ErrPermission, "cant create due to limited access")
				return
			}

			if isJSONContentType(r.Header.Get("Content-Type")) {
				var body map[string]interface{}

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
				r.Body = io.NopCloser(strings.NewReader(string(modres)))
			}
		case "PUT":
			if !userAccess.CanPut {
				controllers.SendError(w, controllers.ErrPermission, "cant update due to limited access")
				return
			}

			if isJSONContentType(r.Header.Get("Content-Type")) {
				var body map[string]interface{}
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
				r.Body = io.NopCloser(strings.NewReader(string(modres)))
			}
		case "DELETE":
			if !userAccess.CanDelete {
				controllers.SendError(w, controllers.ErrPermission, "cant delete due to limited access")
				return
			}
		default:
			controllers.SendError(w, controllers.ErrPermission, "limited access to resources")
			return
		}

		next(w, r)
	}
}

func hostMatches(allowedHost string, requestHost string) bool {
	allowedHost = normalizeHost(allowedHost)
	requestHost = normalizeHost(requestHost)
	return allowedHost == requestHost || allowedHost == "*"
}

func pathMatches(allowedPath string, requestPath string) bool {
	allowedPath = strings.TrimRight(strings.TrimSpace(allowedPath), "/")
	requestPath = strings.TrimRight(strings.TrimSpace(requestPath), "/")
	if allowedPath == "" {
		return false
	}
	if requestPath == allowedPath {
		return true
	}
	return strings.HasPrefix(requestPath, allowedPath+"/")
}

func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.Index(contentType, ";"); i >= 0 {
		contentType = strings.TrimSpace(contentType[:i])
	}
	return contentType == "application/json" || strings.HasSuffix(contentType, "+json")
}
