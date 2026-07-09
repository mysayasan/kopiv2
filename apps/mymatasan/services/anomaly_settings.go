package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

const anomalySettingsKey = "anomalyDetection"

// AnomalySettings is the runtime-editable configuration for the statistical
// anomaly monitor (Phase 3). The monitor reads it live each tick, so enabling or
// retuning it from the Settings UI takes effect without a restart. It is opt-in
// (disabled by default) because it only becomes meaningful once a few weeks of
// activity history have accumulated.
type AnomalySettings struct {
	// Enabled turns anomaly alerting on/off.
	Enabled bool `json:"enabled"`
	// Mode selects the detection tier: "smart" = the statistical per-camera baseline
	// (Sensitivity), "manual" = fixed site-wide hourly thresholds (ManualUpper/Lower).
	Mode string `json:"mode"`
	// ManualUpper/ManualLower are the fixed per-hour total-event thresholds used in
	// manual mode (0 disables that side): alert when the whole system's events in the
	// last hour exceed ManualUpper or fall below ManualLower.
	ManualUpper int64 `json:"manualUpper"`
	ManualLower int64 `json:"manualLower"`
	// Sensitivity is the band half-width in robust sigmas (k): lower = more
	// sensitive (more alerts), higher = only large deviations. Typical 2.0–3.0.
	Sensitivity float64 `json:"sensitivity"`
	// DetectHigh/DetectLow choose which breach directions alert. Low ("unusual
	// silence") is the high-value tamper/obstruction/offline signal.
	DetectHigh bool `json:"detectHigh"`
	DetectLow  bool `json:"detectLow"`
	// MinActivity suppresses "unusual silence" alerts for slots whose normal (median)
	// activity is below this, so genuinely-quiet hours are not flagged as silent.
	MinActivity float64 `json:"minActivity"`
	// RequireConsecutive is the number of consecutive anomalous hours (same camera +
	// direction) before the first alert — a debounce against one-off blips.
	RequireConsecutive int `json:"requireConsecutive"`
	// CooldownHours suppresses repeat alerts for the same camera+direction for this
	// long after one fires, so a sustained anomaly does not alert every hour.
	CooldownHours int `json:"cooldownHours"`
	// CheckIntervalMs is how often the monitor looks for a newly-closed hour to score.
	CheckIntervalMs int `json:"checkIntervalMs"`
}

type anomalySettingsService struct {
	repo     dbsql.IGenericRepo[entities.RuntimeSetting]
	defaults AnomalySettings
}

// NewAnomalySettingsService creates the anomaly settings service, persisted under
// a single "anomalyDetection" key in the shared runtime_setting table.
func NewAnomalySettingsService(repo dbsql.IGenericRepo[entities.RuntimeSetting], defaults AnomalySettings) IAnomalySettingsService {
	return &anomalySettingsService{repo: repo, defaults: normalizeAnomalySettings(defaults)}
}

func (s *anomalySettingsService) Get(ctx context.Context) (AnomalySettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", anomalySettingsKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return s.Save(ctx, s.defaults)
		}
		return AnomalySettings{}, err
	}
	settings := AnomalySettings{}
	if row != nil && strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &settings); err != nil {
			return AnomalySettings{}, fmt.Errorf("parse anomaly settings failed: %w", err)
		}
	}
	return normalizeAnomalySettings(settings), nil
}

func (s *anomalySettingsService) Save(ctx context.Context, settings AnomalySettings) (AnomalySettings, error) {
	settings = normalizeAnomalySettings(settings)
	payload, err := json.Marshal(settings)
	if err != nil {
		return AnomalySettings{}, err
	}
	now := time.Now().UTC().Unix()

	existing, err := s.repo.GetByUnique(ctx, "", "key", anomalySettingsKey)
	if err == nil && existing != nil {
		existing.Value = string(payload)
		existing.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *existing); err != nil {
			return AnomalySettings{}, err
		}
	} else if err != nil && !isNoResultFoundErr(err) {
		return AnomalySettings{}, err
	} else if _, err := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key:       anomalySettingsKey,
		Value:     string(payload),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return AnomalySettings{}, err
	}
	return settings, nil
}

// DefaultAnomalySettings returns the opt-in defaults: off, conservative k=3, both
// directions, a 5-minute check cadence, and anti-spam debounce/cooldown.
func DefaultAnomalySettings() AnomalySettings {
	return AnomalySettings{
		Enabled:            false,
		Mode:               "smart",
		ManualUpper:        0,
		ManualLower:        0,
		Sensitivity:        3.0,
		DetectHigh:         true,
		DetectLow:          true,
		MinActivity:        3,
		RequireConsecutive: 1,
		CooldownHours:      6,
		CheckIntervalMs:    300000,
	}
}

func normalizeAnomalySettings(s AnomalySettings) AnomalySettings {
	d := DefaultAnomalySettings()
	if s.Mode != "manual" {
		s.Mode = "smart"
	}
	if s.ManualUpper < 0 {
		s.ManualUpper = 0
	}
	if s.ManualLower < 0 {
		s.ManualLower = 0
	}
	if s.Sensitivity < 1.0 || s.Sensitivity > 6.0 {
		s.Sensitivity = d.Sensitivity
	}
	if s.MinActivity < 0 {
		s.MinActivity = d.MinActivity
	}
	s.RequireConsecutive = clampInt(s.RequireConsecutive, 1, 12)
	s.CooldownHours = clampInt(s.CooldownHours, 1, 168)
	s.CheckIntervalMs = normalizeInt(s.CheckIntervalMs, d.CheckIntervalMs, 30000, 3600000)
	return s
}
