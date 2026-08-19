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

const continuitySettingsKey = "continuity"

// ContinuitySettings configures the recording-continuity monitor. Persisted in the same
// runtime_setting table as the other monitor settings and read live on every sweep, so a
// change applies without a restart.
type ContinuitySettings struct {
	// Enabled turns the monitor on/off. Default ON: an NVR that is not checking whether
	// it recorded anything is the failure this monitor exists to catch, and an operator
	// who has to discover the feature and switch it on will not.
	Enabled bool `json:"enabled"`
	// IntervalMs is the gap between sweeps. Sweeps score whole CLOSED hours, so running
	// more often than hourly only shortens the delay between an hour closing and it being
	// scored; it does not score anything twice.
	IntervalMs int `json:"intervalMs"`
	// MinCoveragePercent is how much of an hour must be on disk for it to count as
	// covered. Not 100: segment boundaries, a recorder restart and the remux queue all
	// cost a few seconds an hour legitimately, and a monitor that cries wolf on every
	// normal hour gets muted — after which it protects nothing.
	MinCoveragePercent float64 `json:"minCoveragePercent"`
	// FailureThreshold is the consecutive bad hours before raising an alert, and
	// RecoveryThreshold the consecutive good hours before clearing it. Both exist for the
	// same reason as the reachability monitor's: one bad hour is a blip, three in a row
	// is a camera that has stopped recording.
	FailureThreshold  int `json:"failureThreshold"`
	RecoveryThreshold int `json:"recoveryThreshold"`
}

// DefaultContinuitySettings is what an install starts with.
//
// 95% allows three minutes of legitimate loss per hour, which comfortably covers segment
// rollover and a recorder restart while still catching a camera that recorded 40 minutes
// of a 60-minute hour. Two consecutive bad hours before alerting keeps a single ffmpeg
// restart quiet without letting a genuinely dead camera go unnoticed past mid-morning.
func DefaultContinuitySettings() ContinuitySettings {
	return ContinuitySettings{
		Enabled:            true,
		IntervalMs:         10 * 60 * 1000,
		MinCoveragePercent: 95,
		FailureThreshold:   2,
		RecoveryThreshold:  1,
	}
}

func normalizeContinuitySettings(in ContinuitySettings) ContinuitySettings {
	def := DefaultContinuitySettings()
	if in.IntervalMs <= 0 {
		in.IntervalMs = def.IntervalMs
	}
	// A percentage outside (0,100] cannot mean anything useful: 0 would score every hour
	// as covered and make the monitor a no-op, above 100 would alarm on every hour.
	if in.MinCoveragePercent <= 0 || in.MinCoveragePercent > 100 {
		in.MinCoveragePercent = def.MinCoveragePercent
	}
	if in.FailureThreshold <= 0 {
		in.FailureThreshold = def.FailureThreshold
	}
	if in.RecoveryThreshold <= 0 {
		in.RecoveryThreshold = def.RecoveryThreshold
	}
	return in
}

// IContinuitySettingsService reads and writes the monitor's configuration.
type IContinuitySettingsService interface {
	Get(ctx context.Context) (ContinuitySettings, error)
	Save(ctx context.Context, in ContinuitySettings) (ContinuitySettings, error)
}

type continuitySettingsService struct {
	repo     dbsql.IGenericRepo[entities.RuntimeSetting]
	defaults ContinuitySettings
}

func NewContinuitySettingsService(repo dbsql.IGenericRepo[entities.RuntimeSetting]) IContinuitySettingsService {
	return &continuitySettingsService{repo: repo, defaults: DefaultContinuitySettings()}
}

func (s *continuitySettingsService) Get(ctx context.Context) (ContinuitySettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", continuitySettingsKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return s.Save(ctx, s.defaults)
		}
		return ContinuitySettings{}, err
	}
	settings := ContinuitySettings{}
	if row != nil && strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &settings); err != nil {
			return ContinuitySettings{}, fmt.Errorf("parse continuity settings failed: %w", err)
		}
	}
	return normalizeContinuitySettings(settings), nil
}

func (s *continuitySettingsService) Save(ctx context.Context, in ContinuitySettings) (ContinuitySettings, error) {
	settings := normalizeContinuitySettings(in)
	blob, err := json.Marshal(settings)
	if err != nil {
		return ContinuitySettings{}, err
	}
	now := time.Now().Unix()
	row, err := s.repo.GetByUnique(ctx, "", "key", continuitySettingsKey)
	if err != nil && !isNoResultFoundErr(err) {
		return ContinuitySettings{}, err
	}
	if err == nil && row != nil {
		row.Value = string(blob)
		row.UpdatedAt = now
		if _, uerr := s.repo.UpdateById(ctx, "", *row); uerr != nil {
			return ContinuitySettings{}, uerr
		}
		return settings, nil
	}
	if _, cerr := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key: continuitySettingsKey, Value: string(blob), CreatedAt: now, UpdatedAt: now,
	}); cerr != nil {
		return ContinuitySettings{}, cerr
	}
	return settings, nil
}
