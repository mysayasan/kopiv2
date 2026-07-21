package apis

import (
	"net/http"
	"strconv"
	"strings"
	"time"

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
//	GET  /api/notifications           — list (paged; ?nodeId= scopes to one node, ?unread=true)
//	GET  /api/notifications/stream    — live Server-Sent Events feed
//	POST /api/notifications/{id}/read — mark one notification read (clears the unread badge)
func NewNotificationApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, serv *notification.Service) {
	h := &notificationApi{serv: serv}
	g := router.PathPrefix("/notifications").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("/stats", h.stats).Methods("GET")
	g.HandleFunc("/tally", h.tally).Methods("GET")
	g.HandleFunc("/stream", h.stream).Methods("GET")
	g.HandleFunc("/{id:[0-9]+}/read", h.markRead).Methods("POST")
}

// stats returns the aggregated dashboard payload over myseliasan's OWN notifications
// table (node-pushed events + control-plane events) for the given window. from/to are
// unix seconds (to defaults to now, from to 7 days before); bucket is "hour" or "day"
// (default day); tzOffset is the viewer's timezone offset in minutes so buckets align
// to the local clock. The per-node breakdown is Stats.BySource (source = "node:<id>").
func (a *notificationApi) stats(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Unix()
	to := queryInt64(r, "to", 0)
	if to <= 0 {
		to = now
	}
	from := queryInt64(r, "from", 0)
	if from <= 0 {
		from = to - 7*86400
	}
	bucketSeconds := int64(86400)
	if strings.EqualFold(r.URL.Query().Get("bucket"), "hour") {
		bucketSeconds = 3600
	}
	tzOffsetMin := queryInt64(r, "tzOffset", 0)
	if tzOffsetMin < -840 || tzOffsetMin > 840 {
		tzOffsetMin = 0
	}

	result, err := a.serv.Stats(r.Context(), from, to, bucketSeconds, tzOffsetMin*60)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
}

func (a *notificationApi) list(w http.ResponseWriter, r *http.Request) {
	limit := queryUint(r, "limit", 100)
	offset := queryUint(r, "offset", 0)
	source := ""
	if nodeID := strings.TrimSpace(r.URL.Query().Get("nodeId")); nodeID != "" {
		source = "node:" + nodeID
	}
	unreadOnly := r.URL.Query().Get("unread") == "true"
	// cameraId scopes the feed to a single camera SERVER-SIDE. Without it the caller has to
	// over-fetch the whole node feed and filter in the browser, which silently drops a
	// camera's events once they fall outside the fetched page (the per-camera badge counts
	// the full set via /tally, so the two disagree). 0 = no camera filter.
	cameraId := int64(queryUint(r, "cameraId", 0))

	items, total, err := a.serv.List(r.Context(), limit, offset, cameraId, unreadOnly, "", source)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items, "total": total}, "succeed")
}

// tally returns compact per-(source,camera) notification counts aggregated on the server, so the
// fleet map can badge nodes/buildings without paging every raw row to the browser. ?unread=true
// (the map's use) restricts it to the unread feed.
func (a *notificationApi) tally(w http.ResponseWriter, r *http.Request) {
	unreadOnly := r.URL.Query().Get("unread") == "true"
	rows, err := a.serv.Tally(r.Context(), unreadOnly)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, rows, "succeed")
}

func (a *notificationApi) stream(w http.ResponseWriter, r *http.Request) {
	a.serv.StreamHandler().ServeHTTP(w, r)
}

// markRead flags one notification as read for the signed-in operator, so the
// consolidated feed's unread badge clears. The feed is shared, so a read here is a
// read for everyone (the control plane has a single unread state, not per-user).
func (a *notificationApi) markRead(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	n, err := a.serv.MarkRead(r.Context(), id, operatorUserId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, n, "succeed")
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

func queryInt64(r *http.Request, key string, def int64) int64 {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}
