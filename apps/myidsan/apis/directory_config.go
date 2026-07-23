package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// directoryConfigApi manages the LDAP/Active Directory connection (singleton) and
// the federated group→role mappings — the Federation → Directory admin page.
type directoryConfigApi struct {
	directory services.IDirectoryService
	mappings  dbsql.IGenericRepo[myidsanentities.FederatedGroupMapping]
}

func NewDirectoryConfigApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	access *middlewares.AccessSessionMidware,
	directory services.IDirectoryService,
	mappings dbsql.IGenericRepo[myidsanentities.FederatedGroupMapping],
) {
	handler := &directoryConfigApi{directory: directory, mappings: mappings}

	group := router.PathPrefix("/directory-config").Subrouter()
	group.Use(auth.Middleware)
	group.Use(access.Middleware) // RBAC-matrix governed
	group.HandleFunc("", handler.get).Methods("GET")
	group.HandleFunc("", handler.put).Methods("PUT")
	// Test never persists anything; it validates the SUBMITTED settings (stored
	// bind password fills in when the payload leaves it blank).
	group.HandleFunc("/test", handler.test).Methods("POST")

	mappingGroup := router.PathPrefix("/federated-group-mapping").Subrouter()
	mappingGroup.Use(auth.Middleware)
	mappingGroup.Use(access.Middleware)
	mappingGroup.HandleFunc("", handler.listMappings).Methods("GET")
	mappingGroup.HandleFunc("", handler.createMapping).Methods("POST")
	mappingGroup.HandleFunc("", handler.updateMapping).Methods("PUT")
	mappingGroup.HandleFunc("/{id}", handler.deleteMapping).Methods("DELETE")
}

func (m *directoryConfigApi) get(w http.ResponseWriter, r *http.Request) {
	view, err := m.directory.GetView(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, view)
}

func (m *directoryConfigApi) put(w http.ResponseWriter, r *http.Request) {
	payload, err := sharedapis.DecodeRequestDto[services.DirectoryConfigPayload, services.DirectoryConfigPayload](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	view, err := m.directory.Save(r.Context(), *payload, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (m *directoryConfigApi) test(w http.ResponseWriter, r *http.Request) {
	payload, err := sharedapis.DecodeRequestDto[services.DirectoryTestPayload, services.DirectoryTestPayload](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	// The result (including failures) is a 200: "test ran, here is what happened"
	// is a successful admin operation; only a malformed request is an error.
	controllers.SendResult(w, m.directory.Test(r.Context(), *payload))
}

func (m *directoryConfigApi) listMappings(w http.ResponseWriter, r *http.Request) {
	opts, err := sharedapis.ParseListQueryOptions[myidsanentities.FederatedGroupMapping](r)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	rows, totalCnt, err := m.mappings.Get(r.Context(), "", opts.Limit, opts.Offset, opts.Filters, opts.Sorters)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, opts.Limit, opts.Offset, totalCnt)
}

func (m *directoryConfigApi) createMapping(w http.ResponseWriter, r *http.Request) {
	payload, err := sharedapis.DecodeRequestDto[myidsanentities.FederatedGroupMapping, myidsanentities.FederatedGroupMapping](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if !validMapping(payload) {
		controllers.SendError(w, controllers.ErrBadRequest, "provider, groupName and a positive roleId are required")
		return
	}
	id, err := m.mappings.Create(r.Context(), "", *payload)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"id": id}, "succeed")
}

func (m *directoryConfigApi) updateMapping(w http.ResponseWriter, r *http.Request) {
	payload, err := sharedapis.DecodeRequestDto[myidsanentities.FederatedGroupMapping, myidsanentities.FederatedGroupMapping](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if !validMapping(payload) {
		controllers.SendError(w, controllers.ErrBadRequest, "provider, groupName and a positive roleId are required")
		return
	}
	affected, err := m.mappings.UpdateById(r.Context(), "", *payload)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"affected": affected}, "succeed")
}

func (m *directoryConfigApi) deleteMapping(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	affected, err := m.mappings.DeleteById(r.Context(), "", id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"affected": affected}, "succeed")
}

func validMapping(payload *myidsanentities.FederatedGroupMapping) bool {
	return payload != nil && payload.Provider != "" && payload.GroupName != "" && payload.RoleId > 0
}

// actorId extracts the acting admin's user id from the authenticated request.
func actorId(r *http.Request) int64 {
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		return claims.Id
	}
	return 0
}
