package apis

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type discoveryApi struct {
	enroll  *services.Enrollment
	devices *services.DeviceService
	scan    *services.ScanService
}

// NewDiscoveryApi registers onboarding: the enrollment window, the candidates it collects, and
// adoption.
//
// Every route here is ADMIN-ONLY (see services.Policy). Opening the window is the one act in
// the whole app that lets an unknown thing talk to the broker at all, and an operator does not
// get to do it.
func NewDiscoveryApi(router *mux.Router, enroll *services.Enrollment, devices *services.DeviceService, scan *services.ScanService) {
	h := &discoveryApi{enroll: enroll, devices: devices, scan: scan}
	g := router.PathPrefix("/discovery").Subrouter()
	g.HandleFunc("/window", h.status).Methods("GET")
	g.HandleFunc("/window", h.open).Methods("POST")
	g.HandleFunc("/window", h.close).Methods("DELETE")
	// Active network scan — the counterpart to the announce/enroll window. Admin-only like the rest
	// of /discovery; its results land in the same candidate list.
	g.HandleFunc("/scan", h.runScan).Methods("POST")
	g.HandleFunc("/candidates", h.candidates).Methods("GET")
	g.HandleFunc("/candidates/{id}/adopt", h.adopt).Methods("POST")
	g.HandleFunc("/candidates/{id}", h.reject).Methods("DELETE")
}

func (a *discoveryApi) runScan(w http.ResponseWriter, r *http.Request) {
	var body services.ScanRequest
	if !decode(w, r, &body) {
		return
	}
	res, err := a.scan.Scan(r.Context(), body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *discoveryApi) status(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, a.enroll.Status(), "succeed")
}

type openWindowRequest struct {
	// Minutes the window stays open. Clamped: an hour is already generous for walking a
	// building, and a window measured in days is not a window.
	Minutes int `json:"minutes"`
}

func (a *discoveryApi) open(w http.ResponseWriter, r *http.Request) {
	var body openWindowRequest
	if !decode(w, r, &body) {
		return
	}
	ttl := time.Duration(body.Minutes) * time.Minute
	status, err := a.enroll.Open(r.Context(), ttl, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// The key is in this response and nowhere else. It is bcrypt-hashed at rest and cannot be
	// read back — reopening the window mints a new one.
	controllers.SendResult(w, status, "succeed")
}

func (a *discoveryApi) close(w http.ResponseWriter, r *http.Request) {
	a.enroll.Close()
	controllers.SendResult(w, a.enroll.Status(), "succeed")
}

func (a *discoveryApi) candidates(w http.ResponseWriter, r *http.Request) {
	rows, err := a.enroll.Candidates(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows}, "succeed")
}

func (a *discoveryApi) adopt(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body services.AdoptRequest
	if !decode(w, r, &body) {
		return
	}
	dev, err := a.enroll.Adopt(r.Context(), id, body, a.devices, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// Carries the device's real, permanent credential — shown once, never readable again.
	controllers.SendResult(w, dev, "succeed")
}

func (a *discoveryApi) reject(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if err := a.enroll.Reject(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"rejected": id}, "succeed")
}
