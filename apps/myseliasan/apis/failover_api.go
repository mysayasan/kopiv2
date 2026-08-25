package apis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type failoverApi struct {
	svc     services.IFailoverService
	session *middlewares.AccessSessionMidware
}

// NewFailoverApi registers N+1 node failover: which spare appliance covers which recorder,
// whether it has been PROVED able to, and the handover in both directions.
//
// Reading is available to any role that can reach the fleet — "is this site covered" is a
// health question, and hiding it from the people who would notice a gap helps nobody.
// Everything that WRITES is superadmin-only, on the same reasoning as a fleet policy but
// with more at stake: whoever can write a plan can point a building's cameras at a
// different appliance, and whoever can press activate can start recording forty cameras
// that belong to another site.
func NewFailoverApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, svc services.IFailoverService) {
	h := &failoverApi{svc: svc, session: session}
	g := router.PathPrefix("/failover-plans").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("/{id}", h.get).Methods("GET")
	g.HandleFunc("", h.requireSuper(h.save)).Methods("POST")
	g.HandleFunc("/{id}", h.requireSuper(h.remove)).Methods("DELETE")
	// Copy the camera set across now, rather than waiting for the hourly pass. What an
	// operator presses after adding cameras to a site.
	g.HandleFunc("/{id}/stage", h.requireSuper(h.stage)).Methods("POST")
	// The drill. THE button on this screen: it is the only thing that turns "we have a
	// plan" into "we have tested it", and it is deliberately something a person can press
	// on a quiet afternoon rather than only a thing that happens on a timer.
	g.HandleFunc("/{id}/drill", h.requireSuper(h.drill)).Methods("POST")
	g.HandleFunc("/{id}/activate", h.requireSuper(h.activate)).Methods("POST")
	g.HandleFunc("/{id}/release", h.requireSuper(h.release)).Methods("POST")
}

func (a *failoverApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.session == nil || !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmins only")
			return
		}
		next(w, r)
	}
}

func (a *failoverApi) list(w http.ResponseWriter, r *http.Request) {
	items, err := a.svc.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *failoverApi) get(w http.ResponseWriter, r *http.Request) {
	id, ok := failoverId(w, r)
	if !ok {
		return
	}
	view, err := a.svc.Get(r.Context(), id)
	if err != nil {
		a.sendErr(w, err)
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (a *failoverApi) save(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body services.SaveFailoverPlanRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	view, err := a.svc.Save(r.Context(), body, failoverActor(r))
	if err != nil {
		a.sendErr(w, err)
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (a *failoverApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := failoverId(w, r)
	if !ok {
		return
	}
	if err := a.svc.Delete(r.Context(), id, failoverActor(r)); err != nil {
		a.sendErr(w, err)
		return
	}
	controllers.SendResult(w, map[string]any{"id": id}, "succeed")
}

func (a *failoverApi) stage(w http.ResponseWriter, r *http.Request) { a.act(w, r, a.svc.Stage) }
func (a *failoverApi) drill(w http.ResponseWriter, r *http.Request) { a.act(w, r, a.svc.Drill) }
func (a *failoverApi) release(w http.ResponseWriter, r *http.Request) {
	a.act(w, r, a.svc.Release)
}

func (a *failoverApi) activate(w http.ResponseWriter, r *http.Request) {
	id, ok := failoverId(w, r)
	if !ok {
		return
	}
	// automatic=false: this one has a person behind it, and the audit trail says so. The
	// sweep's own takeovers are the ones that would otherwise be indistinguishable.
	view, err := a.svc.Activate(r.Context(), id, failoverActor(r), false)
	if err != nil {
		a.sendErr(w, err)
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (a *failoverApi) act(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, id int64, actor int64) (*services.FailoverPlanView, error)) {
	id, ok := failoverId(w, r)
	if !ok {
		return
	}
	view, err := fn(r.Context(), id, failoverActor(r))
	if err != nil {
		a.sendErr(w, err)
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (a *failoverApi) sendErr(w http.ResponseWriter, err error) {
	if errors.Is(err, services.ErrFailoverPlanNotFound) {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendError(w, controllers.ErrBadRequest, err.Error())
}

// failoverActor is the signed-in user's id for the service's audit records. The shared
// auditActor helper also yields a label and a role that this service does not need.
func failoverActor(r *http.Request) int64 {
	id, _, _ := auditActor(r)
	return id
}

func failoverId(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}
