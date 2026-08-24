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

// Privacy zones (W3-6). See services/privacy.go for the difference between a region the
// CAMERA masks and one only the exports redact — which is the whole feature.

type privacyApi struct {
	serv  services.IPrivacyService
	audit *Auditor
}

// NewPrivacyApi mounts the privacy-zone routes on the CAMERA subrouter.
//
//	GET  /api/cameras/{id}/privacy               this camera's zones and what it can enforce
//	POST /api/cameras/{id}/privacy               create
//	POST /api/cameras/{id}/privacy/{zoneId}      update
//	POST /api/cameras/{id}/privacy/{zoneId}/delete
//	POST /api/cameras/{id}/privacy/apply         re-push and re-verify against the camera
//
// Its own path rather than a corner of /ptz or /relays, and governed as CAMERA
// ADMINISTRATION rather than as an operator capability: deciding what is never recorded is
// a policy decision about the site, not a thing done during a shift. An operator who can
// draw a privacy zone can also un-draw one, and doing that quietly is how footage that was
// supposed to be protected stops being.
func NewPrivacyApi(cameraGroup *mux.Router, serv services.IPrivacyService, audit *Auditor) {
	h := &privacyApi{serv: serv, audit: audit}
	cameraGroup.HandleFunc("/{id}/privacy", h.list).Methods("GET")
	cameraGroup.HandleFunc("/{id}/privacy", h.create).Methods("POST")
	cameraGroup.HandleFunc("/{id}/privacy/apply", h.apply).Methods("POST")
	cameraGroup.HandleFunc("/{id}/privacy/{zoneId}", h.update).Methods("POST")
	cameraGroup.HandleFunc("/{id}/privacy/{zoneId}/delete", h.remove).Methods("POST")
}

type privacyZoneBody struct {
	Name    string       `json:"name"`
	Points  [][2]float64 `json:"points"`
	Style   string       `json:"style"`
	Enabled bool         `json:"enabled"`
}

func (a *privacyApi) list(w http.ResponseWriter, r *http.Request) {
	id, ok := privacyCameraId(w, r)
	if !ok {
		return
	}
	zones, err := a.serv.Zones(r.Context(), int64(id))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// The STATUS rides with the list, always. A screen that shows zones without saying
	// whether the camera is enforcing them is a screen that implies the strong claim, and
	// the difference between "not recorded" and "not exported" is the entire point of the
	// feature.
	status, err := a.serv.Status(r.Context(), int64(id))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"zones": zones, "status": status}, "succeed")
}

func (a *privacyApi) create(w http.ResponseWriter, r *http.Request) { a.save(w, r, 0) }

func (a *privacyApi) update(w http.ResponseWriter, r *http.Request) {
	a.save(w, r, privacyZoneIdVar(r))
}

func (a *privacyApi) save(w http.ResponseWriter, r *http.Request, zoneId int64) {
	id, ok := privacyCameraId(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body privacyZoneBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	actorID, actorName, _ := auditActor(r)
	zone, err := a.serv.SaveZone(r.Context(), services.PrivacyZoneSave{
		Id: zoneId, CameraId: int64(id), Name: body.Name, Points: body.Points,
		Style: body.Style, Enabled: body.Enabled,
		Actor: services.CaseActor{Id: actorID, Name: actorName},
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	verb := "created"
	if zoneId > 0 {
		verb = "changed"
	}
	// Audited because a privacy zone is a promise to somebody who is not in the room —
	// the neighbour, the person at the counter — and changing or disabling one silently
	// is how that promise stops being kept without anybody noticing.
	a.audit.Success(r, services.ActionPrivacyZoneChange, services.TargetCamera,
		strconv.FormatUint(id, 10),
		fmt.Sprintf("%s the privacy zone %q (%s)", verb, zone.Name,
			map[bool]string{true: "active", false: "switched off"}[zone.Enabled]),
		map[string]any{"zoneId": zone.Id, "name": zone.Name, "enabled": zone.Enabled, "style": zone.Style})
	controllers.SendResult(w, zone, "succeed")
}

func (a *privacyApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := privacyCameraId(w, r)
	if !ok {
		return
	}
	zoneId := privacyZoneIdVar(r)
	name := ""
	if zones, err := a.serv.Zones(r.Context(), int64(id)); err == nil {
		for _, zone := range zones {
			if zone.Id == zoneId {
				name = zone.Name
			}
		}
	}
	if err := a.serv.DeleteZone(r.Context(), zoneId); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionPrivacyZoneDelete, services.TargetCamera,
		strconv.FormatUint(id, 10),
		strings.TrimSpace(fmt.Sprintf("removed the privacy zone %q", name)),
		map[string]any{"zoneId": zoneId, "name": name})
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

func (a *privacyApi) apply(w http.ResponseWriter, r *http.Request) {
	id, ok := privacyCameraId(w, r)
	if !ok {
		return
	}
	// Re-push and re-verify. Zones are applied on save already; this exists because a
	// camera that was offline, rebooted or factory-reset since then will have lost its
	// masks, and there has to be a way to find that out and fix it that does not involve
	// editing a zone to trigger a side effect.
	status, err := a.serv.Apply(r.Context(), int64(id))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, status, "succeed")
}

func privacyCameraId(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	if err != nil || id == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid camera id")
		return 0, false
	}
	return id, true
}

func privacyZoneIdVar(r *http.Request) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)["zoneId"], 10, 64)
	return id
}
