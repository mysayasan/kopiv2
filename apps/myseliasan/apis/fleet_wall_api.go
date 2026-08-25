package apis

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type fleetWallApi struct {
	svc     services.IFleetWallService
	session *middlewares.AccessSessionMidware
}

// NewFleetWallApi registers fleet video walls (W3-3d) — saved camera arrangements that span
// appliances.
//
// READING follows the permission matrix: a wall is what a control room watches, and the people
// who watch it are exactly the people who should not need an administrator to open it.
//
// WRITING is superadmin-only. A wall is SHARED — changing one changes what everybody on that
// screen sees, including the person who is mid-shift and did not change anything — and the
// default wall decides what a guard station opens with when nobody chooses. That is an estate
// decision, on the same reasoning as a fleet policy.
func NewFleetWallApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, svc services.IFleetWallService) {
	h := &fleetWallApi{svc: svc, session: session}
	g := router.PathPrefix("/fleet-walls").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	// The layouts the server will accept, so a client offers exactly those instead of
	// discovering the answer through a 400.
	g.HandleFunc("/grids", h.grids).Methods("GET")
	g.HandleFunc("/{id}", h.get).Methods("GET")
	g.HandleFunc("", h.requireSuper(h.save)).Methods("POST")
	g.HandleFunc("/{id}", h.requireSuper(h.remove)).Methods("DELETE")
}

func (a *fleetWallApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.session == nil || !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmins only")
			return
		}
		next(w, r)
	}
}

func (a *fleetWallApi) list(w http.ResponseWriter, r *http.Request) {
	items, err := a.svc.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *fleetWallApi) grids(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, map[string]any{"grids": a.svc.Grids()}, "succeed")
}

func (a *fleetWallApi) get(w http.ResponseWriter, r *http.Request) {
	id, ok := fleetWallId(w, r)
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

func (a *fleetWallApi) save(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body services.SaveFleetWallRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	actor, name, _ := auditActor(r)
	view, err := a.svc.Save(r.Context(), body, actor, name)
	if err != nil {
		a.sendErr(w, err)
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (a *fleetWallApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := fleetWallId(w, r)
	if !ok {
		return
	}
	actor, _, _ := auditActor(r)
	if err := a.svc.Delete(r.Context(), id, actor); err != nil {
		a.sendErr(w, err)
		return
	}
	controllers.SendResult(w, map[string]any{"id": id}, "succeed")
}

func (a *fleetWallApi) sendErr(w http.ResponseWriter, err error) {
	if errors.Is(err, services.ErrFleetWallNotFound) {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendError(w, controllers.ErrBadRequest, err.Error())
}

func fleetWallId(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}
