package apis

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	outputdtos "github.com/mysayasan/kopiv2/domain/shared/dtos/output"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type runtimeLogApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	serv services.IRuntimeLogDtoService[outputdtos.RuntimeLogDto]
}

// Create RuntimeLogApi
func NewRuntimeLogApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv services.IRuntimeLogDtoService[outputdtos.RuntimeLogDto]) {
	handler := &runtimeLogApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
	}

	group := router.PathPrefix("/log-service").Subrouter()
	group.Use(auth.Middleware)
	group.HandleFunc("", rbac.RbacHandler(handler.list)).Methods("GET")
	group.HandleFunc("", rbac.RbacHandler(handler.deleteByMonth)).Methods("DELETE")
}

func (m *runtimeLogApi) list(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)

	res, totalCnt, err := m.serv.List(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendPagingResult(w, res, limit, offset, totalCnt)
}

func (m *runtimeLogApi) deleteByMonth(w http.ResponseWriter, r *http.Request) {
	year, err := strconv.Atoi(r.URL.Query().Get("year"))
	if err != nil || year < 1 {
		controllers.SendError(w, controllers.ErrBadRequest, "year query parameter is required")
		return
	}

	month, err := strconv.Atoi(r.URL.Query().Get("month"))
	if err != nil || month < 1 || month > 12 {
		controllers.SendError(w, controllers.ErrBadRequest, "month query parameter must be between 1 and 12")
		return
	}

	deleted, err := m.serv.DeleteByMonth(r.Context(), year, month)
	if err != nil {
		if errors.Is(err, services.ErrCurrentMonthLogDelete) {
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, map[string]uint64{"deleted": deleted}, "succeed")
}
