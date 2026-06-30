package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	inputdtos "github.com/mysayasan/kopiv2/apps/myidsan/dtos/input"
	outputdtos "github.com/mysayasan/kopiv2/apps/myidsan/dtos/output"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/models"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// UserLoginApi struct
type userLoginApi struct {
	auth middlewares.AuthMidware
	serv services.IUserLoginDtoService[outputdtos.UserLoginDto]
}

// Create UserLoginApi
//
// User-account management (listing, role assignment, enable/disable, deletion) is
// SUPERADMIN-ONLY, mirroring myseliasan's /api/rbac surface. Role assignment is a
// privilege-escalation vector, so it must never be governed by the generic permission
// matrix alone (a non-superadmin role with a broad GET/PUT grant could otherwise change
// any account's role — including its own — to superadmin).
func NewUserLoginApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	access *middlewares.AccessSessionMidware,
	serv services.IUserLoginDtoService[outputdtos.UserLoginDto]) {
	handler := &userLoginApi{
		auth: auth,
		serv: serv,
	}

	// Create api sub-router — the whole user-account surface is superadmin-only.
	group := router.PathPrefix("/user-credential").Subrouter()
	group.Use(auth.Middleware)
	group.Use(access.Middleware)
	group.Use(access.RequireSuperadmin)

	// Group Handlers
	group.HandleFunc("", handler.get).Methods("GET")
	group.HandleFunc("/email", handler.getByEmail).Methods("GET")
	group.HandleFunc("", handler.put).Methods("PUT")
	group.HandleFunc("/{id}", handler.delete).Methods("DELETE")
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
	usermail := r.URL.Query().Get("email")

	res, err := m.serv.GetByEmail(r.Context(), usermail)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendResult(w, res)
}

func (m *userLoginApi) put(w http.ResponseWriter, r *http.Request) {
	body, err := sharedapis.DecodeRequestDto[inputdtos.UserLoginDto, entities.UserLogin](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	// Defence in depth: a superadmin may not change their OWN role (no self-elevation
	// games, and no accidental self-lockout). claims.RoleId is the live role — the
	// access middleware re-stamps it from the user store on each request.
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		if body.Id == claims.Id && body.UserRoleId != claims.RoleId {
			controllers.SendError(w, controllers.ErrLimitedAccess, "you cannot change your own role")
			return
		}
	}

	res, err := m.serv.Update(r.Context(), *body)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}

func (m *userLoginApi) delete(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	res, err := m.serv.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}
