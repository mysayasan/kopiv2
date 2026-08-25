package apis

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// N+1 failover, the appliance's side (W3-7). See services/standby.go for the design.

type standbyApi struct {
	serv  services.IStandbyService
	audit *Auditor
}

// NewStandbyApi mounts the standby routes.
//
//	GET  /api/standby                        what this appliance holds, and for whom
//	GET  /api/standby/handoff-key            the one-exchange key a peer seals a set to
//	POST /api/standby/handoff                seal THIS appliance's cameras for a named spare
//	POST /api/standby/stage                  accept a sealed set
//	POST /api/standby/{nodeId}/drill         can this appliance actually open those cameras?
//	POST /api/standby/{nodeId}/activate      take the set over and start recording
//	POST /api/standby/{nodeId}/release       hand it back; keep the footage
//	POST /api/standby/{nodeId}/forget        drop a set this appliance no longer covers
//
// ADMINISTRATOR ONLY, at every level, and it is not in any grantable page. Two separate
// reasons, either of which would be enough. The handoff endpoint emits this appliance's
// entire camera set — sealed, but still: it is the one call that moves credentials off the
// box. And activation starts recording forty cameras belonging to another site, which is a
// decision about the ESTATE, not a shift task. In practice these are called by the control
// plane over the fleet tunnel, which asserts the admin role and has the node evaluate its
// own matrix (see NewControlDispatcher) — the same path fleet policy enforcement uses.
func NewStandbyApi(router *mux.Router, serv services.IStandbyService, audit *Auditor) {
	h := &standbyApi{serv: serv, audit: audit}
	g := router.PathPrefix("/standby").Subrouter()
	g.HandleFunc("", h.status).Methods("GET")
	g.HandleFunc("/handoff-key", h.handoffKey).Methods("GET")
	g.HandleFunc("/handoff", h.handoff).Methods("POST")
	g.HandleFunc("/stage", h.stage).Methods("POST")
	g.HandleFunc("/{nodeId}/drill", h.drill).Methods("POST")
	g.HandleFunc("/{nodeId}/activate", h.activate).Methods("POST")
	g.HandleFunc("/{nodeId}/release", h.release).Methods("POST")
	g.HandleFunc("/{nodeId}/forget", h.forget).Methods("POST")
}

func (a *standbyApi) status(w http.ResponseWriter, r *http.Request) {
	status, err := a.serv.Status(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, status, "succeed")
}

func (a *standbyApi) handoffKey(w http.ResponseWriter, r *http.Request) {
	key, err := a.serv.HandoffKey(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, key, "succeed")
}

func (a *standbyApi) handoff(w http.ResponseWriter, r *http.Request) {
	var body services.StandbyHandoffRequest
	if !decodeStandbyBody(w, r, &body) {
		return
	}
	res, err := a.serv.Handoff(r.Context(), body)
	if err != nil {
		a.audit.Failure(r, services.ActionStandbyHandoff, services.TargetStandby,
			body.RecipientNodeId, err.Error(), nil)
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// The COUNT is audited, never the contents. The trail's job here is to record that this
	// appliance's camera set left it and where it went; a trail that also carried the set
	// would be a second copy of the thing the sealing exists to protect.
	a.audit.Success(r, services.ActionStandbyHandoff, services.TargetStandby,
		body.RecipientNodeId,
		fmt.Sprintf("sealed %d camera(s) for standby appliance %s", res.CameraCount, body.RecipientNodeId),
		map[string]any{"recipientNodeId": body.RecipientNodeId, "cameraCount": res.CameraCount})
	controllers.SendResult(w, res, "succeed")
}

func (a *standbyApi) stage(w http.ResponseWriter, r *http.Request) {
	var body services.StandbyStageRequest
	if !decodeStandbyBody(w, r, &body) {
		return
	}
	set, err := a.serv.Stage(r.Context(), body)
	if err != nil {
		a.audit.Failure(r, services.ActionStandbyStage, services.TargetStandby, "", err.Error(), nil)
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionStandbyStage, services.TargetStandby, set.SourceNodeId,
		fmt.Sprintf("staged %d camera(s) from appliance %s", len(set.Cameras), standbyLabel(set)),
		map[string]any{"sourceNodeId": set.SourceNodeId, "cameraCount": len(set.Cameras)})
	controllers.SendResult(w, set, "succeed")
}

func (a *standbyApi) drill(w http.ResponseWriter, r *http.Request) {
	nodeId := strings.TrimSpace(mux.Vars(r)["nodeId"])
	set, err := a.serv.Drill(r.Context(), nodeId)
	if err != nil {
		a.sendSetError(w, r, services.ActionStandbyDrill, nodeId, err)
		return
	}
	a.audit.Success(r, services.ActionStandbyDrill, services.TargetStandby, nodeId,
		fmt.Sprintf("drilled the standby set from %s: %d of %d camera(s) reachable",
			standbyLabel(set), set.Reachable, set.Total),
		map[string]any{"sourceNodeId": nodeId, "reachable": set.Reachable, "total": set.Total,
			"readiness": set.Readiness})
	controllers.SendResult(w, set, "succeed")
}

func (a *standbyApi) activate(w http.ResponseWriter, r *http.Request) {
	nodeId := strings.TrimSpace(mux.Vars(r)["nodeId"])
	set, err := a.serv.Activate(r.Context(), nodeId)
	if err != nil {
		a.sendSetError(w, r, services.ActionStandbyActivate, nodeId, err)
		return
	}
	a.audit.Success(r, services.ActionStandbyActivate, services.TargetStandby, nodeId,
		fmt.Sprintf("took over %d camera(s) from appliance %s", len(set.Cameras), standbyLabel(set)),
		map[string]any{"sourceNodeId": nodeId, "cameraCount": len(set.Cameras),
			"outcomes": standbyOutcomes(set)})
	controllers.SendResult(w, set, "succeed")
}

func (a *standbyApi) release(w http.ResponseWriter, r *http.Request) {
	nodeId := strings.TrimSpace(mux.Vars(r)["nodeId"])
	set, err := a.serv.Release(r.Context(), nodeId)
	if err != nil {
		a.sendSetError(w, r, services.ActionStandbyRelease, nodeId, err)
		return
	}
	a.audit.Success(r, services.ActionStandbyRelease, services.TargetStandby, nodeId,
		fmt.Sprintf("handed %d camera(s) back to appliance %s; the footage recorded here stays",
			len(set.Cameras), standbyLabel(set)),
		map[string]any{"sourceNodeId": nodeId, "cameraCount": len(set.Cameras)})
	controllers.SendResult(w, set, "succeed")
}

func (a *standbyApi) forget(w http.ResponseWriter, r *http.Request) {
	nodeId := strings.TrimSpace(mux.Vars(r)["nodeId"])
	if err := a.serv.Forget(r.Context(), nodeId); err != nil {
		a.sendSetError(w, r, services.ActionStandbyForget, nodeId, err)
		return
	}
	a.audit.Success(r, services.ActionStandbyForget, services.TargetStandby, nodeId,
		fmt.Sprintf("stopped standing by for appliance %s", nodeId),
		map[string]any{"sourceNodeId": nodeId})
	controllers.SendResult(w, map[string]any{"sourceNodeId": nodeId}, "succeed")
}

// sendSetError maps a service error onto the right status. "nothing is staged for that
// appliance" is a 404 rather than a 400: the control plane's sweeper asks for sets it may
// not have created yet, and a 400 would read as a malformed request to whoever is looking
// at the log rather than as the ordinary answer it is.
func (a *standbyApi) sendSetError(w http.ResponseWriter, r *http.Request, action, nodeId string, err error) {
	a.audit.Failure(r, action, services.TargetStandby, nodeId, err.Error(),
		map[string]any{"sourceNodeId": nodeId})
	if errors.Is(err, services.ErrStandbyNoSuchSet) {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendError(w, controllers.ErrBadRequest, err.Error())
}

func decodeStandbyBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	// A sealed camera set for a large site is the biggest thing this API accepts, and it is
	// base64 — bounded well above the 512-camera cap the service enforces.
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return false
	}
	return true
}

func standbyLabel(set *services.StandbySet) string {
	if set == nil {
		return ""
	}
	if strings.TrimSpace(set.SourceNodeName) != "" {
		return set.SourceNodeName
	}
	return set.SourceNodeId
}

// standbyOutcomes flattens the per-camera result into the audit metadata. What actually
// happened per camera is the interesting part of a takeover — "took over 40 cameras" with
// six of them not recording is the entry that has to be answerable later.
func standbyOutcomes(set *services.StandbySet) map[string]string {
	if set == nil {
		return nil
	}
	out := map[string]string{}
	for _, cam := range set.Cameras {
		if cam.Outcome != "" {
			out[cam.Name] = cam.Outcome
		}
	}
	return out
}
