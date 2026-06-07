package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	inputdtos "github.com/mysayasan/kopiv2/domain/shared/dtos/input"
	outputdtos "github.com/mysayasan/kopiv2/domain/shared/dtos/output"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// ApiEndpointApi struct
type apiEndpointApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	serv services.IApiEndpointDtoService[outputdtos.ApiEndpointDto]
}

// Create ApiEndpointApi
func NewApiEndpointApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv services.IApiEndpointDtoService[outputdtos.ApiEndpointDto]) {
	handler := &apiEndpointApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
	}

	// Create api sub-router
	group := router.PathPrefix("/endpoint").Subrouter()
	group.Use(auth.Middleware)

	// Group Handlers
	group.HandleFunc("", rbac.RbacHandler(handler.get)).Methods("GET")
	group.HandleFunc("", rbac.RbacHandler(handler.post)).Methods("POST")
	group.HandleFunc("", rbac.RbacHandler(handler.put)).Methods("PUT")
	group.HandleFunc("/{id}", rbac.RbacHandler(handler.delete)).Methods("DELETE")
}

func (m *apiEndpointApi) get(w http.ResponseWriter, r *http.Request) {

	opts, err := parseListQueryOptions[entities.ApiEndpoint](r)
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

func (m *apiEndpointApi) post(w http.ResponseWriter, r *http.Request) {
	body, err := decodeRequestDto[inputdtos.ApiEndpointDto, entities.ApiEndpoint](w, r)
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

func (m *apiEndpointApi) put(w http.ResponseWriter, r *http.Request) {
	body, err := decodeRequestDto[inputdtos.ApiEndpointDto, entities.ApiEndpoint](w, r)
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

func (m *apiEndpointApi) delete(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	res, err := m.serv.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}
