package apis

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// CacheServiceApi struct
type cacheServiceApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	serv services.ICacheService
	log  services.IApiLogService
}

type cacheWipeRequest struct {
	Key     string `json:"key"`
	Prefix  string `json:"prefix"`
	WipeAll bool   `json:"wipeAll"`
}

// Create CacheServiceApi
func NewCacheServiceApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv services.ICacheService,
	apiLogServ services.IApiLogService) {
	handler := &cacheServiceApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
		log:  apiLogServ,
	}

	group := router.PathPrefix("/cache-service").Subrouter()
	group.Use(auth.Middleware)

	group.HandleFunc("", rbac.RbacHandler(handler.list)).Methods("GET")
	group.HandleFunc("/health", rbac.RbacHandler(handler.health)).Methods("GET")
	group.HandleFunc("", rbac.RbacHandler(handler.wipe)).Methods("DELETE")
	group.HandleFunc("/wipe", rbac.RbacHandler(handler.wipeByPayload)).Methods("POST")
}

func (m *cacheServiceApi) list(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))

	res, totalCnt, err := m.serv.ListKeys(r.Context(), prefix, limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendPagingResult(w, res, limit, offset, totalCnt)
}

func (m *cacheServiceApi) health(w http.ResponseWriter, r *http.Request) {
	ok, err := m.serv.Ping(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, ok, "succeed")
}

func (m *cacheServiceApi) wipe(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	wipeAll := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("wipeAll")), "true")

	ok, err := m.executeWipe(r, key, prefix, wipeAll)
	if err != nil {
		if errors.Is(err, controllers.ErrBadRequest) {
			controllers.SendError(w, controllers.ErrBadRequest, "provide key, prefix, or wipeAll=true")
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	m.auditWipe(r, key, prefix, wipeAll)
	controllers.SendResult(w, ok, "succeed")
}

func (m *cacheServiceApi) wipeByPayload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(cacheWipeRequest)
	if err := dec.Decode(body); err != nil {
		if errors.Is(err, io.EOF) {
			controllers.SendError(w, controllers.ErrBadRequest, "payload is required")
			return
		}
		controllers.SendError(w, controllers.ErrBadRequest, "wrong payload")
		return
	}

	ok, err := m.executeWipe(r, strings.TrimSpace(body.Key), strings.TrimSpace(body.Prefix), body.WipeAll)
	if err != nil {
		if errors.Is(err, controllers.ErrBadRequest) {
			controllers.SendError(w, controllers.ErrBadRequest, "provide key, prefix, or wipeAll=true")
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	m.auditWipe(r, strings.TrimSpace(body.Key), strings.TrimSpace(body.Prefix), body.WipeAll)
	controllers.SendResult(w, ok, "succeed")
}

func (m *cacheServiceApi) executeWipe(r *http.Request, key string, prefix string, wipeAll bool) (bool, error) {
	if key == "" && prefix == "" && !wipeAll {
		return false, controllers.ErrBadRequest
	}

	if key != "" {
		return m.serv.WipeByKey(r.Context(), key)
	}

	if wipeAll {
		prefix = ""
	}

	return m.serv.WipeByPrefix(r.Context(), prefix)
}

func (m *cacheServiceApi) auditWipe(r *http.Request, key string, prefix string, wipeAll bool) {
	if m.log == nil {
		return
	}

	createdBy := int64(0)
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		createdBy = claims.Id
	}

	clientIP := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if i := strings.Index(clientIP, ","); i >= 0 {
		clientIP = strings.TrimSpace(clientIP[:i])
	}
	if clientIP == "" {
		host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
		if err == nil {
			clientIP = host
		} else {
			clientIP = strings.TrimSpace(r.RemoteAddr)
		}
	}

	logModel := entities.ApiLog{
		StatsCode:  http.StatusOK,
		LogMsg:     fmt.Sprintf("cache-wipe key=%q prefix=%q wipeAll=%t", key, prefix, wipeAll),
		RequestUrl: r.URL.RequestURI(),
		CreatedBy:  createdBy,
		CreatedAt:  time.Now().UTC().Unix(),
	}
	if strings.Contains(clientIP, ":") {
		logModel.ClientIpAddrV6 = clientIP
	} else {
		logModel.ClientIpAddrV4 = clientIP
	}

	if _, err := m.log.Create(r.Context(), logModel); err != nil {
		log.Printf("cache-service wipe audit warning: %v", err)
	}
}
