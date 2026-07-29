package apis

import (
	"crypto/subtle"
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

func NewSSOApi(router *mux.Router, cfg *config.AppConfigModel, auth *middlewares.AuthMidware) {
	handler := &ssoApi{
		auth: auth,
	}
	if cfg != nil {
		handler.internalToken = strings.TrimSpace(cfg.SSO.InternalToken)
	}

	group := router.PathPrefix("/sso").Subrouter()
	group.HandleFunc("/introspect", handler.introspect).Methods("POST")
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

// authorizeInternal gates /api/sso/introspect on the shared internal token. Both header
// forms are compared in constant time: a plain == on a secret leaks its length and a
// prefix of its content through timing, and this endpoint answers "is this token valid"
// for any relying app that cannot share the session cache.
func (m *ssoApi) authorizeInternal(r *http.Request) bool {
	if m.internalToken == "" {
		return false
	}
	if constantTimeMatch(r.Header.Get("X-Myidsan-Internal-Token"), m.internalToken) {
		return true
	}
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	return constantTimeMatch(strings.TrimPrefix(authHeader, "Bearer "), m.internalToken)
}

func constantTimeMatch(presented, expected string) bool {
	return subtle.ConstantTimeCompare(
		[]byte(strings.TrimSpace(presented)),
		[]byte(expected),
	) == 1
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
