package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// PTZ presets, home and guard tours (W3-5). See services/ptz.go for the rules about who
// gets a camera when a person, an alarm and a patrol all want it.

type ptzApi struct {
	cameras services.ICameraService
	tours   services.IPTZService
	audit   *Auditor
}

// NewPTZApi mounts the preset and tour routes on the CAMERA subrouter (see NewCameraApi),
// under the existing PTZ path.
//
//	GET    /api/cameras/{id}/ptz/presets                  what the camera has stored
//	POST   /api/cameras/{id}/ptz/presets                  save the current position
//	POST   /api/cameras/{id}/ptz/presets/{token}/goto     recall one
//	POST   /api/cameras/{id}/ptz/presets/{token}/delete   remove one
//	GET    /api/cameras/{id}/ptz/status                   where the camera is now
//	POST   /api/cameras/{id}/ptz/home                     go home
//	POST   /api/cameras/{id}/ptz/home/set                 make here home
//	GET    /api/cameras/{id}/ptz/tours                    this camera's patrols
//	POST   /api/cameras/{id}/ptz/tours                    create
//	POST   /api/cameras/{id}/ptz/tours/{tourId}           update
//	POST   /api/cameras/{id}/ptz/tours/{tourId}/start     begin patrolling
//	POST   /api/cameras/{id}/ptz/tours/{tourId}/stop      stop
//	POST   /api/cameras/{id}/ptz/tours/{tourId}/delete    remove
//
// UNDER /ptz on purpose. The role model already expresses "may move a camera" as
// `/api/cameras/*/ptz`, granted a rung above watching. Everything here moves a camera or
// decides when a camera moves, so hanging it off the same prefix means the capability an
// administrator already granted keeps meaning what its label says — rather than a new
// top-level path that every role would silently lack, or, worse, that a broad grant would
// silently hand out.
//
// Deleting is a POST for the same reason it is on the walls: the appliance role model keeps
// the DELETE verb out of every grantable level so that destroying footage needs an
// administrator, and a preset is not footage.
func NewPTZApi(cameraGroup *mux.Router, cameras services.ICameraService, tours services.IPTZService, audit *Auditor) {
	h := &ptzApi{cameras: cameras, tours: tours, audit: audit}
	g := cameraGroup
	g.HandleFunc("/{id}/ptz/presets", h.listPresets).Methods("GET")
	g.HandleFunc("/{id}/ptz/presets", h.savePreset).Methods("POST")
	g.HandleFunc("/{id}/ptz/presets/{token}/goto", h.gotoPreset).Methods("POST")
	g.HandleFunc("/{id}/ptz/presets/{token}/delete", h.deletePreset).Methods("POST")
	g.HandleFunc("/{id}/ptz/status", h.status).Methods("GET")
	g.HandleFunc("/{id}/ptz/home", h.home).Methods("POST")
	g.HandleFunc("/{id}/ptz/home/set", h.setHome).Methods("POST")
	g.HandleFunc("/{id}/ptz/tours", h.listTours).Methods("GET")
	g.HandleFunc("/{id}/ptz/tours", h.createTour).Methods("POST")
	g.HandleFunc("/{id}/ptz/tours/{tourId}", h.updateTour).Methods("POST")
	g.HandleFunc("/{id}/ptz/tours/{tourId}/start", h.startTour).Methods("POST")
	g.HandleFunc("/{id}/ptz/tours/{tourId}/stop", h.stopTour).Methods("POST")
	g.HandleFunc("/{id}/ptz/tours/{tourId}/delete", h.deleteTour).Methods("POST")
}

type ptzPresetBody struct {
	Name string `json:"name"`
	// Token, when set, overwrites that preset instead of creating a new one.
	Token string `json:"token"`
}

type ptzGotoBody struct {
	Speed float64 `json:"speed"`
}

type ptzTourBody struct {
	Name         string                 `json:"name"`
	DwellSeconds int                    `json:"dwellSeconds"`
	Stops        []services.PTZTourStop `json:"stops"`
}

func (a *ptzApi) listPresets(w http.ResponseWriter, r *http.Request) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	presets, err := a.cameras.PTZPresets(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"presets": presets}, "succeed")
}

func (a *ptzApi) savePreset(w http.ResponseWriter, r *http.Request) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var body ptzPresetBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	token, err := a.cameras.PTZSavePreset(r.Context(), id, body.Name, body.Token)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	verb := "saved"
	if strings.TrimSpace(body.Token) != "" {
		verb = "replaced"
	}
	// Audited because a preset is where an alarm will send this camera. Quietly moving
	// "Front gate" to point at the sky is a way to make an alarm useless that leaves the
	// rule, the tour and the screen all looking correct.
	a.audit.Success(r, services.ActionPTZPresetSave, services.TargetCamera,
		strconv.FormatUint(id, 10),
		fmt.Sprintf("%s the PTZ preset %q", verb, strings.TrimSpace(body.Name)),
		map[string]any{"presetToken": token, "presetName": strings.TrimSpace(body.Name)})
	controllers.SendResult(w, map[string]any{"token": token}, "succeed")
}

func (a *ptzApi) gotoPreset(w http.ResponseWriter, r *http.Request) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	token := mux.Vars(r)["token"]
	var body ptzGotoBody
	// A speed is optional; an empty body is the ordinary case and must not be an error.
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4*1024)).Decode(&body)
	if err := a.cameras.PTZGotoPreset(r.Context(), id, token, body.Speed); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"moved": true}, "succeed")
}

func (a *ptzApi) deletePreset(w http.ResponseWriter, r *http.Request) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	token := mux.Vars(r)["token"]
	if err := a.cameras.PTZDeletePreset(r.Context(), id, token); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionPTZPresetDelete, services.TargetCamera,
		strconv.FormatUint(id, 10),
		fmt.Sprintf("deleted the PTZ preset %q", token),
		map[string]any{"presetToken": token})
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

func (a *ptzApi) status(w http.ResponseWriter, r *http.Request) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	status, err := a.cameras.PTZStatus(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, status, "succeed")
}

func (a *ptzApi) home(w http.ResponseWriter, r *http.Request) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	if err := a.cameras.PTZHome(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"moved": true}, "succeed")
}

func (a *ptzApi) setHome(w http.ResponseWriter, r *http.Request) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	if err := a.cameras.PTZSetHome(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionPTZHomeSet, services.TargetCamera,
		strconv.FormatUint(id, 10), "set the PTZ home position", nil)
	controllers.SendResult(w, map[string]any{"saved": true}, "succeed")
}

func (a *ptzApi) listTours(w http.ResponseWriter, r *http.Request) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	tours, err := a.tours.Tours(r.Context(), int64(id))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"tours": tours}, "succeed")
}

func (a *ptzApi) createTour(w http.ResponseWriter, r *http.Request) { a.saveTour(w, r, 0) }

func (a *ptzApi) updateTour(w http.ResponseWriter, r *http.Request) {
	a.saveTour(w, r, tourIdVar(r))
}

func (a *ptzApi) saveTour(w http.ResponseWriter, r *http.Request, tourId int64) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body ptzTourBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	actorID, actorName, _ := auditActor(r)
	tour, err := a.tours.SaveTour(r.Context(), services.PTZTourSave{
		Id: tourId, CameraId: int64(id), Name: body.Name,
		Stops: body.Stops, DwellSeconds: body.DwellSeconds,
		Actor: services.CaseActor{Id: actorID, Name: actorName},
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	verb := "created"
	if tourId > 0 {
		verb = "changed"
	}
	a.audit.Success(r, services.ActionPTZTourChange, services.TargetCamera,
		strconv.FormatUint(id, 10),
		fmt.Sprintf("%s the guard tour %q (%d stops)", verb, tour.Name, len(tour.Stops)),
		map[string]any{"tourId": tour.Id, "tourName": tour.Name, "stops": tour.Stops,
			"dwellSeconds": tour.DwellSeconds})
	controllers.SendResult(w, tour, "succeed")
}

func (a *ptzApi) startTour(w http.ResponseWriter, r *http.Request) { a.setTourRunning(w, r, true) }

func (a *ptzApi) stopTour(w http.ResponseWriter, r *http.Request) { a.setTourRunning(w, r, false) }

func (a *ptzApi) setTourRunning(w http.ResponseWriter, r *http.Request, running bool) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	tour, err := a.tours.SetTourRunning(r.Context(), tourIdVar(r), running)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	verb := "stopped"
	if running {
		verb = "started"
	}
	// Audited: "why was this camera not looking at the door" is answered by who stopped
	// its patrol and when.
	a.audit.Success(r, services.ActionPTZTourRun, services.TargetCamera,
		strconv.FormatUint(id, 10),
		fmt.Sprintf("%s the guard tour %q", verb, tour.Name),
		map[string]any{"tourId": tour.Id, "tourName": tour.Name, "running": running})
	controllers.SendResult(w, tour, "succeed")
}

func (a *ptzApi) deleteTour(w http.ResponseWriter, r *http.Request) {
	id, ok := a.cameraId(w, r)
	if !ok {
		return
	}
	tourId := tourIdVar(r)
	name := ""
	if existing, err := a.tours.Tour(r.Context(), tourId); err == nil && existing != nil {
		name = existing.Name
	}
	if err := a.tours.DeleteTour(r.Context(), tourId); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionPTZTourDelete, services.TargetCamera,
		strconv.FormatUint(id, 10),
		strings.TrimSpace(fmt.Sprintf("deleted the guard tour %q", name)),
		map[string]any{"tourId": tourId, "tourName": name})
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

func (a *ptzApi) cameraId(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	if err != nil || id == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid camera id")
		return 0, false
	}
	return id, true
}

func tourIdVar(r *http.Request) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)["tourId"], 10, 64)
	return id
}
