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

const healthSettingsKey = "health"

// HealthSettings is the runtime-editable camera health monitor configuration
// persisted in the database and edited from the Settings UI. The monitor reads
// it live on every sweep, so changes apply without a restart.
type HealthSettings struct {
	// Enabled turns the monitor on/off without restarting the app.
	Enabled bool `json:"enabled"`
	// IntervalMs is the gap between full reachability sweeps.
	IntervalMs int `json:"intervalMs"`
	// TimeoutMs is the per-probe deadline (TCP dial and RTSP deep-check).
	TimeoutMs int `json:"timeoutMs"`
	// FailureThreshold is the consecutive failed probes before declaring offline.
	FailureThreshold int `json:"failureThreshold"`
	// RecoveryThreshold is the consecutive successful probes before declaring online.
	RecoveryThreshold int `json:"recoveryThreshold"`
}

type healthSettingsService struct {
	repo     dbsql.IGenericRepo[entities.RuntimeSetting]
	defaults HealthSettings
}

// NewHealthSettingsService creates the camera health settings service, seeded
// with defaults from app config. Settings persist under a single "health" key
// in the same runtime_setting table used by decoder/vision/notification.
func NewHealthSettingsService(
	repo dbsql.IGenericRepo[entities.RuntimeSetting],
	defaults HealthSettings,
) IHealthSettingsService {
	return &healthSettingsService{repo: repo, defaults: normalizeHealthSettings(defaults)}
}

func (s *healthSettingsService) Get(ctx context.Context) (HealthSettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", healthSettingsKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return s.Save(ctx, s.defaults)
		}
		return HealthSettings{}, err
	}
	settings := HealthSettings{}
	if row != nil && strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &settings); err != nil {
			return HealthSettings{}, fmt.Errorf("parse health settings failed: %w", err)
		}
	}
	return normalizeHealthSettings(settings), nil
}

func (s *healthSettingsService) Save(ctx context.Context, settings HealthSettings) (HealthSettings, error) {
	settings = normalizeHealthSettings(settings)
	payload, err := json.Marshal(settings)
	if err != nil {
		return HealthSettings{}, err
	}
	now := time.Now().UTC().Unix()

	existing, err := s.repo.GetByUnique(ctx, "", "key", healthSettingsKey)
	if err == nil && existing != nil {
		existing.Value = string(payload)
		existing.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *existing); err != nil {
			return HealthSettings{}, err
		}
	} else if err != nil && !isNoResultFoundErr(err) {
		return HealthSettings{}, err
	} else if _, err := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key:       healthSettingsKey,
		Value:     string(payload),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return HealthSettings{}, err
	}
	return settings, nil
}

// normalizeHealthSettings clamps values to safe ranges so a bad save can never
// produce a runaway probe loop (e.g. a zero interval) or a too-eager flap.
func normalizeHealthSettings(s HealthSettings) HealthSettings {
	if s.IntervalMs < 5000 {
		s.IntervalMs = 30000
	}
	if s.TimeoutMs < 1000 {
		s.TimeoutMs = 5000
	}
	if s.TimeoutMs > 60000 {
		s.TimeoutMs = 60000
	}
	if s.FailureThreshold < 1 {
		s.FailureThreshold = 3
	}
	if s.RecoveryThreshold < 1 {
		s.RecoveryThreshold = 2
	}
	return s
}
