package apis

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type notificationApi struct {
	serv *notification.Service
}

// NewNotificationApi mounts the control plane's unified feed of node-pushed events:
//
//	GET /api/notifications        — list (paged; ?nodeId= scopes to one node, ?unread=true)
//	GET /api/notifications/stream — live Server-Sent Events feed
func NewNotificationApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, serv *notification.Service) {
	h := &notificationApi{serv: serv}
	g := router.PathPrefix("/notifications").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("/stream", h.stream).Methods("GET")
}

func (a *notificationApi) list(w http.ResponseWriter, r *http.Request) {
	limit := queryUint(r, "limit", 100)
	offset := queryUint(r, "offset", 0)
	source := ""
	if nodeID := strings.TrimSpace(r.URL.Query().Get("nodeId")); nodeID != "" {
		source = "node:" + nodeID
	}
	unreadOnly := r.URL.Query().Get("unread") == "true"

	items, total, err := a.serv.List(r.Context(), limit, offset, 0, unreadOnly, "", source)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items, "total": total}, "succeed")
}

func (a *notificationApi) stream(w http.ResponseWriter, r *http.Request) {
	a.serv.StreamHandler().ServeHTTP(w, r)
}

func queryUint(r *http.Request, key string, def uint64) uint64 {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}
