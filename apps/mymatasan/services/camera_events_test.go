package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/onvif"
)

type fakeEventCameras struct{ name string }

func (f *fakeEventCameras) Get(context.Context, uint64, uint64) ([]*CameraDetail, uint64, error) {
	return nil, 0, nil
}
func (f *fakeEventCameras) GetCameraCapabilities(context.Context, uint64) (*CameraCapabilities, error) {
	return &CameraCapabilities{Events: true}, nil
}
func (f *fakeEventCameras) EventEndpoint(context.Context, uint64) (string, onvif.Credentials, error) {
	return "", onvif.Credentials{}, nil
}
func (f *fakeEventCameras) DisplayName(context.Context, int64) string { return f.name }

func newEventRig() (*CameraEventMonitor, *capturedNotifications) {
	notif := &capturedNotifications{}
	m := NewCameraEventMonitor(&fakeEventCameras{name: "Loading bay"}, nil, nil, notif, nil)
	return m, notif
}

func inputEvent(token string, operation string, state string) onvif.Event {
	return onvif.Event{
		Topic:     "tns1:Device/Trigger/DigitalInput",
		Operation: operation,
		UTCTime:   time.Now().UTC(),
		Source:    map[string]string{"InputToken": token},
		Data:      map[string]string{"LogicalState": state},
	}
}

// THE TRAP THE WHOLE LISTENER IS ARRANGED AROUND. On subscribing, a camera sends the CURRENT
// state of every property it publishes — so a building with four closed door contacts
// announces four closed door contacts the instant we connect. Treated as alerts, every
// restart, every renewal failure and every network blip would raise a burst of alarms for
// doors nobody touched, at exactly the moments an operator is least able to tell a real one
// from noise.
func TestTheInitialStateOfAnInputIsNotAnAlarm(t *testing.T) {
	m, notif := newEventRig()
	cfg := DefaultOnvifEventSettings()
	state := &cameraEventState{seen: map[string]bool{}}
	ctx := context.Background()

	// What a camera says the moment we subscribe.
	m.handle(ctx, 3, inputEvent("DI_1", "Initialized", "true"), cfg, state)
	m.handle(ctx, 3, inputEvent("DI_2", "Initialized", "false"), cfg, state)
	if len(notif.sent) != 0 {
		t.Fatalf("subscribing raised %d notification(s) about doors nobody touched", len(notif.sent))
	}

	// ...and then somebody actually opens one.
	m.handle(ctx, 3, inputEvent("DI_1", "Changed", "true"), cfg, state)
	if len(notif.sent) != 1 {
		t.Fatalf("a real change was not reported: %d notification(s)", len(notif.sent))
	}
	// It carries the camera id, which is what makes the feed answerable by camera — the
	// question the AI alert log was going to answer before it turned out to refuse a row
	// with no rule behind it.
	if notif.sent[0].CameraId != 3 {
		t.Fatalf("the event must name its camera, got %d", notif.sent[0].CameraId)
	}
	if !strings.Contains(notif.sent[0].Body, "Loading bay") {
		t.Fatalf("the notification should name the camera: %q", notif.sent[0].Body)
	}
	if !strings.Contains(notif.sent[0].Body, "DI_1") {
		t.Fatalf("the notification should name WHICH input: %q", notif.sent[0].Body)
	}
}

// A door contact is a SENSOR READING, not a detection. Filing it under vision.alert would
// make "tell me when a door opens, but not about every person the AI sees" unexpressible
// for anyone routing notifications.
func TestAnInputIsADeviceAlertNotAVisionAlert(t *testing.T) {
	m, notif := newEventRig()
	m.handle(context.Background(), 3, inputEvent("DI_1", "Changed", "true"),
		DefaultOnvifEventSettings(), &cameraEventState{seen: map[string]bool{}})

	if notif.sent[0].Category != notification.CategoryDeviceAlert {
		t.Fatalf("category = %q, want %q", notif.sent[0].Category, notification.CategoryDeviceAlert)
	}
	// ...and the category has to be one a destination can actually subscribe to, or a
	// destination set up for door contacts alone would silently receive everything.
	if !knownCategories[notification.CategoryDeviceAlert] {
		t.Fatal("device.alert is not a category a destination can subscribe to")
	}
}

// A return to normal is information, never an alarm — otherwise a door closing wakes
// somebody as loudly as a door opening, and the feature is muted within a week.
func TestAnInputReturningToNormalIsNotAnAlarm(t *testing.T) {
	m, notif := newEventRig()
	cfg := DefaultOnvifEventSettings()
	state := &cameraEventState{seen: map[string]bool{}}
	ctx := context.Background()

	m.handle(ctx, 3, inputEvent("DI_1", "Changed", "true"), cfg, state)
	m.handle(ctx, 3, inputEvent("DI_1", "Changed", "false"), cfg, state)

	if len(notif.sent) != 2 {
		t.Fatalf("both transitions should be reported, got %d", len(notif.sent))
	}
	if notif.sent[0].Severity != notification.Warning {
		t.Fatalf("an input becoming active is a warning, got %v", notif.sent[0].Severity)
	}
	if notif.sent[1].Severity != notification.Info {
		t.Fatalf("an input returning to normal is information, got %v", notif.sent[1].Severity)
	}
}

// The camera's own motion is opt-in and off by default. This appliance already runs its own
// detection over the same picture with rules, zones, schedules and cooldowns the camera
// knows nothing about; a second unfiltered stream of "something moved" would bury the first.
func TestTheCamerasOwnMotionIsOffByDefault(t *testing.T) {
	m, notif := newEventRig()
	motion := onvif.Event{
		Topic: "tns1:VideoSource/MotionAlarm", Operation: "Changed",
		Source: map[string]string{"VideoSourceConfigurationToken": "VSC1"},
		Data:   map[string]string{"State": "true"},
	}
	cfg := DefaultOnvifEventSettings()
	state := &cameraEventState{seen: map[string]bool{}}

	m.handle(context.Background(), 3, motion, cfg, state)
	if len(notif.sent) != 0 {
		t.Fatalf("camera motion leaked into the feed by default")
	}

	cfg.IncludeMotion = true
	m.handle(context.Background(), 3, motion, cfg, state)
	if len(notif.sent) != 1 {
		t.Fatalf("camera motion should be reported when it is asked for, got %d", len(notif.sent))
	}

	// An input is NOT affected by that switch: inputs and relays have no overlap with our
	// own detection, so they are always on when the listener is.
	m.handle(context.Background(), 3, inputEvent("DI_1", "Changed", "true"),
		DefaultOnvifEventSettings(), state)
	if len(notif.sent) != 2 {
		t.Fatal("an input must report regardless of the camera-motion setting")
	}
}

// Renewal at two thirds of the lease, driven by the lease the DEVICE reported. A renewal
// that is merely "before expiry" races the camera's clock, its rounding and the round trip
// — and losing that race costs the whole subscription, silently.
func TestRenewalLeavesRoomToLoseTheRace(t *testing.T) {
	cfg := DefaultOnvifEventSettings()
	if got := renewAfter(60*time.Second, cfg); got != 40*time.Second {
		t.Fatalf("renewAfter(60s) = %v, want 40s", got)
	}
	// A device that reports no lease falls back to what we asked for, not to zero (which
	// would renew in a tight loop) and not to the raw lease (which would renew too late).
	if got := renewAfter(0, cfg); got != time.Duration(cfg.LeaseSeconds)*2/3*time.Second {
		t.Fatalf("renewAfter(0) = %v", got)
	}
	// A very short lease still leaves a floor, or the renewal loop becomes the workload.
	if got := renewAfter(3*time.Second, cfg); got < 5*time.Second {
		t.Fatalf("renewAfter(3s) = %v, want at least 5s", got)
	}
}

func TestEventSettingsRejectNonsense(t *testing.T) {
	def := DefaultOnvifEventSettings()
	// The poll must finish well inside the lease, or the renewal never gets a turn and the
	// subscription lapses on a schedule.
	got := normalizeOnvifEventSettings(OnvifEventSettings{LeaseSeconds: 30, PullTimeoutSeconds: 300})
	if got.PullTimeoutSeconds > got.LeaseSeconds/2 {
		t.Fatalf("a poll longer than half the lease starves the renewal: %+v", got)
	}
	got = normalizeOnvifEventSettings(OnvifEventSettings{LeaseSeconds: 1})
	if got.LeaseSeconds != def.LeaseSeconds {
		t.Fatalf("a one-second lease spends all its time renewing: %d", got.LeaseSeconds)
	}
	got = normalizeOnvifEventSettings(OnvifEventSettings{MaxCameras: -1})
	if got.MaxCameras <= 0 {
		t.Fatal("a non-positive camera limit would listen to nothing while claiming to run")
	}
	// Shorter than a lease and a single missed renewal reads as a fault.
	got = normalizeOnvifEventSettings(OnvifEventSettings{LeaseSeconds: 120, LostAfterSeconds: 5})
	if got.LostAfterSeconds < got.LeaseSeconds {
		t.Fatalf("a lost-after shorter than the lease alerts on every renewal: %+v", got)
	}
	// Default OFF: this one opens a long-lived connection per camera.
	if def.Enabled {
		t.Fatal("the event listener must be opt-in")
	}
}
