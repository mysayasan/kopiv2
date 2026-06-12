package middlewares

import (
	"context"
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
	appCode   string
}

type RbacConfig struct {
	AppCode  string
	CacheTTL time.Duration
}

type AuthorizationResult struct {
	Allowed bool                               `json:"allowed"`
	Reason  string                             `json:"reason"`
	Matched *entities.ApiEndpointRbacJoinModel `json:"matched,omitempty"`
}

// Create NewRbac
func NewRbac(
	apiEpServ services.IApiEndpointRbacService,
	cacheStore cache.Store,
	cacheTTL time.Duration,
) *RbacMidware {
	return NewRbacWithConfig(apiEpServ, cacheStore, RbacConfig{CacheTTL: cacheTTL})
}

func NewRbacWithConfig(
	apiEpServ services.IApiEndpointRbacService,
	cacheStore cache.Store,
	cfg RbacConfig,
) *RbacMidware {
	cacheTTL := cfg.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 10 * time.Second
	}

	return &RbacMidware{
		apiEpServ: apiEpServ,
		cache:     cacheStore,
		cacheTTL:  cacheTTL,
		appCode:   strings.TrimSpace(cfg.AppCode),
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
		decision, err := m.AuthorizeClaims(r.Context(), claims, host, path, r.Method)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}

		if !decision.Allowed {
			controllers.SendError(w, controllers.ErrPermission, decision.Reason)
			return
		}

		switch strings.ToUpper(r.Method) {
		case "POST":
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
		}

		next(w, r)
	}
}

func (m *RbacMidware) AuthorizeClaims(ctx context.Context, claims *models.JwtCustomClaims, host string, path string, method string) (*AuthorizationResult, error) {
	return m.AuthorizeClaimsForApp(ctx, claims, m.appCode, host, path, method)
}

func (m *RbacMidware) AuthorizeClaimsForApp(ctx context.Context, claims *models.JwtCustomClaims, appCode string, host string, path string, method string) (*AuthorizationResult, error) {
	if claims == nil || claims.RoleId < 1 {
		return &AuthorizationResult{Allowed: false, Reason: "token not valid"}, nil
	}

	userAccs, err := m.getAccessList(ctx, claims, appCode)
	if err != nil {
		return nil, err
	}
	if len(userAccs) == 0 {
		return &AuthorizationResult{Allowed: false, Reason: "limited access to resources"}, nil
	}

	var userAccess *entities.ApiEndpointRbacJoinModel
	for _, userAcc := range userAccs {
		if !appMatches(userAcc.AppCode, appCode) || !hostMatches(userAcc.Host, host) || !pathMatches(userAcc.Path, path) {
			continue
		}
		userAccess = userAcc
		break
	}
	if userAccess == nil {
		return &AuthorizationResult{Allowed: false, Reason: "limited access to resources"}, nil
	}

	allowed, reason := methodAllowed(userAccess, method)
	if allowed {
		reason = "allowed"
	}
	return &AuthorizationResult{
		Allowed: allowed,
		Reason:  reason,
		Matched: userAccess,
	}, nil
}

func (m *RbacMidware) getAccessList(ctx context.Context, claims *models.JwtCustomClaims, appCode string) ([]*entities.ApiEndpointRbacJoinModel, error) {
	cacheKey := rbacPolicyCacheKey(claims, appCode)
	var userAccs []*entities.ApiEndpointRbacJoinModel

	if m.cache != nil {
		found, err := m.cache.Get(ctx, cacheKey, &userAccs)
		if err != nil {
			log.Printf("rbac cache get warning key=%s err=%v", cacheKey, err)
			found = false
		}
		if found {
			return userAccs, nil
		}
	}

	userAccs, _, err := m.apiEpServ.GetApiEpByUserRole(ctx, uint64(claims.Id))
	if err != nil {
		return nil, err
	}

	if m.cache != nil {
		if err := m.cache.Set(ctx, cacheKey, userAccs, m.cacheTTL); err != nil {
			log.Printf("rbac cache set warning key=%s err=%v", cacheKey, err)
		}
	}

	return userAccs, nil
}

func rbacPolicyCacheKey(claims *models.JwtCustomClaims, resourceAppCode string) string {
	appCode := strings.TrimSpace(resourceAppCode)
	if appCode == "" {
		appCode = strings.TrimSpace(claims.AppCode)
		if appCode == "" && len(claims.Audience) > 0 {
			appCode = strings.TrimSpace(claims.Audience[0])
		}
		if appCode == "" {
			appCode = "default"
		}
	}
	policyVersion := claims.PolicyVersion
	if policyVersion <= 0 {
		policyVersion = 1
	}
	return strings.Join([]string{
		memcacheenums.GetString(memcacheenums.Mware_Rbac_GetApiEpByUserRole_Result),
		appCode,
		strconv.FormatInt(claims.RoleId, 10),
		strconv.FormatInt(policyVersion, 10),
	}, ":")
}

func appMatches(allowedAppCode string, requestAppCode string) bool {
	allowedAppCode = strings.TrimSpace(allowedAppCode)
	requestAppCode = strings.TrimSpace(requestAppCode)
	return allowedAppCode == "" || requestAppCode == "" || strings.EqualFold(allowedAppCode, requestAppCode)
}

func methodAllowed(userAccess *entities.ApiEndpointRbacJoinModel, method string) (bool, string) {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET":
		return userAccess.CanGet, "cant read due to limited access"
	case "POST":
		return userAccess.CanPost, "cant create due to limited access"
	case "PUT":
		return userAccess.CanPut, "cant update due to limited access"
	case "DELETE":
		return userAccess.CanDelete, "cant delete due to limited access"
	default:
		return false, "limited access to resources"
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
