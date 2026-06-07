package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	outputdtos "github.com/mysayasan/kopiv2/apps/mymatasan/dtos/output"
	"github.com/mysayasan/kopiv2/domain/entities"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type userLoginApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	serv sharedservices.IUserLoginDtoService[outputdtos.UserLoginDto]
}

func NewUserLoginApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv sharedservices.IUserLoginDtoService[outputdtos.UserLoginDto]) {
	handler := &userLoginApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
	}

	group := router.PathPrefix("/user-login").Subrouter()
	group.Use(auth.Middleware)

	group.HandleFunc("", rbac.RbacHandler(handler.get)).Methods("GET")
	group.HandleFunc("/email", rbac.RbacHandler(handler.getByEmail)).Methods("GET")
}

func (m *userLoginApi) get(w http.ResponseWriter, r *http.Request) {
	opts, err := sharedapis.ParseListQueryOptions[entities.UserLogin](r)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}

	res, totalCnt, err := m.serv.Get(r.Context(), opts.Limit, opts.Offset, opts.Filters, opts.Sorters)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendPagingResult(w, res, opts.Limit, opts.Offset, totalCnt)
}

func (m *userLoginApi) getByEmail(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")

	res, err := m.serv.GetByEmail(r.Context(), email)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendResult(w, res)
}
