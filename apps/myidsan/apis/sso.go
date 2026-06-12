package apis

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/config"
)

type ssoApi struct {
	auth          *middlewares.AuthMidware
	rbac          *middlewares.RbacMidware
	internalToken string
}

type introspectRequest struct {
	Token    string `json:"token"`
	Audience string `json:"audience"`
}

type introspectResponse struct {
	Active        bool     `json:"active"`
	UserId        int64    `json:"userId,omitempty"`
	RoleId        int64    `json:"roleId,omitempty"`
	Email         string   `json:"email,omitempty"`
	Name          string   `json:"name,omitempty"`
	SessionId     string   `json:"sessionId,omitempty"`
	Issuer        string   `json:"issuer,omitempty"`
	Audience      []string `json:"audience,omitempty"`
	AppCode       string   `json:"appCode,omitempty"`
	PolicyVersion int64    `json:"policyVersion,omitempty"`
	ExpiresAt     int64    `json:"expiresAt,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

type authorizeRequest struct {
	Token    string `json:"token"`
	Audience string `json:"audience"`
	Host     string `json:"host"`
	Path     string `json:"path"`
	Method   string `json:"method"`
}

type authorizeResponse struct {
	Active        bool                             `json:"active"`
	Allowed       bool                             `json:"allowed"`
	Reason        string                           `json:"reason"`
	UserId        int64                            `json:"userId,omitempty"`
	RoleId        int64                            `json:"roleId,omitempty"`
	Email         string                           `json:"email,omitempty"`
	SessionId     string                           `json:"sessionId,omitempty"`
	Issuer        string                           `json:"issuer,omitempty"`
	Audience      []string                         `json:"audience,omitempty"`
	AppCode       string                           `json:"appCode,omitempty"`
	PolicyVersion int64                            `json:"policyVersion,omitempty"`
	Matched       *middlewares.AuthorizationResult `json:"decision,omitempty"`
}

func NewSSOApi(router *mux.Router, cfg *config.AppConfigModel, auth *middlewares.AuthMidware, rbac *middlewares.RbacMidware) {
	handler := &ssoApi{
		auth: auth,
		rbac: rbac,
	}
	if cfg != nil {
		handler.internalToken = strings.TrimSpace(cfg.SSO.InternalToken)
	}

	group := router.PathPrefix("/sso").Subrouter()
	group.HandleFunc("/introspect", handler.introspect).Methods("POST")
	group.HandleFunc("/authorize", handler.authorize).Methods("POST")
}

func (m *ssoApi) introspect(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeInternal(r) {
		controllers.SendError(w, controllers.ErrLimitedAccess, "internal token is required")
		return
	}

	body := new(introspectRequest)
	if err := decodeJSON(w, r, body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	claims, err := m.auth.ClaimsFromToken(r.Context(), body.Token)
	if err != nil {
		controllers.SendResult(w, introspectResponse{Active: false, Reason: err.Error()})
		return
	}
	if !audienceAllowed(claims, body.Audience) {
		controllers.SendResult(w, introspectResponse{Active: false, Reason: "token audience not valid"})
		return
	}

	controllers.SendResult(w, introspectionFromClaims(claims, ""))
}

func (m *ssoApi) authorize(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeInternal(r) {
		controllers.SendError(w, controllers.ErrLimitedAccess, "internal token is required")
		return
	}

	body := new(authorizeRequest)
	if err := decodeJSON(w, r, body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	claims, err := m.auth.ClaimsFromToken(r.Context(), body.Token)
	if err != nil {
		controllers.SendResult(w, authorizeResponse{Active: false, Allowed: false, Reason: err.Error()})
		return
	}
	appCode := strings.TrimSpace(body.Audience)
	if !audienceAllowed(claims, appCode) {
		controllers.SendResult(w, authorizeResponse{Active: false, Allowed: false, Reason: "token audience not valid"})
		return
	}

	decision, err := m.rbac.AuthorizeClaimsForApp(r.Context(), claims, appCode, body.Host, body.Path, body.Method)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, authorizeResponse{
		Active:        true,
		Allowed:       decision.Allowed,
		Reason:        decision.Reason,
		UserId:        claims.Id,
		RoleId:        claims.RoleId,
		Email:         claims.Email,
		SessionId:     claims.SessionId,
		Issuer:        claims.Issuer,
		Audience:      []string(claims.Audience),
		AppCode:       appCode,
		PolicyVersion: claims.PolicyVersion,
		Matched:       decision,
	})
}

func (m *ssoApi) authorizeInternal(r *http.Request) bool {
	if m.internalToken == "" {
		return false
	}
	if strings.TrimSpace(r.Header.Get("X-Myidsan-Internal-Token")) == m.internalToken {
		return true
	}
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer ")) == m.internalToken
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(target)
}

func introspectionFromClaims(claims *models.JwtCustomClaims, reason string) introspectResponse {
	res := introspectResponse{
		Active:        reason == "",
		UserId:        claims.Id,
		RoleId:        claims.RoleId,
		Email:         claims.Email,
		Name:          claims.Name,
		SessionId:     claims.SessionId,
		Issuer:        claims.Issuer,
		Audience:      []string(claims.Audience),
		AppCode:       claims.AppCode,
		PolicyVersion: claims.PolicyVersion,
		Reason:        reason,
	}
	if claims.ExpiresAt != nil {
		res.ExpiresAt = claims.ExpiresAt.Time.Unix()
	}
	return res
}

func audienceAllowed(claims *models.JwtCustomClaims, audience string) bool {
	audience = strings.TrimSpace(audience)
	if audience == "" {
		return true
	}
	for _, actual := range claims.Audience {
		if strings.EqualFold(strings.TrimSpace(actual), audience) {
			return true
		}
	}
	return false
}
