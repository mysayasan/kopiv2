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
	repo dbsql.IGenericRepo[entities.AppAuthConfig]
}

// RefreshTokenTTLSeconds, RequirePKCE and AllowRefreshToken are deliberately absent from
// both the payload and the view. The columns still exist on entities.AppAuthConfig, but
// nothing in the authorize/token path has ever read them — code_challenge is not parsed
// and grant_type=refresh_token is rejected outright — so accepting and echoing them told
// operators a security control was configured when it was not. They come back, enforced,
// with OIDC conformance (docs/MYIDSAN_PRODUCTIZATION_PLAN.md phases 5.3 and 5.4).
type appAuthConfigPayload struct {
	Id                    int64  `json:"id"`
	AppRegistryId         int64  `json:"appRegistryId"`
	ClientId              string `json:"clientId"`
	ClientSecret          string `json:"clientSecret"`
	AuthCodeTTLSeconds    int64  `json:"authCodeTtlSeconds"`
	AccessTokenTTLSeconds int64  `json:"accessTokenTtlSeconds"`
	SessionTTLSeconds     int64  `json:"sessionTtlSeconds"`
	IsActive              bool   `json:"isActive"`
	CreatedBy             int64  `json:"createdBy"`
	CreatedAt             int64  `json:"createdAt"`
	UpdatedBy             int64  `json:"updatedBy"`
	UpdatedAt             int64  `json:"updatedAt"`
}

type appAuthConfigView struct {
	Id                    int64  `json:"id"`
	AppRegistryId         int64  `json:"appRegistryId"`
	ClientId              string `json:"clientId"`
	HasClientSecret       bool   `json:"hasClientSecret"`
	AuthCodeTTLSeconds    int64  `json:"authCodeTtlSeconds"`
	AccessTokenTTLSeconds int64  `json:"accessTokenTtlSeconds"`
	SessionTTLSeconds     int64  `json:"sessionTtlSeconds"`
	IsActive              bool   `json:"isActive"`
	CreatedBy             int64  `json:"createdBy"`
	CreatedAt             int64  `json:"createdAt"`
	UpdatedBy             int64  `json:"updatedBy"`
	UpdatedAt             int64  `json:"updatedAt"`
}

func NewAppAuthConfigApi(router *mux.Router, auth middlewares.AuthMidware, access *middlewares.AccessSessionMidware, repo dbsql.IGenericRepo[entities.AppAuthConfig]) {
	handler := &appAuthConfigApi{auth: auth, repo: repo}
	group := router.PathPrefix("/app-auth-config").Subrouter()
	group.Use(auth.Middleware)
	group.Use(access.Middleware) // RBAC-matrix governed (delegate per role; not granted by default)
	group.HandleFunc("", handler.get).Methods("GET")
	group.HandleFunc("", handler.post).Methods("POST")
	group.HandleFunc("", handler.put).Methods("PUT")
	group.HandleFunc("/{id}", handler.delete).Methods("DELETE")
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
	secretHash, err := hashClientSecret(payload.ClientSecret)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	model := appAuthConfigPayloadToEntity(*payload, secretHash)
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
		hashed, err := hashClientSecret(payload.ClientSecret)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		secretHash = hashed
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
	// The refresh/PKCE columns are left at their zero values: they are unread by the
	// authorize and token paths, and writing an operator-supplied value would reintroduce
	// the impression that they do something. See the type comment above.
	return entities.AppAuthConfig{
		Id:                    payload.Id,
		AppRegistryId:         payload.AppRegistryId,
		ClientId:              payload.ClientId,
		ClientSecretHash:      secretHash,
		AuthCodeTTLSeconds:    payload.AuthCodeTTLSeconds,
		AccessTokenTTLSeconds: payload.AccessTokenTTLSeconds,
		SessionTTLSeconds:     payload.SessionTTLSeconds,
		IsActive:              payload.IsActive,
		CreatedBy:             payload.CreatedBy,
		CreatedAt:             payload.CreatedAt,
		UpdatedBy:             payload.UpdatedBy,
		UpdatedAt:             payload.UpdatedAt,
	}
}

func appAuthConfigToView(row *entities.AppAuthConfig) appAuthConfigView {
	if row == nil {
		return appAuthConfigView{}
	}
	return appAuthConfigView{
		Id:                    row.Id,
		AppRegistryId:         row.AppRegistryId,
		ClientId:              row.ClientId,
		HasClientSecret:       strings.TrimSpace(row.ClientSecretHash) != "",
		AuthCodeTTLSeconds:    row.AuthCodeTTLSeconds,
		AccessTokenTTLSeconds: row.AccessTokenTTLSeconds,
		SessionTTLSeconds:     row.SessionTTLSeconds,
		IsActive:              row.IsActive,
		CreatedBy:             row.CreatedBy,
		CreatedAt:             row.CreatedAt,
		UpdatedBy:             row.UpdatedBy,
		UpdatedAt:             row.UpdatedAt,
	}
}
