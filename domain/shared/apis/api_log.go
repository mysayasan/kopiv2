package apis

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// ApiLogApi struct
type apiLogApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	serv services.IApiLogService
}

// Create ApiLogApi
func NewApiLogApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv services.IApiLogService) {
	handler := &apiLogApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
	}

	// Create api sub-router
	group := router.PathPrefix("/log").Subrouter()
	group.Use(auth.Middleware)

	// Group Handlers
	group.HandleFunc("", rbac.RbacHandler(handler.get)).Methods("GET")
	group.HandleFunc("", rbac.RbacHandler(handler.deleteByMonth)).Methods("DELETE")

	// group := router.Group("log")
	// group.Get("/", auth.JwtHandler(), rbac.ApiHandler(), timeout.NewWithContext(handler.get, 60*1000*time.Millisecond)).Name("latest")
}

func (m *apiLogApi) get(w http.ResponseWriter, r *http.Request) {

	limit, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)

	res, totalCnt, err := m.serv.Get(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendPagingResult(w, res, limit, offset, totalCnt)
}

func (m *apiLogApi) deleteByMonth(w http.ResponseWriter, r *http.Request) {
	year, err := strconv.Atoi(r.URL.Query().Get("year"))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "year is required")
		return
	}

	month, err := strconv.Atoi(r.URL.Query().Get("month"))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "month is required")
		return
	}

	deleted, err := m.serv.DeleteByMonth(r.Context(), year, month)
	if err != nil {
		if errors.Is(err, services.ErrCurrentMonthApiLogDelete) {
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
			return
		}
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}

	controllers.SendResult(w, map[string]uint64{"deleted": deleted}, "api logs deleted")
}
