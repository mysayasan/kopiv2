package apis

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// Relay outputs (W3-5b). See services/relay.go for why every route here funnels into one
// Fire() call, and why switching something OFF is never refused.

type relayApi struct {
	relays services.IRelayService
}

// NewRelayApi mounts the relay routes on the CAMERA subrouter.
//
//	GET  /api/cameras/{id}/relays              what outputs this camera has
//	POST /api/cameras/{id}/relays/{token}/fire switch one
//
// A SEPARATE PATH FROM /ptz, deliberately. The role model grants `/api/cameras/*/ptz` to an
// operator as "may move a camera", and switching a siren, a gate or a door strike is not
// the same capability wearing a different name — a control room operator who may point a
// camera is not automatically somebody who may open a gate. Giving it its own path is what
// makes the two grantable apart. See services/rbac.go.
func NewRelayApi(cameraGroup *mux.Router, relays services.IRelayService) {
	h := &relayApi{relays: relays}
	cameraGroup.HandleFunc("/{id}/relays", h.list).Methods("GET")
	cameraGroup.HandleFunc("/{id}/relays/{token}/fire", h.fire).Methods("POST")
}

type relayFireBody struct {
	// Action is pulse, on or off. Empty means pulse.
	Action string `json:"action"`
	// PulseSeconds applies to a pulse; 0 uses the default.
	PulseSeconds int `json:"pulseSeconds"`
	// Reason is what the operator was doing, recorded in the trail.
	Reason string `json:"reason"`
}

func (a *relayApi) list(w http.ResponseWriter, r *http.Request) {
	id, ok := relayCameraId(w, r)
	if !ok {
		return
	}
	relays, err := a.relays.Relays(r.Context(), int64(id))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"relays": relays}, "succeed")
}

func (a *relayApi) fire(w http.ResponseWriter, r *http.Request) {
	id, ok := relayCameraId(w, r)
	if !ok {
		return
	}
	var body relayFireBody
	// An empty body is a pulse with the default hold — the ordinary case for a button, and
	// it must not be an error.
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&body)

	// Not audited here. The relay service audits every actuation itself, including the
	// automatic ones that never pass through a handler — putting a second record at this
	// layer would double-count the manual ones and still miss the rest.
	if err := a.relays.Fire(r.Context(), services.RelayFireRequest{
		CameraId:     int64(id),
		Token:        mux.Vars(r)["token"],
		Action:       body.Action,
		PulseSeconds: body.PulseSeconds,
		Reason:       body.Reason,
	}); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"switched": true}, "succeed")
}

func relayCameraId(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	if err != nil || id == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid camera id")
		return 0, false
	}
	return id, true
}
