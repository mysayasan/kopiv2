package apis

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type notificationApi struct {
	serv services.INotificationService
}

// NewNotificationApi registers the unified notification feed routes under
// /notifications.
func NewNotificationApi(router *mux.Router, serv services.INotificationService) {
	h := &notificationApi{serv: serv}
	g := router.PathPrefix("/notifications").Subrouter()

	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("/stats", h.stats).Methods("GET")
	g.HandleFunc("/heatmap", h.heatmap).Methods("GET")
	g.HandleFunc("/baseline", h.baseline).Methods("GET")
	g.HandleFunc("/reliability", h.reliability).Methods("GET")
	g.HandleFunc("/noise", h.noise).Methods("GET")
	g.HandleFunc("/stream", h.stream).Methods("GET")
	g.HandleFunc("/purge", h.purge).Methods("POST")
	g.HandleFunc("/{id}/read", h.markRead).Methods("POST")
}

func (a *notificationApi) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := readPaging(r)
	cameraId := parseInt64Query(r, "cameraId")
	unreadOnly := r.URL.Query().Get("unread") == "true"
	category := r.URL.Query().Get("category")
	source := r.URL.Query().Get("source")

	items, total, err := a.serv.List(r.Context(), limit, offset, cameraId, unreadOnly, category, source)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"items": items,
		"total": total,
	}, "succeed")
}

// stats returns the aggregated dashboard payload for the events in the given
// time window. from/to are unix seconds (to defaults to now, from to 7 days
// before now); bucket is "hour" or "day" (default day); tzOffset is the
// viewer's timezone offset in minutes so buckets align to their local clock.
func (a *notificationApi) stats(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Unix()
	to := parseInt64Query(r, "to")
	if to <= 0 {
		to = now
	}
	from := parseInt64Query(r, "from")
	if from <= 0 {
		from = to - 7*86400
	}
	bucketSeconds := int64(86400)
	if strings.EqualFold(r.URL.Query().Get("bucket"), "hour") {
		bucketSeconds = 3600
	}
	// tzOffset is minutes (JS getTimezoneOffset is inverted, so the client sends
	// the already-corrected value); clamp to a sane ±14h.
	tzOffsetMin := parseInt64Query(r, "tzOffset")
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

// heatmap returns the activity-rhythm grid (local day-of-week × hour-of-day) for
// the given window. from/to are unix seconds (to defaults to now, from to 28 days
// before — enough history for a stable weekly rhythm); cameraId>0 scopes to one
// camera; tzOffset is the viewer's timezone offset in minutes.
func (a *notificationApi) heatmap(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Unix()
	to := parseInt64Query(r, "to")
	if to <= 0 {
		to = now
	}
	from := parseInt64Query(r, "from")
	if from <= 0 {
		from = to - 28*86400
	}
	cameraId := parseInt64Query(r, "cameraId")
	tzOffsetMin := parseInt64Query(r, "tzOffset")
	if tzOffsetMin < -840 || tzOffsetMin > 840 {
		tzOffsetMin = 0
	}

	result, err := a.serv.Heatmap(r.Context(), from, to, cameraId, tzOffsetMin*60)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
}

// baseline returns the expected-activity band for each bucket of the events-over-
// time chart in the given window, matching its granularity (bucket "hour" or "day").
// It shares the stats window params (from/to/tzOffset) plus an optional cameraId.
func (a *notificationApi) baseline(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Unix()
	to := parseInt64Query(r, "to")
	if to <= 0 {
		to = now
	}
	from := parseInt64Query(r, "from")
	if from <= 0 {
		from = to - 7*86400
	}
	bucketSeconds := int64(86400)
	if strings.EqualFold(r.URL.Query().Get("bucket"), "hour") {
		bucketSeconds = 3600
	}
	cameraId := parseInt64Query(r, "cameraId")
	tzOffsetMin := parseInt64Query(r, "tzOffset")
	if tzOffsetMin < -840 || tzOffsetMin > 840 {
		tzOffsetMin = 0
	}

	result, err := a.serv.Baseline(r.Context(), from, to, bucketSeconds, tzOffsetMin*60, cameraId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
}

// reliability returns per-camera uptime/outage stats over the window (from/to unix
// seconds; defaults to the last 7 days), worst-first.
func (a *notificationApi) reliability(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Unix()
	to := parseInt64Query(r, "to")
	if to <= 0 {
		to = now
	}
	from := parseInt64Query(r, "from")
	if from <= 0 {
		from = to - 7*86400
	}
	result, err := a.serv.CameraReliability(r.Context(), from, to)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
}

// noise returns the noisiest cameras (top AI-alert volume + unread count) over the
// window (from/to unix seconds; defaults to the last 7 days).
func (a *notificationApi) noise(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Unix()
	to := parseInt64Query(r, "to")
	if to <= 0 {
		to = now
	}
	from := parseInt64Query(r, "from")
	if from <= 0 {
		from = to - 7*86400
	}
	limit := int(parseInt64Query(r, "limit"))
	result, err := a.serv.NoisyCameras(r.Context(), from, to, limit)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
}

// stream serves the live Server-Sent Events feed of new notifications.
func (a *notificationApi) stream(w http.ResponseWriter, r *http.Request) {
	a.serv.StreamHandler().ServeHTTP(w, r)
}

// purge deletes notifications older than the given number of days. The
// olderThanDays query parameter is required; onlyRead=true keeps unread ones.
func (a *notificationApi) purge(w http.ResponseWriter, r *http.Request) {
	days := int(parseInt64Query(r, "olderThanDays"))
	if days <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "olderThanDays must be greater than zero")
		return
	}
	onlyRead := r.URL.Query().Get("onlyRead") == "true"
	deleted, err := a.serv.PurgeOlderThanDays(r.Context(), days, onlyRead)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]int{"deleted": deleted}, "succeed")
}

func (a *notificationApi) markRead(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	notif, err := a.serv.MarkRead(r.Context(), id, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, notif, "succeed")
}
