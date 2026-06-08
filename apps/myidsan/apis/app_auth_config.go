package apis

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type appAuthConfigApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	repo dbsql.IGenericRepo[entities.AppAuthConfig]
}

type appAuthConfigPayload struct {
	Id                     int64  `json:"id"`
	AppRegistryId          int64  `json:"appRegistryId"`
	ClientId               string `json:"clientId"`
	ClientSecret           string `json:"clientSecret"`
	AuthCodeTTLSeconds     int64  `json:"authCodeTtlSeconds"`
	AccessTokenTTLSeconds  int64  `json:"accessTokenTtlSeconds"`
	SessionTTLSeconds      int64  `json:"sessionTtlSeconds"`
	RefreshTokenTTLSeconds int64  `json:"refreshTokenTtlSeconds"`
	RequirePKCE            bool   `json:"requirePkce"`
	AllowRefreshToken      bool   `json:"allowRefreshToken"`
	IsActive               bool   `json:"isActive"`
	CreatedBy              int64  `json:"createdBy"`
	CreatedAt              int64  `json:"createdAt"`
	UpdatedBy              int64  `json:"updatedBy"`
	UpdatedAt              int64  `json:"updatedAt"`
}

type appAuthConfigView struct {
	Id                     int64  `json:"id"`
	AppRegistryId          int64  `json:"appRegistryId"`
	ClientId               string `json:"clientId"`
	HasClientSecret        bool   `json:"hasClientSecret"`
	AuthCodeTTLSeconds     int64  `json:"authCodeTtlSeconds"`
	AccessTokenTTLSeconds  int64  `json:"accessTokenTtlSeconds"`
	SessionTTLSeconds      int64  `json:"sessionTtlSeconds"`
	RefreshTokenTTLSeconds int64  `json:"refreshTokenTtlSeconds"`
	RequirePKCE            bool   `json:"requirePkce"`
	AllowRefreshToken      bool   `json:"allowRefreshToken"`
	IsActive               bool   `json:"isActive"`
	CreatedBy              int64  `json:"createdBy"`
	CreatedAt              int64  `json:"createdAt"`
	UpdatedBy              int64  `json:"updatedBy"`
	UpdatedAt              int64  `json:"updatedAt"`
}

func NewAppAuthConfigApi(router *mux.Router, auth middlewares.AuthMidware, rbac middlewares.RbacMidware, repo dbsql.IGenericRepo[entities.AppAuthConfig]) {
	handler := &appAuthConfigApi{auth: auth, rbac: rbac, repo: repo}
	group := router.PathPrefix("/app-auth-config").Subrouter()
	group.Use(auth.Middleware)
	group.HandleFunc("", rbac.RbacHandler(handler.get)).Methods("GET")
	group.HandleFunc("", rbac.RbacHandler(handler.post)).Methods("POST")
	group.HandleFunc("", rbac.RbacHandler(handler.put)).Methods("PUT")
	group.HandleFunc("/{id}", rbac.RbacHandler(handler.delete)).Methods("DELETE")
}

func (m *appAuthConfigApi) get(w http.ResponseWriter, r *http.Request) {
	opts, err := sharedapis.ParseListQueryOptions[entities.AppAuthConfig](r)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	rows, totalCnt, err := m.repo.Get(r.Context(), "", opts.Limit, opts.Offset, opts.Filters, opts.Sorters)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	views := make([]appAuthConfigView, 0, len(rows))
	for _, row := range rows {
		views = append(views, appAuthConfigToView(row))
	}
	controllers.SendPagingResult(w, views, opts.Limit, opts.Offset, totalCnt)
}

func (m *appAuthConfigApi) post(w http.ResponseWriter, r *http.Request) {
	payload, err := sharedapis.DecodeRequestDto[appAuthConfigPayload, appAuthConfigPayload](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if strings.TrimSpace(payload.ClientSecret) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "clientSecret is required")
		return
	}
	model := appAuthConfigPayloadToEntity(*payload, hashClientSecret(payload.ClientSecret))
	id, err := m.repo.Create(r.Context(), "", model)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"id": id}, "succeed")
}

func (m *appAuthConfigApi) put(w http.ResponseWriter, r *http.Request) {
	payload, err := sharedapis.DecodeRequestDto[appAuthConfigPayload, appAuthConfigPayload](w, r)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	existing, err := m.repo.GetById(r.Context(), "", uint64(payload.Id))
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	secretHash := existing.ClientSecretHash
	if strings.TrimSpace(payload.ClientSecret) != "" {
		secretHash = hashClientSecret(payload.ClientSecret)
	}
	model := appAuthConfigPayloadToEntity(*payload, secretHash)
	affected, err := m.repo.UpdateById(r.Context(), "", model)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"affected": affected}, "succeed")
}

func (m *appAuthConfigApi) delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	affected, err := m.repo.DeleteById(r.Context(), "", id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"affected": affected}, "succeed")
}

func appAuthConfigPayloadToEntity(payload appAuthConfigPayload, secretHash string) entities.AppAuthConfig {
	return entities.AppAuthConfig{
		Id:                     payload.Id,
		AppRegistryId:          payload.AppRegistryId,
		ClientId:               payload.ClientId,
		ClientSecretHash:       secretHash,
		AuthCodeTTLSeconds:     payload.AuthCodeTTLSeconds,
		AccessTokenTTLSeconds:  payload.AccessTokenTTLSeconds,
		SessionTTLSeconds:      payload.SessionTTLSeconds,
		RefreshTokenTTLSeconds: payload.RefreshTokenTTLSeconds,
		RequirePKCE:            payload.RequirePKCE,
		AllowRefreshToken:      payload.AllowRefreshToken,
		IsActive:               payload.IsActive,
		CreatedBy:              payload.CreatedBy,
		CreatedAt:              payload.CreatedAt,
		UpdatedBy:              payload.UpdatedBy,
		UpdatedAt:              payload.UpdatedAt,
	}
}

func appAuthConfigToView(row *entities.AppAuthConfig) appAuthConfigView {
	if row == nil {
		return appAuthConfigView{}
	}
	return appAuthConfigView{
		Id:                     row.Id,
		AppRegistryId:          row.AppRegistryId,
		ClientId:               row.ClientId,
		HasClientSecret:        strings.TrimSpace(row.ClientSecretHash) != "",
		AuthCodeTTLSeconds:     row.AuthCodeTTLSeconds,
		AccessTokenTTLSeconds:  row.AccessTokenTTLSeconds,
		SessionTTLSeconds:      row.SessionTTLSeconds,
		RefreshTokenTTLSeconds: row.RefreshTokenTTLSeconds,
		RequirePKCE:            row.RequirePKCE,
		AllowRefreshToken:      row.AllowRefreshToken,
		IsActive:               row.IsActive,
		CreatedBy:              row.CreatedBy,
		CreatedAt:              row.CreatedAt,
		UpdatedBy:              row.UpdatedBy,
		UpdatedAt:              row.UpdatedAt,
	}
}
