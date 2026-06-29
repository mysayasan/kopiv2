package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// ssoCertApi exposes myidsan's SSO certificate authority: download the CA public
// cert, and issue a client certificate (CN = the app's client_id) for mTLS client
// authentication. Issuing returns the client key once and never stores it.
type ssoCertApi struct {
	ca      services.ISsoCaService
	configs dbsql.IGenericRepo[entities.AppAuthConfig]
}

func NewSsoCertApi(router *mux.Router, auth middlewares.AuthMidware, access *middlewares.AccessSessionMidware, ca services.ISsoCaService, configs dbsql.IGenericRepo[entities.AppAuthConfig]) {
	h := &ssoCertApi{ca: ca, configs: configs}
	group := router.PathPrefix("/sso-ca").Subrouter()
	group.Use(auth.Middleware)
	group.Use(access.Middleware)
	group.HandleFunc("", h.getCA).Methods("GET")
	group.HandleFunc("/issue/{id}", h.issue).Methods("POST")
}

func (h *ssoCertApi) getCA(w http.ResponseWriter, r *http.Request) {
	pem, err := h.ca.CARootPEM(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]string{"caCertPem": string(pem)}, "succeed")
}

func (h *ssoCertApi) issue(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	cfg, err := h.configs.GetById(r.Context(), "", id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	if cfg == nil || cfg.ClientId == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "no SSO client configured for this app")
		return
	}
	cert, key, caCert, err := h.ca.IssueClient(r.Context(), cfg.ClientId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]string{
		"clientId":      cfg.ClientId,
		"caCertPem":     string(caCert),
		"clientCertPem": string(cert),
		"clientKeyPem":  string(key),
	}, "succeed")
}
