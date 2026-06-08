package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	inputdtos "github.com/mysayasan/kopiv2/apps/myidsan/dtos/input"
	outputdtos "github.com/mysayasan/kopiv2/apps/myidsan/dtos/output"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// UserGroupApi struct
type userGroupApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	serv services.IUserGroupDtoService[outputdtos.UserGroupDto]
}

// Create UserGroupApi
func NewUserGroupApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv services.IUserGroupDtoService[outputdtos.UserGroupDto]) {
	handler := &userGroupApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
	}

	// Create api sub-router
	group := router.PathPrefix("/user-group").Subrouter()
	group.Use(auth.Middleware)

	// Group Handlers
	group.HandleFunc("", rbac.RbacHandler(handler.get)).Methods("GET")
	group.HandleFunc("", rbac.RbacHandler(handler.post)).Methods("POST")
	group.HandleFunc("", rbac.RbacHandler(handler.put)).Methods("PUT")
	group.HandleFunc("/{id}", rbac.RbacHandler(handler.delete)).Methods("DELETE")
}

func (m *userGroupApi) get(w http.ResponseWriter, r *http.Request) {

	opts, err := sharedapis.ParseListQueryOptions[entities.UserGroup](r)
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

func (m *userGroupApi) post(w http.ResponseWriter, r *http.Request) {
	body, err := sharedapis.DecodeRequestDto[inputdtos.UserGroupDto, entities.UserGroup](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	res, err := m.serv.Create(r.Context(), *body)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}

func (m *userGroupApi) put(w http.ResponseWriter, r *http.Request) {
	body, err := sharedapis.DecodeRequestDto[inputdtos.UserGroupDto, entities.UserGroup](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	res, err := m.serv.Update(r.Context(), *body)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}

func (m *userGroupApi) delete(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	res, err := m.serv.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}
