package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// HomeApi struct
type homeApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	serv services.IHomeService
}

// Create HomeApi
func NewHomeApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv services.IHomeService) {
	handler := &homeApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
	}

	// Create api sub-router
	group := router.PathPrefix("/home").Subrouter()

	// Group Handlers
	group.HandleFunc("/latest", rbac.RbacHandler(handler.latest)).Methods("GET")
	group.HandleFunc("/new", rbac.RbacHandler(handler.new)).Methods("POST")

	// group := router.Group("home")
	// group.Get("/latest", auth.JwtHandler(), rbac.ApiHandler(), timeout.NewWithContext(handler.latest, 60*1000*time.Millisecond)).Name("latest")
	// group.Post("/new", auth.JwtHandler(), rbac.ApiHandler(), timeout.NewWithContext(handler.new, 60*1000*time.Millisecond)).Name("new")
}

func (m *homeApi) latest(w http.ResponseWriter, r *http.Request) {

	limit, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)

	res, totalCnt, err := m.serv.GetLatest(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendPagingResult(w, res, limit, offset, totalCnt)
}

func (m *homeApi) new(w http.ResponseWriter, r *http.Request) {
	controllers.SendPagingResult(w, "ok", 0, 0, 1)
}
