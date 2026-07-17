package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type schedulesApi struct {
	schedules *services.ScheduleService
}

// NewSchedulesApi registers schedules (time and sunrise/sunset triggers) and the site location the
// sun triggers need.
//
// Authoring a schedule, TEST-firing it, and setting the site location are all admin-only in the RBAC
// matrix (services.Policy) — a schedule commands real devices, and its location is the input that
// decides when. Listing them is readable by anyone signed in.
func NewSchedulesApi(router *mux.Router, schedules *services.ScheduleService) {
	h := &schedulesApi{schedules: schedules}

	g := router.PathPrefix("/schedules").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	g.HandleFunc("/{id}", h.update).Methods("PUT")
	g.HandleFunc("/{id}", h.remove).Methods("DELETE")
	g.HandleFunc("/{id}/run", h.run).Methods("POST")

	// Site latitude/longitude, used to compute sunrise/sunset. Admin-only (see Policy).
	router.HandleFunc("/settings/location", h.getLocation).Methods("GET")
	router.HandleFunc("/settings/location", h.setLocation).Methods("PUT")
}

func (a *schedulesApi) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.schedules.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows}, "succeed")
}

func (a *schedulesApi) create(w http.ResponseWriter, r *http.Request) {
	var body services.SaveScheduleRequest
	if !decode(w, r, &body) {
		return
	}
	sched, err := a.schedules.Create(r.Context(), body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, sched, "succeed")
}

func (a *schedulesApi) update(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body services.SaveScheduleRequest
	if !decode(w, r, &body) {
		return
	}
	sched, err := a.schedules.Update(r.Context(), id, body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, sched, "succeed")
}

func (a *schedulesApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if err := a.schedules.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

func (a *schedulesApi) run(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if err := a.schedules.RunNow(r.Context(), id, actorId(r), actorName(r)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"fired": id}, "succeed")
}

func (a *schedulesApi) getLocation(w http.ResponseWriter, r *http.Request) {
	lat, lon, set := a.schedules.GetLocation(r.Context())
	controllers.SendResult(w, map[string]any{"latitude": lat, "longitude": lon, "set": set}, "succeed")
}

func (a *schedulesApi) setLocation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := a.schedules.SetLocation(r.Context(), body.Latitude, body.Longitude, actorId(r)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"latitude": body.Latitude, "longitude": body.Longitude}, "succeed")
}
