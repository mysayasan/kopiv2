package apis

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// ApiEndpointRbacApi struct
type apiEndpointRbacApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	serv services.IApiEndpointRbacService
}

// Create ApiEndpointRbacApi
func NewApiEndpointRbacApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv services.IApiEndpointRbacService) {
	handler := &apiEndpointRbacApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
	}

	// Create api sub-router
	group := router.PathPrefix("/endpoint-rbac").Subrouter()
	group.Use(auth.Middleware)

	// Group Handlers
	group.HandleFunc("", rbac.RbacHandler(handler.get)).Methods("GET")
	group.HandleFunc("/validate/me", rbac.RbacHandler(handler.getValidate)).Methods("GET")
	group.HandleFunc("/ep/me", rbac.RbacHandler(handler.getApiEpByUserRole)).Methods("GET")
	group.HandleFunc("", rbac.RbacHandler(handler.post)).Methods("POST")
	group.HandleFunc("", rbac.RbacHandler(handler.put)).Methods("PUT")
	group.HandleFunc("/{id}", rbac.RbacHandler(handler.delete)).Methods("DELETE")
}

func (m *apiEndpointRbacApi) get(w http.ResponseWriter, r *http.Request) {

	limit, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)

	res, totalCnt, err := m.serv.Get(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendPagingResult(w, res, limit, offset, totalCnt)
}

func (m *apiEndpointRbacApi) getApiEpByUserRole(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)

	res, _, err := m.serv.GetApiEpByUserRole(r.Context(), uint64(claims.Id))
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendResult(w, res)
}

func (m *apiEndpointRbacApi) getValidate(w http.ResponseWriter, r *http.Request) {

	claims := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)

	host := r.URL.Query().Get("host")
	path := r.URL.Query().Get("path")

	res, err := m.serv.Validate(r.Context(), host, path, uint64(claims.RoleId))
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}

func (m *apiEndpointRbacApi) post(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(entities.ApiEndpointRbac)

	if err := dec.Decode(&body); err != nil {
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

func (m *apiEndpointRbacApi) put(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(entities.ApiEndpointRbac)

	if err := dec.Decode(&body); err != nil {
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

func (m *apiEndpointRbacApi) delete(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	res, err := m.serv.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}
