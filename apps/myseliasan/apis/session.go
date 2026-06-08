package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type sessionApi struct {
	auth middlewares.AuthMidware
}

func NewSessionApi(router *mux.Router, auth middlewares.AuthMidware) {
	handler := &sessionApi{auth: auth}
	group := router.PathPrefix("/session").Subrouter()
	group.Use(auth.Middleware)
	group.HandleFunc("/me", handler.me).Methods("GET")
}

func (m *sessionApi) me(w http.ResponseWriter, r *http.Request) {
	claims, _ := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	controllers.SendResult(w, map[string]any{
		"userId":        claims.Id,
		"roleId":        claims.RoleId,
		"email":         claims.Email,
		"name":          claims.Name,
		"issuer":        claims.Issuer,
		"audience":      []string(claims.Audience),
		"appCode":       claims.AppCode,
		"policyVersion": claims.PolicyVersion,
	})
}
