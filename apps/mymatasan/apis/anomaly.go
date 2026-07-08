package apis

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type anomalyApi struct {
	settings services.IAnomalySettingsService
	scanner  services.IAnomalyScanner
	camera   services.ICameraService
}

// NewAnomalyApi registers the statistical anomaly monitor's config + preview routes
// under /anomaly.
func NewAnomalyApi(router *mux.Router, settings services.IAnomalySettingsService, scanner services.IAnomalyScanner, camera services.ICameraService) {
	h := &anomalyApi{settings: settings, scanner: scanner, camera: camera}
	g := router.PathPrefix("/anomaly").Subrouter()
	g.HandleFunc("/settings", h.getSettings).Methods("GET")
	g.HandleFunc("/settings", h.saveSettings).Methods("PUT")
	g.HandleFunc("/scan", h.scan).Methods("GET")
}

func (a *anomalyApi) getSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := a.settings.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, cfg, "succeed")
}

func (a *anomalyApi) saveSettings(w http.ResponseWriter, r *http.Request) {
	var payload services.AnomalySettings
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	saved, err := a.settings.Save(r.Context(), payload)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, saved, "succeed")
}

// scan runs an on-demand anomaly scan of one closed hour (defaults to the most
// recently completed hour), returning the findings enriched with camera names so
// the Settings UI can preview what would alert with the current sensitivity.
func (a *anomalyApi) scan(w http.ResponseWriter, r *http.Request) {
	cfg, err := a.settings.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC().Unix()
	hour := parseInt64Query(r, "hour")
	if hour <= 0 {
		hour = now - now%3600 - 3600
	}
	tzOffsetMin := parseInt64Query(r, "tzOffset")
	if tzOffsetMin < -840 || tzOffsetMin > 840 {
		tzOffsetMin = 0
	}

	findings, err := a.scanner.AnomalyScan(r.Context(), hour, tzOffsetMin*60, cfg.Sensitivity, cfg.MinActivity)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	items := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		items = append(items, map[string]any{
			"cameraId":   f.CameraId,
			"cameraName": a.camera.DisplayName(r.Context(), f.CameraId),
			"direction":  f.Direction,
			"actual":     f.Actual,
			"lo":         f.Lo,
			"hi":         f.Hi,
			"median":     f.Mid,
			"samples":    f.Samples,
			"hourStart":  f.HourStart,
		})
	}
	controllers.SendResult(w, map[string]any{"hourStart": hour, "findings": items}, "succeed")
}
