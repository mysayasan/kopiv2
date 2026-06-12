package apis

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/recording"
)

type visionApi struct {
	serv     services.IVisionService
	recorder *recording.Manager
}

// NewVisionApi registers AI detection rule and alert routes.
func NewVisionApi(router *mux.Router, serv services.IVisionService, recorder *recording.Manager) {
	handler := &visionApi{serv: serv, recorder: recorder}
	group := router.PathPrefix("/vision").Subrouter()

	group.HandleFunc("/rules", handler.listRules).Methods("GET")
	group.HandleFunc("/rules", handler.saveRule).Methods("POST")
	group.HandleFunc("/rules/{id}", handler.deleteRule).Methods("DELETE")
	group.HandleFunc("/alerts", handler.listAlerts).Methods("GET")
	group.HandleFunc("/alerts", handler.createAlert).Methods("POST")
	group.HandleFunc("/alerts/{id}/snapshot", handler.getAlertSnapshot).Methods("GET")
	group.HandleFunc("/alerts/{id}/ack", handler.acknowledgeAlert).Methods("POST")
}

func (a *visionApi) listRules(w http.ResponseWriter, r *http.Request) {
	limit, offset := readPaging(r)
	rules, total, err := a.serv.GetRules(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"items": rules,
		"total": total,
	}, "succeed")
}

func (a *visionApi) saveRule(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body services.DetectionRuleRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	rule, err := a.serv.SaveRule(r.Context(), body, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, rule, "succeed")
}

func (a *visionApi) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	count, err := a.serv.DeleteRule(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"deleted": count}, "succeed")
}

func (a *visionApi) listAlerts(w http.ResponseWriter, r *http.Request) {
	limit, offset := readPaging(r)
	cameraId := parseInt64Query(r, "cameraId")
	createdAfter := parseInt64Query(r, "createdAfter")
	createdBefore := parseInt64Query(r, "createdBefore")
	alerts, total, err := a.serv.GetAlerts(r.Context(), limit, offset, cameraId, createdAfter, createdBefore)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"items": alerts,
		"total": total,
	}, "succeed")
}

func (a *visionApi) createAlert(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body services.AlertEventRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	alert, err := a.serv.CreateAlert(r.Context(), body, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	if a.recorder != nil && alert != nil {
		a.recorder.TriggerEvent(alert.CameraId, alert.Id, 0)
	}
	controllers.SendResult(w, alert, "succeed")
}

func (a *visionApi) getAlertSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	alert, err := a.serv.GetAlertById(r.Context(), id)
	if err != nil || alert == nil || alert.SnapshotPath == "" {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(alert.SnapshotPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "max-age=86400, immutable")
	w.Write(data)
}

func (a *visionApi) acknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	alert, err := a.serv.AcknowledgeAlert(r.Context(), id, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, alert, "succeed")
}

func localUserID(r *http.Request) int64 {
	user, ok := LocalUserFromContext(r.Context())
	if !ok || user == nil {
		return 0
	}
	return user.Id
}
