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
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/recording"
)

type visionApi struct {
	serv      services.IVisionService
	classes   services.IDetectionClassService
	recorder  *recording.Manager
	notifier  services.INotificationService
	camera    services.ICameraService
	settings  services.IRuntimeSettingsService
	notifDest services.INotificationDestinationsProvider
	source    *services.DetectionSource
	cipher    *atrest.Cipher
	search    *services.SightingSearch
}

// NewVisionApi registers AI detection rule, alert, and class-registry routes.
func NewVisionApi(router *mux.Router, serv services.IVisionService, classes services.IDetectionClassService, recorder *recording.Manager, notifier services.INotificationService, camera services.ICameraService, settings services.IRuntimeSettingsService, notifDest services.INotificationDestinationsProvider, cipher *atrest.Cipher, search *services.SightingSearch) {
	handler := &visionApi{serv: serv, classes: classes, recorder: recorder, notifier: notifier, camera: camera, settings: settings, notifDest: notifDest, cipher: cipher, search: search,
		source: services.NewDetectionSource(camera, recorder, settings, nil)}
	group := router.PathPrefix("/vision").Subrouter()

	// The exact frame the detector samples for a camera (honors capture mode), so
	// the rule-editor draws zones/lines on the same pixels that get detected.
	group.HandleFunc("/cameras/{id}/frame", handler.detectionFrame).Methods("GET")
	group.HandleFunc("/rules", handler.listRules).Methods("GET")
	group.HandleFunc("/rules", handler.saveRule).Methods("POST")
	group.HandleFunc("/rules/{id}", handler.deleteRule).Methods("DELETE")
	group.HandleFunc("/alerts", handler.listAlerts).Methods("GET")
	// The identity half of federated fleet search (W2-4): plates and recognized faces,
	// which live on alert events rather than in the object-metadata index.
	//
	// It sits under /vision, NOT under /observations beside the object half, because that
	// is where its data lives and therefore which grant should govern it. A role that may
	// read object metadata but not the AI log must not learn who was recognized on this
	// appliance simply because the question was asked through the control plane.
	group.HandleFunc("/alerts/identities", handler.searchIdentities).Methods("GET")
	group.HandleFunc("/alerts", handler.createAlert).Methods("POST")
	group.HandleFunc("/alerts/purge", handler.purgeAlerts).Methods("POST")
	group.HandleFunc("/alerts/{id}/snapshot", handler.getAlertSnapshot).Methods("GET")
	group.HandleFunc("/alerts/{id}/ack", handler.acknowledgeAlert).Methods("POST")
	group.HandleFunc("/classes", handler.listClasses).Methods("GET")
	group.HandleFunc("/classes", handler.saveClass).Methods("POST")
	group.HandleFunc("/classes/{id}", handler.deleteClass).Methods("DELETE")
	group.HandleFunc("/labels", handler.listLabelCatalog).Methods("GET")
}

// searchIdentities answers one node's share of a federated identity search — "where has
// this plate / this person been seen".
func (a *visionApi) searchIdentities(w http.ResponseWriter, r *http.Request) {
	if a.search == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "search is unavailable")
		return
	}
	page, err := a.search.SearchIdentities(r.Context(), sightingQueryFromRequest(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, page, "succeed")
}

// detectionFrame returns, as a JPEG, the exact frame the AI detector would sample
// for the camera under the active capture mode (siphon / standalone / auto). The
// rule editor draws zones and lines on this so the geometry always matches what
// the detector actually sees.
func (a *visionApi) detectionFrame(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if a.source == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "detection source unavailable")
		return
	}
	frame, err := a.source.Capture(r.Context(), int64(id))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(frame.Data)
}

// listLabelCatalog returns every raw label the detector can emit, tagged with its
// source and group so the Object Classes picker can group + search at scale.
func (a *visionApi) listLabelCatalog(w http.ResponseWriter, r *http.Request) {
	if a.classes == nil {
		controllers.SendResult(w, map[string]any{"items": []any{}}, "succeed")
		return
	}
	items, err := a.classes.LabelCatalog(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *visionApi) listClasses(w http.ResponseWriter, r *http.Request) {
	if a.classes == nil {
		controllers.SendResult(w, map[string]any{"items": []any{}}, "succeed")
		return
	}
	items, err := a.classes.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *visionApi) saveClass(w http.ResponseWriter, r *http.Request) {
	if a.classes == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "class registry unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
	var body services.DetectionClassRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	class, err := a.classes.Save(r.Context(), body, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, class, "succeed")
}

func (a *visionApi) deleteClass(w http.ResponseWriter, r *http.Request) {
	if a.classes == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "class registry unavailable")
		return
	}
	id, ok := readID(w, r)
	if !ok {
		return
	}
	count, err := a.classes.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"deleted": count}, "succeed")
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
	status := r.URL.Query().Get("status")
	// The grid drives filtering + sorting server-side: `filters`/`sorters` query
	// params (DataTable format) are validated against AlertEvent's fields, so paging
	// runs over the true filtered set rather than a client-side slice.
	opts, err := sharedapis.ParseListQueryOptions[entities.AlertEvent](r)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	alerts, total, err := a.serv.GetAlerts(r.Context(), limit, offset, cameraId, status, opts.Filters, opts.Sorters)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"items": alerts,
		"total": total,
	}, "succeed")
}

// purgeAlerts deletes alert events older than the given number of days. Query
// params: days (int; <= 0 purges everything up to now) and onlyDiagnostics (bool;
// default false also removes real detections). Snapshot image files of the removed
// rows are unlinked. Returns the number of rows deleted.
func (a *visionApi) purgeAlerts(w http.ResponseWriter, r *http.Request) {
	days := int(parseInt64Query(r, "days"))
	onlyDiagnostics := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("onlyDiagnostics")), "true")
	deleted, err := a.serv.PurgeAlertsOlderThanDays(r.Context(), days, onlyDiagnostics)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]int{"deleted": deleted}, "succeed")
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
// API: it resolves the rule name, loads the raw snapshot frame from disk (each
// destination renders its own copy), and supplies the delivery destinations.
// This gives the manual create-alert path parity with the background monitor.
func (a *visionApi) alertOptions(ctx context.Context, alert *entities.AlertEvent) services.VisionAlertOptions {
	opts := services.VisionAlertOptions{}
	if alert == nil {
		return opts
	}
	name, ruleDests := a.ruleInfo(ctx, alert.RuleId)
	opts.RuleName = name
	opts.RuleDestinations = ruleDests
	if a.notifDest != nil {
		opts.Destinations = a.notifDest.Destinations(ctx)
	}
	// Load the raw frame once; renderVisionAlert annotates/strips/omits it per
	// each destination's own field config.
	if alert.SnapshotPath != "" {
		if data, err := a.readSnapshot(alert.SnapshotPath); err == nil {
			opts.RawImage = data
		}
	}
	return opts
}

// ruleName resolves a detection rule's name by id, or "" when unavailable.
func (a *visionApi) ruleName(ctx context.Context, ruleID int64) string {
	name, _ := a.ruleInfo(ctx, ruleID)
	return name
}

// ruleInfo resolves a detection rule's name and its per-rule destination routing
// (ruleConfig.destinations) by id. Returns ("", nil) when unavailable.
func (a *visionApi) ruleInfo(ctx context.Context, ruleID int64) (string, []string) {
	if a.serv == nil || ruleID <= 0 {
		return "", nil
	}
	rules, _, err := a.serv.GetRules(ctx, 1000, 0)
	if err != nil {
		return "", nil
	}
	for _, rule := range rules {
		if rule != nil && rule.Id == ruleID {
			return rule.Name, services.ParseRuleDestinations(rule.RuleConfig)
		}
	}
	return "", nil
}

// readSnapshot reads a stored alert snapshot, decrypting it when encryption-at-rest is
// enabled. Legacy plaintext snapshots (written before encryption) pass through unchanged.
func (a *visionApi) readSnapshot(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if a.cipher != nil {
		return a.cipher.DecryptBytes(data)
	}
	return data, nil
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
	data, err := a.readSnapshot(alert.SnapshotPath)
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
	userID := localUserID(r)
	alert, err := a.serv.AcknowledgeAlert(r.Context(), id, userID)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// Acknowledging the detection also dismisses its bell notification so the
	// unified feed can't keep showing an event the operator has already handled.
	if a.notifier != nil && alert != nil {
		_, _ = a.notifier.MarkReadByRef(r.Context(), "alert_event", alert.Id, userID)
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
