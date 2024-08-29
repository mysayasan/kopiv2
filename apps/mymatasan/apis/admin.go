package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// AdminApi struct
type adminApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
}

// Create AdminApi
func NewAdminApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware) {
	handler := &adminApi{
		auth: auth,
		rbac: rbac,
	}

	// Create api sub-router
	group := router.PathPrefix("/admin").Subrouter()

	// Group Handlers
	group.HandleFunc("/test", rbac.RbacHandler(handler.restricted)).Methods("GET")

	// group := router.Group("admin")
	// group.Get("/test", auth.JwtHandler(), rbac.ApiHandler(), handler.restricted).Name("test")
}

func (m *adminApi) restricted(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	controllers.SendResult(w, "Welcome "+claims.Name)
}
