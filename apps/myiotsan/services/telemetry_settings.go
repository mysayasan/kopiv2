package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

const telemetrySettingsKey = "telemetry"

// TelemetrySettings is the runtime-editable storage/broker configuration. Every field here is read
// ONCE at startup (the write-batcher is sized at construction, the retention loop closes over its
// config, the MQTT listener binds once), so an edit takes effect only after a RESTART. The UI says
// so plainly, and offers the restart. Persisted as one JSON blob in the shared RuntimeSetting KV.
type TelemetrySettings struct {
	// Retention (days). RawRetentionDays trims the per-sample table; RollupRetentionDays the
	// hourly rollups. The deadband keeps the raw table small, so these can be generous.
	RawRetentionDays    int `json:"rawRetentionDays"`
	RollupRetentionDays int `json:"rollupRetentionDays"`
	// Write-batcher: how readings are flushed to the database off the hot path.
	BatchSize int `json:"batchSize"`
	FlushMs   int `json:"flushMs"`
	QueueSize int `json:"queueSize"`
	// MqttAddr is the embedded broker's listen address (host:port). Changing it can cut every
	// device off if wrong, so it is edited deliberately and applied on restart.
	MqttAddr string `json:"mqttAddr"`
}

// TelemetrySettingsService persists TelemetrySettings, seeded from config.json defaults the first
// time. It has no live-apply side effect (unlike the notification store): the values are consumed at
// boot, so Save just persists and the caller restarts to apply.
type TelemetrySettingsService struct {
	repo     dbsql.IGenericRepo[sharedentities.RuntimeSetting]
	defaults TelemetrySettings
}

func NewTelemetrySettingsService(db dbsql.IDbCrud, defaults TelemetrySettings) *TelemetrySettingsService {
	return &TelemetrySettingsService{
		repo:     dbsql.NewGenericRepo[sharedentities.RuntimeSetting](db),
		defaults: defaults,
	}
}

// Get returns the effective settings: the stored blob if present, else the config-derived defaults.
// This is what app.go reads at boot to feed the writer, the rollup loop and the broker.
func (s *TelemetrySettingsService) Get(ctx context.Context) (TelemetrySettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", telemetrySettingsKey)
	if err != nil {
		if isNoResultErr(err) {
			return s.defaults, nil
		}
		return TelemetrySettings{}, err
	}
	if row == nil || strings.TrimSpace(row.Value) == "" {
		return s.defaults, nil
	}
	out := TelemetrySettings{}
	if err := json.Unmarshal([]byte(row.Value), &out); err != nil {
		return TelemetrySettings{}, fmt.Errorf("parse telemetry settings: %w", err)
	}
	return withTelemetryDefaults(out, s.defaults), nil
}

// Save validates and persists. It does NOT apply anything live — the caller restarts to take effect.
func (s *TelemetrySettingsService) Save(ctx context.Context, settings TelemetrySettings) (TelemetrySettings, error) {
	settings = withTelemetryDefaults(settings, s.defaults)
	if err := validateTelemetrySettings(settings); err != nil {
		return TelemetrySettings{}, err
	}
	payload, _ := json.Marshal(settings)
	now := time.Now().Unix()

	row, err := s.repo.GetByUnique(ctx, "", "key", telemetrySettingsKey)
	if err != nil && !isNoResultErr(err) {
		return TelemetrySettings{}, err
	}
	if row == nil {
		_, err = s.repo.Create(ctx, "", sharedentities.RuntimeSetting{
			Key: telemetrySettingsKey, Value: string(payload), CreatedAt: now, UpdatedAt: now,
		})
	} else {
		row.Value = string(payload)
		row.UpdatedAt = now
		_, err = s.repo.UpdateById(ctx, "", *row)
	}
	if err != nil {
		return TelemetrySettings{}, err
	}
	return settings, nil
}

// withTelemetryDefaults fills any zero field from the defaults, so a partial blob (or an older one
// missing a field) never silently becomes 0 batch size or a 0-day retention.
func withTelemetryDefaults(s, def TelemetrySettings) TelemetrySettings {
	if s.RawRetentionDays == 0 {
		s.RawRetentionDays = def.RawRetentionDays
	}
	if s.RollupRetentionDays == 0 {
		s.RollupRetentionDays = def.RollupRetentionDays
	}
	if s.BatchSize == 0 {
		s.BatchSize = def.BatchSize
	}
	if s.FlushMs == 0 {
		s.FlushMs = def.FlushMs
	}
	if s.QueueSize == 0 {
		s.QueueSize = def.QueueSize
	}
	if strings.TrimSpace(s.MqttAddr) == "" {
		s.MqttAddr = def.MqttAddr
	}
	return s
}

func validateTelemetrySettings(s TelemetrySettings) error {
	if s.RawRetentionDays < 1 || s.RollupRetentionDays < 1 {
		return fmt.Errorf("retention must be at least 1 day")
	}
	if s.BatchSize < 1 || s.QueueSize < 1 || s.FlushMs < 1 {
		return fmt.Errorf("batch size, queue size and flush interval must be positive")
	}
	if !strings.Contains(s.MqttAddr, ":") {
		return fmt.Errorf("the broker address must be host:port (e.g. 0.0.0.0:1883)")
	}
	return nil
}
