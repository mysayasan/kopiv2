package apis

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// defaultStockSuperadminEmail is the fallback bootstrap login when localAuth.username
// is unset (matches EnsureStockSuperadmin's default).
const defaultStockSuperadminEmail = "superadmin"

// identityStatusApi reports superadmin-handoff state so the SPA can show a persistent
// banner reminding the operator to disable the stock superadmin once a real one exists.
// The stock account's email is the configured localAuth.username (not hardcoded).
type identityStatusApi struct {
	users      dbsql.IGenericRepo[entities.UserLogin]
	roles      sharedservices.IAccessRoleService
	stockEmail string
}

func NewIdentityStatusApi(router *mux.Router, auth middlewares.AuthMidware, access *middlewares.AccessSessionMidware, users dbsql.IGenericRepo[entities.UserLogin], roles sharedservices.IAccessRoleService, stockEmail string) {
	stockEmail = strings.TrimSpace(stockEmail)
	if stockEmail == "" {
		stockEmail = defaultStockSuperadminEmail
	}
	h := &identityStatusApi{users: users, roles: roles, stockEmail: stockEmail}
	group := router.PathPrefix("/identity-status").Subrouter()
	group.Use(auth.Middleware)
	group.Use(access.Middleware)
	group.HandleFunc("", h.get).Methods("GET")
}

func (h *identityStatusApi) get(w http.ResponseWriter, r *http.Request) {
	super, err := h.roles.GetByName(r.Context(), sharedservices.RoleSuperadmin)
	if err != nil || super == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "superadmin role missing")
		return
	}
	rows, _, err := h.users.Get(r.Context(), "", 1000, 0, nil, nil)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	stockActive, realActive := false, false
	for _, u := range rows {
		if u == nil || !u.IsActive || u.UserRoleId != super.Id {
			continue
		}
		if u.Email == h.stockEmail {
			stockActive = true
		} else {
			realActive = true
		}
	}
	controllers.SendResult(w, map[string]any{
		"stockSuperadminActive":    stockActive,
		"superadminHandoffPending": stockActive && realActive,
		"stockEmail":               h.stockEmail,
	}, "succeed")
}
