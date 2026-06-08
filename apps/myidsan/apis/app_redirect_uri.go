package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type appRedirectUriApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	repo dbsql.IGenericRepo[entities.AppRedirectUri]
}

func NewAppRedirectUriApi(router *mux.Router, auth middlewares.AuthMidware, rbac middlewares.RbacMidware, repo dbsql.IGenericRepo[entities.AppRedirectUri]) {
	handler := &appRedirectUriApi{auth: auth, rbac: rbac, repo: repo}
	group := router.PathPrefix("/app-redirect-uri").Subrouter()
	group.Use(auth.Middleware)
	group.HandleFunc("", rbac.RbacHandler(handler.get)).Methods("GET")
	group.HandleFunc("", rbac.RbacHandler(handler.post)).Methods("POST")
	group.HandleFunc("", rbac.RbacHandler(handler.put)).Methods("PUT")
	group.HandleFunc("/{id}", rbac.RbacHandler(handler.delete)).Methods("DELETE")
}

func (m *appRedirectUriApi) get(w http.ResponseWriter, r *http.Request) {
	opts, err := sharedapis.ParseListQueryOptions[entities.AppRedirectUri](r)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	rows, totalCnt, err := m.repo.Get(r.Context(), "", opts.Limit, opts.Offset, opts.Filters, opts.Sorters)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, opts.Limit, opts.Offset, totalCnt)
}

func (m *appRedirectUriApi) post(w http.ResponseWriter, r *http.Request) {
	body, err := sharedapis.DecodeRequestDto[entities.AppRedirectUri, entities.AppRedirectUri](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	id, err := m.repo.Create(r.Context(), "", *body)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"id": id}, "succeed")
}

func (m *appRedirectUriApi) put(w http.ResponseWriter, r *http.Request) {
	body, err := sharedapis.DecodeRequestDto[entities.AppRedirectUri, entities.AppRedirectUri](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	affected, err := m.repo.UpdateById(r.Context(), "", *body)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"affected": affected}, "succeed")
}

func (m *appRedirectUriApi) delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	affected, err := m.repo.DeleteById(r.Context(), "", id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"affected": affected}, "succeed")
}
