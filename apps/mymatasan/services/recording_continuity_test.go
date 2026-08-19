package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
)

// --- fakes -------------------------------------------------------------------

// fakeCoverageRecording serves a scripted coverage percentage per sweep, so a test can
// drive a sequence of hours without building segment rows for each.
type fakeCoverageRecording struct {
	IRecordingService
	configs []*entities.RecordingConfig
	// pct is consumed one entry per Coverage call; the last value repeats.
	pct  []float64
	call int
}

func (f *fakeCoverageRecording) ListConfigs(context.Context) ([]*entities.RecordingConfig, error) {
	return f.configs, nil
}

func (f *fakeCoverageRecording) Coverage(_ context.Context, cameraId, from, to int64, _ string) (CoverageReport, error) {
	v := 100.0
	if len(f.pct) > 0 {
		if f.call < len(f.pct) {
			v = f.pct[f.call]
		} else {
			v = f.pct[len(f.pct)-1]
		}
	}
	f.call++
	return CoverageReport{CameraId: cameraId, From: from, To: to, OverallPercent: v}, nil
}

type fakeContinuityCamera struct {
	ICameraService
	name   string
	health string
}

func (f *fakeContinuityCamera) DisplayName(context.Context, int64) string { return f.name }
func (f *fakeContinuityCamera) GetById(_ context.Context, id uint64) (*CameraDetail, error) {
	d := &CameraDetail{}
	d.Camera.Id = int64(id)
	d.HealthStatus = f.health
	return d, nil
}

type capturedNotifier struct{ sent []notification.Notification }

func (c *capturedNotifier) Publish(_ context.Context, n notification.Notification) notification.Notification {
	c.sent = append(c.sent, n)
	return n
}

func (c *capturedNotifier) DeliverTo(context.Context, string, notification.Notification) {}

type fakePause struct{ paused bool }

func (f fakePause) IsPaused() bool { return f.paused }

type staticContinuitySettings struct{ cfg ContinuitySettings }

func (s staticContinuitySettings) Get(context.Context) (ContinuitySettings, error) {
	return s.cfg, nil
}
func (s staticContinuitySettings) Save(_ context.Context, in ContinuitySettings) (ContinuitySettings, error) {
	return in, nil
}

// newContinuityRig wires a monitor over the fakes, with an injected clock so each sweep
// scores a DIFFERENT closed hour.
func newContinuityRig(pcts []float64, cfg ContinuitySettings) (*RecordingContinuityMonitor, *capturedNotifier, *fakeContinuityCamera, *fakePause) {
	rec := &fakeCoverageRecording{
		configs: []*entities.RecordingConfig{{CameraId: 3, Enabled: true}},
		pct:     pcts,
	}
	cam := &fakeContinuityCamera{name: "Lobby", health: cameraHealthOnline}
	notif := &capturedNotifier{}
	pause := &fakePause{}
	m := NewRecordingContinuityMonitor(rec, cam, staticContinuitySettings{cfg}, notif, pause, nil)

	// Each call advances an hour, so successive sweeps score successive closed hours —
	// which is what the once-per-hour guard requires to let a streak build.
	base := time.Unix(testHourStart, 0).UTC().Add(time.Hour)
	n := 0
	m.now = func() time.Time {
		t := base.Add(time.Duration(n) * time.Hour)
		n++
		return t
	}
	return m, notif, cam, pause
}

func testContinuityCfg() ContinuitySettings {
	return ContinuitySettings{
		Enabled: true, IntervalMs: 600000, MinCoveragePercent: 95,
		FailureThreshold: 2, RecoveryThreshold: 1,
	}
}

func gapNotifications(sent []notification.Notification) []notification.Notification {
	var out []notification.Notification
	for _, n := range sent {
		if n.Title == "Recording gap" {
			out = append(out, n)
		}
	}
	return out
}

// --- tests -------------------------------------------------------------------

// The finding, end to end: a camera that stops writing footage raises an alert. Before
// this, a wedged ffmpeg, a full disk or a quarantine loop all reported green and the
// operator found out when somebody asked for the footage.
func TestContinuityRaisesGapAfterConsecutiveBadHours(t *testing.T) {
	m, notif, _, _ := newContinuityRig([]float64{0, 0}, testContinuityCfg())
	ctx := context.Background()

	m.Sweep(ctx, testContinuityCfg())
	if got := gapNotifications(notif.sent); len(got) != 0 {
		t.Fatalf("one bad hour must not alert (that is a blip), got %d", len(got))
	}

	m.Sweep(ctx, testContinuityCfg())
	gaps := gapNotifications(notif.sent)
	if len(gaps) != 1 {
		t.Fatalf("expected one gap alert after the threshold, got %d", len(gaps))
	}
	if gaps[0].Severity != notification.Critical {
		t.Errorf("a recording gap is critical, got %v", gaps[0].Severity)
	}
	if gaps[0].CameraId != 3 {
		t.Errorf("cameraId = %d", gaps[0].CameraId)
	}
	if gaps[0].Data["reason"] != "unexplained" {
		t.Errorf("reason = %v, want unexplained for a reachable camera", gaps[0].Data["reason"])
	}
}

// Edge-triggered: once raised, the alert must not repeat every sweep. A monitor that
// re-notifies hourly is one an operator mutes, after which it protects nothing.
func TestContinuityAlertsOnceNotEverySweep(t *testing.T) {
	m, notif, _, _ := newContinuityRig([]float64{0}, testContinuityCfg())
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		m.Sweep(ctx, testContinuityCfg())
	}
	if got := gapNotifications(notif.sent); len(got) != 1 {
		t.Fatalf("expected exactly one alert across five bad hours, got %d", len(got))
	}
}

func TestContinuityClearsWhenRecordingResumes(t *testing.T) {
	m, notif, _, _ := newContinuityRig([]float64{0, 0, 100}, testContinuityCfg())
	ctx := context.Background()
	m.Sweep(ctx, testContinuityCfg())
	m.Sweep(ctx, testContinuityCfg())
	m.Sweep(ctx, testContinuityCfg())

	var recovered int
	for _, n := range notif.sent {
		if n.Title == "Recording resumed" {
			recovered++
		}
	}
	if recovered != 1 {
		t.Fatalf("expected one recovery notification, got %d (all: %d)", recovered, len(notif.sent))
	}
}

// A healthy camera must never alert, or the monitor is noise. 95% leaves room for segment
// rollover and a recorder restart, which cost a few seconds an hour legitimately.
func TestContinuityStaysQuietOnANormalHour(t *testing.T) {
	m, notif, _, _ := newContinuityRig([]float64{99.7, 99.9, 98.5}, testContinuityCfg())
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		m.Sweep(ctx, testContinuityCfg())
	}
	if len(notif.sent) != 0 {
		t.Fatalf("a normally-covered hour must not notify, got %+v", notif.sent)
	}
}

// One incident, one story. If the reachability monitor already has this camera offline,
// "no footage" is that outage's consequence — not a second independent fault to chase.
func TestContinuityAttributesTheGapToAnOfflineCamera(t *testing.T) {
	m, notif, cam, _ := newContinuityRig([]float64{0, 0}, testContinuityCfg())
	cam.health = cameraHealthOffline
	ctx := context.Background()
	m.Sweep(ctx, testContinuityCfg())
	m.Sweep(ctx, testContinuityCfg())

	gaps := gapNotifications(notif.sent)
	if len(gaps) != 1 {
		t.Fatalf("expected one gap, got %d", len(gaps))
	}
	if gaps[0].Data["reason"] != "camera-offline" {
		t.Fatalf("reason = %v, want camera-offline", gaps[0].Data["reason"])
	}
	if !strings.Contains(gaps[0].Body, "camera was offline") {
		t.Fatalf("body should say why: %q", gaps[0].Body)
	}
}

// The disk guard pauses every recorder when the volume is nearly full, and raises its own
// alert. Scoring through a pause would blame every camera in the fleet for one disk
// problem — a page full of "recording gap" that all say the same thing.
func TestContinuityDoesNotAlertWhileTheDiskGuardHasPausedRecording(t *testing.T) {
	m, notif, _, pause := newContinuityRig([]float64{0, 0, 0}, testContinuityCfg())
	pause.paused = true
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		m.Sweep(ctx, testContinuityCfg())
	}
	if got := gapNotifications(notif.sent); len(got) != 0 {
		t.Fatalf("a paused recorder is a known cause and must not raise per-camera gaps, got %d", len(got))
	}
}

// A camera whose recording is switched off is not failing — it is configured that way.
// This is also what keeps detect-only AI frame sources out: EnsureDetectionStream never
// sets Enabled on the stored row.
func TestContinuitySkipsCamerasWithRecordingDisabled(t *testing.T) {
	rec := &fakeCoverageRecording{
		configs: []*entities.RecordingConfig{{CameraId: 3, Enabled: false}},
		pct:     []float64{0},
	}
	notif := &capturedNotifier{}
	m := NewRecordingContinuityMonitor(rec, &fakeContinuityCamera{}, staticContinuitySettings{testContinuityCfg()}, notif, fakePause{}, nil)
	m.now = func() time.Time { return time.Unix(testHourEnd, 0).UTC() }

	m.Sweep(context.Background(), testContinuityCfg())
	m.Sweep(context.Background(), testContinuityCfg())

	if rec.call != 0 {
		t.Fatalf("a disabled camera must not even be scored, %d Coverage calls", rec.call)
	}
	if len(notif.sent) != 0 {
		t.Fatalf("a disabled camera must not notify, got %+v", notif.sent)
	}
}

// The sweep interval is shorter than an hour, so the same closed hour is visited several
// times. Scoring it repeatedly would drive the bad streak past any threshold within the
// first hour, turning a debounce meant to span hours into no debounce at all.
func TestContinuityScoresEachHourOnlyOnce(t *testing.T) {
	rec := &fakeCoverageRecording{
		configs: []*entities.RecordingConfig{{CameraId: 3, Enabled: true}},
		pct:     []float64{0},
	}
	notif := &capturedNotifier{}
	m := NewRecordingContinuityMonitor(rec, &fakeContinuityCamera{}, staticContinuitySettings{testContinuityCfg()}, notif, fakePause{}, nil)
	// A frozen clock: every sweep scores the SAME closed hour, as a 10-minute interval does.
	m.now = func() time.Time { return time.Unix(testHourEnd, 0).UTC() }

	for i := 0; i < 6; i++ {
		m.Sweep(context.Background(), testContinuityCfg())
	}
	if got := gapNotifications(notif.sent); len(got) != 0 {
		t.Fatalf("re-scoring one hour must not satisfy a two-hour threshold, got %d alerts", len(got))
	}
}

// Only CLOSED hours are scored. Scoring the hour in progress would find it legitimately
// under-covered and raise a gap on every healthy camera, every sweep.
func TestContinuityScoresThePreviousHourNotTheCurrentOne(t *testing.T) {
	var gotFrom, gotTo int64
	rec := &fakeCoverageRecording{configs: []*entities.RecordingConfig{{CameraId: 3, Enabled: true}}}
	probe := &coverageWindowProbe{inner: rec, from: &gotFrom, to: &gotTo}
	m := NewRecordingContinuityMonitor(probe, &fakeContinuityCamera{}, staticContinuitySettings{testContinuityCfg()}, &capturedNotifier{}, fakePause{}, nil)
	// 14:37 — mid-hour. The hour that must be scored is 13:00–14:00.
	m.now = func() time.Time { return time.Unix(testHourStart+2220, 0).UTC() }

	m.Sweep(context.Background(), testContinuityCfg())

	if gotFrom != testHourStart-3600 || gotTo != testHourStart {
		t.Fatalf("scored [%d,%d), want the previous closed hour [%d,%d)",
			gotFrom, gotTo, testHourStart-3600, testHourStart)
	}
}

// coverageWindowProbe records the window the monitor asked for. It embeds the interface
// so only the two methods under test need implementing; anything else nil-panics loudly
// rather than quietly returning a zero value.
type coverageWindowProbe struct {
	IRecordingService
	inner    IRecordingService
	from, to *int64
}

func (p *coverageWindowProbe) ListConfigs(ctx context.Context) ([]*entities.RecordingConfig, error) {
	return p.inner.ListConfigs(ctx)
}
func (p *coverageWindowProbe) Coverage(ctx context.Context, cameraId, from, to int64, bucket string) (CoverageReport, error) {
	*p.from, *p.to = from, to
	return p.inner.Coverage(ctx, cameraId, from, to, bucket)
}

