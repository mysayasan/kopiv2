package apis

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/recording"
)

type visionApi struct {
	serv     services.IVisionService
	recorder *recording.Manager
	notifier services.INotificationPublisher
	camera   services.ICameraService
	settings services.IRuntimeSettingsService
}

// NewVisionApi registers AI detection rule and alert routes.
func NewVisionApi(router *mux.Router, serv services.IVisionService, recorder *recording.Manager, notifier services.INotificationPublisher, camera services.ICameraService, settings services.IRuntimeSettingsService) {
	handler := &visionApi{serv: serv, recorder: recorder, notifier: notifier, camera: camera, settings: settings}
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
	ruleId := parseInt64Query(r, "ruleId")
	status := r.URL.Query().Get("status")
	alerts, total, err := a.serv.GetAlerts(r.Context(), limit, offset, cameraId, createdAfter, createdBefore, ruleId, status)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"items": alerts,
		"total": total,
	}, "succeed")
}

// cameraName resolves the alert's camera display name, returning "" so the
// notification falls back to "Camera <id>" when it cannot be resolved.
func (a *visionApi) cameraName(ctx context.Context, alert *entities.AlertEvent) string {
	if a.camera == nil || alert == nil {
		return ""
	}
	return a.camera.DisplayName(ctx, alert.CameraId)
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
	services.NotifyVisionAlert(r.Context(), a.notifier, alert, a.cameraName(r.Context(), alert), a.alertOptions(r.Context(), alert))
	controllers.SendResult(w, alert, "succeed")
}

// alertOptions builds the notification options for an alert raised through the
// API: it applies the configured field inclusion, resolves the rule name, and
// loads the snapshot image from disk when the snapshot field is enabled. This
// gives the manual create-alert path parity with the background monitor.
func (a *visionApi) alertOptions(ctx context.Context, alert *entities.AlertEvent) services.VisionAlertOptions {
	opts := services.VisionAlertOptions{}
	if alert == nil {
		return opts
	}
	fields := a.alertNotificationFields(ctx)
	opts.Fields = fields
	opts.RuleName = a.ruleName(ctx, alert.RuleId)
	includeSnapshot := fields == nil || fields.IncludeSnapshot
	if includeSnapshot && alert.SnapshotPath != "" {
		if data, err := os.ReadFile(alert.SnapshotPath); err == nil {
			// Draw the detection box onto the image (when configured) so the
			// notification matches the AI Log detail overlay.
			opts.Snapshot = services.BuildAlertSnapshot(data, alert.BoundingBox, alert.Metadata, alert.DetectionType, fields)
		}
	}
	return opts
}

// alertNotificationFields reads the runtime-editable field-inclusion config. A
// nil return means "include everything" (matching NotifyVisionAlert's default).
func (a *visionApi) alertNotificationFields(ctx context.Context) *services.AlertNotificationSettings {
	if a.settings == nil {
		return nil
	}
	settings, err := a.settings.Get(ctx)
	if err != nil {
		return nil
	}
	return settings.Vision.AlertNotification
}

// ruleName resolves a detection rule's name by id, or "" when unavailable.
func (a *visionApi) ruleName(ctx context.Context, ruleID int64) string {
	if a.serv == nil || ruleID <= 0 {
		return ""
	}
	rules, _, err := a.serv.GetRules(ctx, 1000, 0)
	if err != nil {
		return ""
	}
	for _, rule := range rules {
		if rule != nil && rule.Id == ruleID {
			return rule.Name
		}
	}
	return ""
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
	// Opt-in ?annotated=1 returns the snapshot with the detection box + label drawn
	// in (matching the notification image), so downloads/shares carry the box. The
	// default stays raw so the Log detail view can render its own crisp overlay.
	// Falls back to the raw image when the alert has no bounding box.
	if isTruthyParam(r.URL.Query().Get("annotated")) {
		data = services.BuildAlertSnapshot(data, alert.BoundingBox, alert.Metadata, alert.DetectionType, nil)
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "max-age=86400, immutable")
	w.Write(data)
}

// isTruthyParam reports whether a query parameter value means "on".
func isTruthyParam(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
