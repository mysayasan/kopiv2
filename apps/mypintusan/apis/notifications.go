package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/notification"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type notificationsApi struct {
	svc *notification.Service
}

// NewNotificationsApi registers the unified event feed: door alarms, badge decisions, and the
// app's own security events (a sign-in lockout). One place an operator looks — and the endpoint
// the fleet control plane replays from after a disconnect (`?since=`), which is why a node app
// must serve it: without it, anything raised while the control channel was down silently never
// reaches the parent.
func NewNotificationsApi(router *mux.Router, svc *notification.Service) {
	h := &notificationsApi{svc: svc}
	g := router.PathPrefix("/notifications").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("/{id}/read", h.markRead).Methods("POST")
	// Server-sent events: the feed updates live rather than being polled.
	g.Handle("/stream", svc.StreamHandler()).Methods("GET")
}

func (a *notificationsApi) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := readPaging(r)
	if limit == 0 {
		limit = 100
	}
	q := r.URL.Query()
	// Replay pull (fleet control plane catching up after a disconnect): oldest-first from `since`.
	if sinceRaw := q.Get("since"); sinceRaw != "" {
		since, _ := strconv.ParseInt(sinceRaw, 10, 64)
		rows, total, err := a.svc.ListSince(r.Context(), since, limit)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		controllers.SendResult(w, map[string]any{"items": rows, "total": total}, "succeed")
		return
	}
	rows, total, err := a.svc.List(r.Context(), limit, offset, 0, q.Get("unread") == "true", q.Get("category"), q.Get("source"))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows, "total": total}, "succeed")
}

func (a *notificationsApi) markRead(w http.ResponseWriter, r *http.Request) {
	id, ok := readNotificationID(w, r)
	if !ok {
		return
	}
	n, err := a.svc.MarkRead(r.Context(), uint64(id), notificationActorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, n, "succeed")
}

func readPaging(r *http.Request) (uint64, uint64) {
	q := r.URL.Query()
	limit, _ := strconv.ParseUint(q.Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(q.Get("offset"), 10, 64)
	return limit, offset
}

func readNotificationID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := mux.Vars(r)["id"]
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

// notificationActorId is the signed-in user, for the read-marker trail.
func notificationActorId(r *http.Request) int64 {
	if user, ok := sharedapis.LocalUserFromContext(r.Context()); ok {
		return user.Id
	}
	return 0
}
