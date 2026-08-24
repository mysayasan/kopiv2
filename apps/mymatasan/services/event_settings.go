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

const onvifEventSettingsKey = "onvifEvents"

// OnvifEventSettings configures the camera event listener (W3-5b).
//
// A RUNTIME SETTING RATHER THAN A PER-CAMERA COLUMN, and that is a constraint rather than a
// preference: this app's auto-migrator creates tables from entities but does not ALTER
// existing ones, so a new column on `camera_onvif` would exist on fresh installs and be
// missing on every appliance already in the field. Every other monitor here — health,
// continuity, tamper, anomaly — is configured exactly this way and read live on each pass,
// so retuning takes effect without a restart. Which cameras are subscribed is decided by
// what the cameras themselves advertise, not by a per-camera tick box; see
// CameraEventMonitor.
type OnvifEventSettings struct {
	// Enabled turns the listener on.
	//
	// Default OFF, unlike the tamper and health monitors. This one opens a LONG-LIVED
	// CONNECTION PER CAMERA to hardware we did not write, and an estate that has never
	// wired an input into a camera gains nothing from it. A feature that costs a socket per
	// camera should be asked for.
	Enabled bool `json:"enabled"`
	// LeaseSeconds is how long a subscription is asked to live before renewal.
	//
	// Short leases are SAFER, not worse: the camera forgets us quickly if we die, so a
	// restarted appliance does not exhaust the device's subscription slots. The cost is a
	// renewal round trip, which is cheap.
	LeaseSeconds int `json:"leaseSeconds"`
	// PullTimeoutSeconds is how long the camera holds an empty poll open. This is the
	// latency floor for noticing a subscription has died, and the whole reason the ONVIF
	// client needed a per-call HTTP deadline.
	PullTimeoutSeconds int `json:"pullTimeoutSeconds"`
	// MaxCameras bounds how many cameras are subscribed at once.
	//
	// It exists so an estate of three hundred cameras does not silently open three hundred
	// long-lived sockets because somebody ticked a box. Reaching it is REPORTED rather than
	// silently truncated — a listener that quietly covers the first fifty cameras is worse
	// than one that is off, because the screen says it is running.
	MaxCameras int `json:"maxCameras"`
	// LostAfterSeconds is how long a camera's subscription may stay broken before it is
	// raised as a fault.
	//
	// A lapsed subscription does not report an error — it reports SILENCE, and a door
	// contact that stopped reporting looks exactly like a door nobody opened. This is what
	// turns that silence into something an operator is told about.
	LostAfterSeconds int `json:"lostAfterSeconds"`
	// IncludeMotion folds the camera's OWN motion and analytics events into the alert log.
	//
	// Default OFF, and the reason is the interesting one: this appliance already runs its
	// own detection over the same picture, with rules, zones, schedules and cooldowns that
	// the camera knows nothing about. Turning a camera's built-in motion on as well
	// produces a second, unfiltered stream of "something moved" that no rule governs and
	// nothing can tune — and the two disagree constantly. Inputs and relays have no such
	// overlap, which is why they are always on when the listener is.
	IncludeMotion bool `json:"includeMotion"`
}

// DefaultOnvifEventSettings is what an install starts with.
func DefaultOnvifEventSettings() OnvifEventSettings {
	return OnvifEventSettings{
		Enabled:            false,
		LeaseSeconds:       60,
		PullTimeoutSeconds: 20,
		MaxCameras:         32,
		LostAfterSeconds:   180,
		IncludeMotion:      false,
	}
}

func normalizeOnvifEventSettings(in OnvifEventSettings) OnvifEventSettings {
	def := DefaultOnvifEventSettings()
	// A lease under 20s spends more time renewing than listening; over an hour and a dead
	// appliance holds the camera's subscription slot for an hour.
	if in.LeaseSeconds < 20 || in.LeaseSeconds > 3600 {
		in.LeaseSeconds = def.LeaseSeconds
	}
	// The poll must finish well inside the lease, or the renewal never gets a turn.
	if in.PullTimeoutSeconds < 1 || in.PullTimeoutSeconds > in.LeaseSeconds/2 {
		in.PullTimeoutSeconds = def.PullTimeoutSeconds
		if in.PullTimeoutSeconds > in.LeaseSeconds/2 {
			in.PullTimeoutSeconds = in.LeaseSeconds / 2
		}
	}
	if in.MaxCameras <= 0 || in.MaxCameras > 256 {
		in.MaxCameras = def.MaxCameras
	}
	// Shorter than a lease and a single missed renewal reads as a fault.
	if in.LostAfterSeconds < in.LeaseSeconds || in.LostAfterSeconds > 86400 {
		in.LostAfterSeconds = def.LostAfterSeconds
		if in.LostAfterSeconds < in.LeaseSeconds {
			in.LostAfterSeconds = in.LeaseSeconds * 3
		}
	}
	return in
}

// IOnvifEventSettingsService reads and writes the listener's configuration.
type IOnvifEventSettingsService interface {
	Get(ctx context.Context) (OnvifEventSettings, error)
	Save(ctx context.Context, in OnvifEventSettings) (OnvifEventSettings, error)
}

type onvifEventSettingsService struct {
	repo     dbsql.IGenericRepo[entities.RuntimeSetting]
	defaults OnvifEventSettings
}

func NewOnvifEventSettingsService(repo dbsql.IGenericRepo[entities.RuntimeSetting]) IOnvifEventSettingsService {
	return &onvifEventSettingsService{repo: repo, defaults: DefaultOnvifEventSettings()}
}

func (s *onvifEventSettingsService) Get(ctx context.Context) (OnvifEventSettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", onvifEventSettingsKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return s.Save(ctx, s.defaults)
		}
		return OnvifEventSettings{}, err
	}
	settings := OnvifEventSettings{}
	if row != nil && strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &settings); err != nil {
			return OnvifEventSettings{}, fmt.Errorf("parse ONVIF event settings failed: %w", err)
		}
	}
	return normalizeOnvifEventSettings(settings), nil
}

func (s *onvifEventSettingsService) Save(ctx context.Context, in OnvifEventSettings) (OnvifEventSettings, error) {
	settings := normalizeOnvifEventSettings(in)
	blob, err := json.Marshal(settings)
	if err != nil {
		return OnvifEventSettings{}, err
	}
	now := time.Now().Unix()
	row, err := s.repo.GetByUnique(ctx, "", "key", onvifEventSettingsKey)
	if err != nil && !isNoResultFoundErr(err) {
		return OnvifEventSettings{}, err
	}
	if err == nil && row != nil {
		row.Value = string(blob)
		row.UpdatedAt = now
		if _, uerr := s.repo.UpdateById(ctx, "", *row); uerr != nil {
			return OnvifEventSettings{}, uerr
		}
		return settings, nil
	}
	if _, cerr := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key: onvifEventSettingsKey, Value: string(blob), CreatedAt: now, UpdatedAt: now,
	}); cerr != nil {
		return OnvifEventSettings{}, cerr
	}
	return settings, nil
}
