package apis

import (
	"net/http"

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
