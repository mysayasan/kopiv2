package apis

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type pushApi struct {
	svc     services.IPushService
	session *middlewares.AccessSessionMidware
}

// NewPushApi registers mobile push (W3-9).
//
// TWO AXES, and they are not the same question.
//
// The first is the ordinary one: the whole surface sits behind the accessrbac matrix like
// every other module, so a role needs a grant on `/api/push` before anybody holding it can
// enrol a phone. That is deliberate and it is NOT bureaucracy. Enabling push makes this
// appliance open outbound HTTPS connections to a browser vendor — on the intranet installs
// this product is usually sold into, the deployment's entire security posture is that it does
// not do that. Whether this control plane talks to Google or Apple at all is an
// ADMINISTRATOR's decision, not one each signed-in user gets to make for the estate.
//
// The second is ownership, and the matrix cannot express it: a push subscription is a phone in
// somebody's pocket, not a fleet object. Within the grant, a user sees and acts on THEIR OWN
// devices only. A superadmin sees all of them, because somebody has to be able to revoke the
// device of a person who has left, and a subscription nobody can remove keeps receiving fleet
// alerts forever.
//
// The service enforces that ownership rule again on every call; this layer only decides which
// flag to pass. Two places, because a check that exists only in the HTTP layer is one refactor
// away from not existing at all.
func NewPushApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, svc services.IPushService) {
	h := &pushApi{svc: svc, session: session}
	g := router.PathPrefix("/push").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	// What this appliance can actually do — including, on an air-gapped install, the honest
	// answer that it cannot reach any push service. See PushStatus.
	g.HandleFunc("/status", h.status).Methods("GET")
	g.HandleFunc("/devices", h.list).Methods("GET")
	g.HandleFunc("/devices", h.subscribe).Methods("POST")
	g.HandleFunc("/devices/{id}", h.remove).Methods("DELETE")
	g.HandleFunc("/devices/{id}/test", h.test).Methods("POST")
}

func (a *pushApi) admin(r *http.Request) bool {
	return a.session != nil && a.session.IsSuperadmin(r)
}

func (a *pushApi) status(w http.ResponseWriter, r *http.Request) {
	userId, _, _ := auditActor(r)
	out, err := a.svc.Status(r.Context(), userId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, out, "succeed")
}

func (a *pushApi) list(w http.ResponseWriter, r *http.Request) {
	userId, _, _ := auditActor(r)
	items, err := a.svc.List(r.Context(), userId, a.admin(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *pushApi) subscribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var body services.PushSubscribeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	userId, _, _ := auditActor(r)
	// Subscribe performs a REAL delivery before it returns, so the view handed back already
	// carries the verdict. That is the whole contract of this feature: a device is not
	// "registered", it is registered and proved, or registered and known not to be reachable.
	view, err := a.svc.Subscribe(r.Context(), body, userId)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (a *pushApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pushDeviceId(w, r)
	if !ok {
		return
	}
	userId, _, _ := auditActor(r)
	if err := a.svc.Unsubscribe(r.Context(), id, userId, a.admin(r)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"id": id}, "succeed")
}

func (a *pushApi) test(w http.ResponseWriter, r *http.Request) {
	id, ok := pushDeviceId(w, r)
	if !ok {
		return
	}
	userId, _, _ := auditActor(r)
	view, err := a.svc.TestDevice(r.Context(), id, userId, a.admin(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func pushDeviceId(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}
