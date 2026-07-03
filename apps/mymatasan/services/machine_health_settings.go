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

const machineHealthSettingsKey = "machineHealth"

// MachineThreshold is a two-level percentage threshold for a host metric. A
// sample at or above WarnPercent raises a Warning; at or above CriticalPercent a
// Critical. Zero means "use the built-in default".
type MachineThreshold struct {
	WarnPercent     int `json:"warnPercent"`
	CriticalPercent int `json:"criticalPercent"`
}

// MachineDiskSettings tunes disk-space monitoring. Paths are extra user-specified
// locations to watch; the monitor also auto-includes the volumes the app writes
// to (recordings, logs, working dir).
type MachineDiskSettings struct {
	WarnPercent     int      `json:"warnPercent"`
	CriticalPercent int      `json:"criticalPercent"`
	Paths           []string `json:"paths"`
}

// MachineMitigationSettings controls automatic protective actions taken when a
// monitored disk gets dangerously full, so a 100%-full volume cannot break all
// writes (recordings, snapshots, the database).
type MachineMitigationSettings struct {
	// Enabled gates all automatic mitigation.
	Enabled bool `json:"enabled"`
	// PurgeAtPercent triggers an immediate retention purge of expired recordings
	// when a monitored disk reaches this fullness (low risk; only deletes already
	// expired footage).
	PurgeAtPercent int `json:"purgeAtPercent"`
	// PauseRecordingAtPercent stops NVR recording when a monitored disk reaches
	// this fullness, to stop the volume filling completely. Recording resumes once
	// the disk drops below ResumePercent. NOTE: footage is not captured while paused.
	PauseRecordingAtPercent int `json:"pauseRecordingAtPercent"`
	// ResumePercent is the fullness a paused disk must drop below before recording
	// resumes (hysteresis to avoid flapping).
	ResumePercent int `json:"resumePercent"`
	// OverwriteOldest keeps recording continuous: when a disk reaches
	// PauseRecordingAtPercent, the oldest recorded segments are deleted (even if
	// still inside their retention window) until usage would drop below
	// ResumePercent, instead of pausing. Recording only pauses as a fallback when
	// nothing old enough remains to delete.
	OverwriteOldest bool `json:"overwriteOldest"`
	// OverwriteMinKeepDays is the safety floor for OverwriteOldest: footage newer
	// than this many days is never auto-deleted.
	OverwriteMinKeepDays int `json:"overwriteMinKeepDays"`
}

// MachineHealthSettings is the runtime-editable host (machine) health monitor
// configuration persisted in the database and edited from the Settings UI. The
// monitor reads it live on every sample, so changes apply without a restart.
type MachineHealthSettings struct {
	// Enabled turns the monitor on/off without restarting the app.
	Enabled bool `json:"enabled"`
	// IntervalMs is the gap between host metric samples.
	IntervalMs int `json:"intervalMs"`
	// SustainedSamples is the consecutive breaching samples before alerting (debounce).
	SustainedSamples int `json:"sustainedSamples"`
	// RecoverySamples is the consecutive normal samples before a recovery is announced.
	RecoverySamples int `json:"recoverySamples"`
	// CPU/Memory/Disk are the per-metric thresholds.
	CPU        MachineThreshold          `json:"cpu"`
	Memory     MachineThreshold          `json:"memory"`
	Disk       MachineDiskSettings       `json:"disk"`
	Mitigation MachineMitigationSettings `json:"mitigation"`
}

type machineHealthSettingsService struct {
	repo     dbsql.IGenericRepo[entities.RuntimeSetting]
	defaults MachineHealthSettings
}

// NewMachineHealthSettingsService creates the machine health settings service.
// Settings persist under a single "machineHealth" key in the same runtime_setting
// table used by decoder/vision/notification/health.
func NewMachineHealthSettingsService(
	repo dbsql.IGenericRepo[entities.RuntimeSetting],
	defaults MachineHealthSettings,
) IMachineHealthSettingsService {
	return &machineHealthSettingsService{repo: repo, defaults: normalizeMachineHealthSettings(defaults)}
}

func (s *machineHealthSettingsService) Get(ctx context.Context) (MachineHealthSettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", machineHealthSettingsKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return s.Save(ctx, s.defaults)
		}
		return MachineHealthSettings{}, err
	}
	settings := MachineHealthSettings{}
	if row != nil && strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &settings); err != nil {
			return MachineHealthSettings{}, fmt.Errorf("parse machine health settings failed: %w", err)
		}
	}
	return normalizeMachineHealthSettings(settings), nil
}

func (s *machineHealthSettingsService) Save(ctx context.Context, settings MachineHealthSettings) (MachineHealthSettings, error) {
	settings = normalizeMachineHealthSettings(settings)
	payload, err := json.Marshal(settings)
	if err != nil {
		return MachineHealthSettings{}, err
	}
	now := time.Now().UTC().Unix()

	existing, err := s.repo.GetByUnique(ctx, "", "key", machineHealthSettingsKey)
	if err == nil && existing != nil {
		existing.Value = string(payload)
		existing.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *existing); err != nil {
			return MachineHealthSettings{}, err
		}
	} else if err != nil && !isNoResultFoundErr(err) {
		return MachineHealthSettings{}, err
	} else if _, err := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key:       machineHealthSettingsKey,
		Value:     string(payload),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return MachineHealthSettings{}, err
	}
	return settings, nil
}

// DefaultMachineHealthSettings returns the safe built-in defaults used to seed the
// row on first read and as the app-config fallback.
func DefaultMachineHealthSettings() MachineHealthSettings {
	return MachineHealthSettings{
		Enabled:          true,
		IntervalMs:       30000,
		SustainedSamples: 3,
		RecoverySamples:  2,
		CPU:              MachineThreshold{WarnPercent: 85, CriticalPercent: 95},
		Memory:           MachineThreshold{WarnPercent: 85, CriticalPercent: 95},
		Disk:             MachineDiskSettings{WarnPercent: 80, CriticalPercent: 90, Paths: []string{}},
		Mitigation: MachineMitigationSettings{
			Enabled:                 true,
			PurgeAtPercent:          88,
			PauseRecordingAtPercent: 95,
			ResumePercent:           80,
			OverwriteOldest:         false,
			OverwriteMinKeepDays:    1,
		},
	}
}

// normalizeMachineHealthSettings clamps every field to a safe range and fills
// defaults for zero/invalid values, keeping warn < critical and the mitigation
// thresholds ordered (resume < pause).
func normalizeMachineHealthSettings(s MachineHealthSettings) MachineHealthSettings {
	d := DefaultMachineHealthSettings()
	s.IntervalMs = normalizeInt(s.IntervalMs, d.IntervalMs, 5000, 3600000)
	s.SustainedSamples = normalizeInt(s.SustainedSamples, d.SustainedSamples, 1, 60)
	s.RecoverySamples = normalizeInt(s.RecoverySamples, d.RecoverySamples, 1, 60)
	s.CPU = normalizeMachineThreshold(s.CPU, d.CPU)
	s.Memory = normalizeMachineThreshold(s.Memory, d.Memory)

	s.Disk.WarnPercent = normalizeInt(s.Disk.WarnPercent, d.Disk.WarnPercent, 1, 100)
	s.Disk.CriticalPercent = normalizeInt(s.Disk.CriticalPercent, d.Disk.CriticalPercent, 1, 100)
	if s.Disk.CriticalPercent <= s.Disk.WarnPercent {
		s.Disk.CriticalPercent = clampInt(s.Disk.WarnPercent+5, 1, 100)
	}
	s.Disk.Paths = normalizePathList(s.Disk.Paths)

	s.Mitigation.PurgeAtPercent = normalizeInt(s.Mitigation.PurgeAtPercent, d.Mitigation.PurgeAtPercent, 1, 100)
	s.Mitigation.PauseRecordingAtPercent = normalizeInt(s.Mitigation.PauseRecordingAtPercent, d.Mitigation.PauseRecordingAtPercent, 1, 100)
	s.Mitigation.ResumePercent = normalizeInt(s.Mitigation.ResumePercent, d.Mitigation.ResumePercent, 1, 99)
	if s.Mitigation.ResumePercent >= s.Mitigation.PauseRecordingAtPercent {
		s.Mitigation.ResumePercent = clampInt(s.Mitigation.PauseRecordingAtPercent-10, 1, 99)
	}
	s.Mitigation.OverwriteMinKeepDays = normalizeInt(s.Mitigation.OverwriteMinKeepDays, d.Mitigation.OverwriteMinKeepDays, 1, 365)
	return s
}

func normalizeMachineThreshold(t MachineThreshold, d MachineThreshold) MachineThreshold {
	t.WarnPercent = normalizeInt(t.WarnPercent, d.WarnPercent, 1, 100)
	t.CriticalPercent = normalizeInt(t.CriticalPercent, d.CriticalPercent, 1, 100)
	if t.CriticalPercent <= t.WarnPercent {
		t.CriticalPercent = clampInt(t.WarnPercent+5, 1, 100)
	}
	return t
}

func normalizePathList(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func clampInt(v, minValue, maxValue int) int {
	if v < minValue {
		return minValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
}
