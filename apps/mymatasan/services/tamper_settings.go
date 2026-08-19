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

const tamperSettingsKey = "tamper"

// TamperSettings configures the camera tamper monitor. Persisted in the runtime_setting
// table and read live on every sweep, so retuning takes effect without a restart —
// which matters more here than for the other monitors, because tuning this one is done
// by watching it run on a real site.
type TamperSettings struct {
	// Enabled turns the monitor on or off.
	//
	// Default ON. A covered lens is what an attacker arranges BEFORE doing anything
	// worth recording, and it is invisible to every other check: the camera answers, the
	// recorder writes files, and the footage is of a bag.
	Enabled bool `json:"enabled"`
	// IntervalMs is the gap between samples.
	IntervalMs int `json:"intervalMs"`
	// FailureThreshold is the consecutive abnormal samples before raising an alert. One
	// odd frame is a lorry passing the camera; several in a row is the view being gone.
	FailureThreshold int `json:"failureThreshold"`

	// FrozenSeconds is how long the picture must be completely unchanging before the
	// stream counts as frozen. Expressed in SECONDS rather than samples because the thing
	// that makes it safe is duration: a handful of identical frames happens, a minute of
	// them does not.
	FrozenSeconds int `json:"frozenSeconds"`
	// FrozenMaxDifference is the mean per-pixel difference at or below which two frames
	// count as identical. Essentially zero: a live camera watching an empty room still
	// has sensor noise, so anything above the floor means the source is alive.
	FrozenMaxDifference float64 `json:"frozenMaxDifference"`

	// CoveredRatio is the fraction of the camera's OWN recent median edge energy below
	// which the view counts as blocked. Relative, not absolute: no single threshold can
	// be right for both a busy loading bay and a plain corridor.
	CoveredRatio float64 `json:"coveredRatio"`

	// MovedDistance is the luma-histogram distance (0..1) that counts as the camera
	// pointing somewhere else. High enough that a person crossing frame does not reach it.
	MovedDistance float64 `json:"movedDistance"`
}

// DefaultTamperSettings is what an install starts with.
//
// The numbers lean conservative in every direction, because the failure mode that kills
// this feature is not a missed tamper — it is an operator muting it after a week of false
// alarms, at which point it catches nothing at all.
func DefaultTamperSettings() TamperSettings {
	return TamperSettings{
		Enabled:             true,
		IntervalMs:          30000,
		FailureThreshold:    3,
		FrozenSeconds:       120,
		FrozenMaxDifference: 0.0005,
		CoveredRatio:        0.15,
		MovedDistance:       0.55,
	}
}

func normalizeTamperSettings(in TamperSettings) TamperSettings {
	def := DefaultTamperSettings()
	if in.IntervalMs <= 0 {
		in.IntervalMs = def.IntervalMs
	}
	if in.FailureThreshold <= 0 {
		in.FailureThreshold = def.FailureThreshold
	}
	if in.FrozenSeconds <= 0 {
		in.FrozenSeconds = def.FrozenSeconds
	}
	if in.FrozenMaxDifference < 0 {
		in.FrozenMaxDifference = def.FrozenMaxDifference
	}
	// A ratio outside (0,1) cannot mean anything: 0 never fires, 1 or more fires on every
	// sample including a perfectly normal one.
	if in.CoveredRatio <= 0 || in.CoveredRatio >= 1 {
		in.CoveredRatio = def.CoveredRatio
	}
	if in.MovedDistance <= 0 || in.MovedDistance > 1 {
		in.MovedDistance = def.MovedDistance
	}
	return in
}

// ITamperSettingsService reads and writes the monitor's configuration.
type ITamperSettingsService interface {
	Get(ctx context.Context) (TamperSettings, error)
	Save(ctx context.Context, in TamperSettings) (TamperSettings, error)
}

type tamperSettingsService struct {
	repo     dbsql.IGenericRepo[entities.RuntimeSetting]
	defaults TamperSettings
}

func NewTamperSettingsService(repo dbsql.IGenericRepo[entities.RuntimeSetting]) ITamperSettingsService {
	return &tamperSettingsService{repo: repo, defaults: DefaultTamperSettings()}
}

func (s *tamperSettingsService) Get(ctx context.Context) (TamperSettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", tamperSettingsKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return s.Save(ctx, s.defaults)
		}
		return TamperSettings{}, err
	}
	settings := TamperSettings{}
	if row != nil && strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &settings); err != nil {
			return TamperSettings{}, fmt.Errorf("parse tamper settings failed: %w", err)
		}
	}
	return normalizeTamperSettings(settings), nil
}

func (s *tamperSettingsService) Save(ctx context.Context, in TamperSettings) (TamperSettings, error) {
	settings := normalizeTamperSettings(in)
	blob, err := json.Marshal(settings)
	if err != nil {
		return TamperSettings{}, err
	}
	now := time.Now().Unix()
	row, err := s.repo.GetByUnique(ctx, "", "key", tamperSettingsKey)
	if err != nil && !isNoResultFoundErr(err) {
		return TamperSettings{}, err
	}
	if err == nil && row != nil {
		row.Value = string(blob)
		row.UpdatedAt = now
		if _, uerr := s.repo.UpdateById(ctx, "", *row); uerr != nil {
			return TamperSettings{}, uerr
		}
		return settings, nil
	}
	if _, cerr := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key: tamperSettingsKey, Value: string(blob), CreatedAt: now, UpdatedAt: now,
	}); cerr != nil {
		return TamperSettings{}, cerr
	}
	return settings, nil
}
