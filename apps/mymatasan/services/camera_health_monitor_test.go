package services

import (
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
)

func newTestHealthMonitor(failureN, recoveryN int) *CameraHealthMonitor {
	return &CameraHealthMonitor{
		failureN:  failureN,
		recoveryN: recoveryN,
		state:     map[int64]*cameraHealthState{},
	}
}

func TestDecideDebouncesOfflineUntilFailureThreshold(t *testing.T) {
	m := newTestHealthMonitor(3, 2)
	st := &cameraHealthState{}

	// Two failures: below the threshold, no transition yet.
	for i := 0; i < 2; i++ {
		if tr, _, _ := m.decide(st, false, int64(i)); tr != "" {
			t.Fatalf("failure %d: expected no transition, got %q", i+1, tr)
		}
	}
	// Third consecutive failure crosses the threshold -> offline.
	tr, _, status := m.decide(st, false, 100)
	if tr != "offline" {
		t.Fatalf("expected offline transition, got %q", tr)
	}
	if status != cameraHealthOffline {
		t.Fatalf("expected status %q, got %q", cameraHealthOffline, status)
	}
	if st.offlineSince != 100 {
		t.Fatalf("expected offlineSince=100, got %d", st.offlineSince)
	}

	// A further failure while already offline does not re-fire.
	if tr, _, _ := m.decide(st, false, 101); tr != "" {
		t.Fatalf("expected no repeat offline transition, got %q", tr)
	}
}

func TestDecideRecoveryReportsDowntime(t *testing.T) {
	m := newTestHealthMonitor(1, 2)
	st := &cameraHealthState{}

	// One failure declares offline at t=10.
	if tr, _, _ := m.decide(st, false, 10); tr != "offline" {
		t.Fatalf("expected offline, got %q", tr)
	}
	// One success: below recovery threshold (2), no transition.
	if tr, _, _ := m.decide(st, true, 40); tr != "" {
		t.Fatalf("expected no transition on first success, got %q", tr)
	}
	// Second success at t=70 crosses recovery threshold -> recovery, downtime 60s.
	tr, offlineFor, status := m.decide(st, true, 70)
	if tr != "recovery" {
		t.Fatalf("expected recovery transition, got %q", tr)
	}
	if offlineFor != 60 {
		t.Fatalf("expected offlineFor=60, got %d", offlineFor)
	}
	if status != cameraHealthOnline {
		t.Fatalf("expected status %q, got %q", cameraHealthOnline, status)
	}
}

func TestDecideInitialOnlineDoesNotNotify(t *testing.T) {
	m := newTestHealthMonitor(3, 2)
	st := &cameraHealthState{}

	// Camera healthy from the start: reaching online from "" is normal, no alert.
	m.decide(st, true, 1)
	tr, _, status := m.decide(st, true, 2)
	if tr != "" {
		t.Fatalf("expected no transition for initial online, got %q", tr)
	}
	if status != cameraHealthOnline {
		t.Fatalf("expected status %q, got %q", cameraHealthOnline, status)
	}
}

func TestDecideFlappingSuccessResetsFailStreak(t *testing.T) {
	m := newTestHealthMonitor(3, 2)
	st := &cameraHealthState{}

	// Two failures then a success should reset the streak so offline never fires.
	m.decide(st, false, 1)
	m.decide(st, false, 2)
	m.decide(st, true, 3)
	m.decide(st, false, 4)
	if tr, _, _ := m.decide(st, false, 5); tr != "" {
		t.Fatalf("expected no offline after intervening success reset, got %q", tr)
	}
}

func TestCameraHealthReadinessStatus(t *testing.T) {
	m := newTestHealthMonitor(3, 2)
	if got := m.ReadinessStatus(); got != "ok" {
		t.Fatalf("no cameras = %q, want ok", got)
	}
	m.state[1] = &cameraHealthState{status: cameraHealthOnline}
	m.state[2] = &cameraHealthState{status: cameraHealthOffline}
	if got := m.ReadinessStatus(); got != "degraded (1 offline)" {
		t.Fatalf("one offline = %q, want degraded (1 offline)", got)
	}
}

func TestHealthProbeAddressPrefersRTSPPort(t *testing.T) {
	cam := &CameraDetail{Camera: entities.Camera{Host: "192.168.1.50", Port: 80, RTSPUrl: "rtsp://192.168.1.50:8554/stream1"}}
	if got := healthProbeAddress(cam); got != "192.168.1.50:8554" {
		t.Fatalf("expected rtsp host:port, got %q", got)
	}
}

func TestHealthProbeAddressRTSPDefaultPort(t *testing.T) {
	cam := &CameraDetail{Camera: entities.Camera{Host: "10.0.0.2", RTSPUrl: "rtsp://10.0.0.2/cam"}}
	if got := healthProbeAddress(cam); got != "10.0.0.2:554" {
		t.Fatalf("expected default rtsp port 554, got %q", got)
	}
}

func TestHealthProbeAddressFallsBackToHost(t *testing.T) {
	cam := &CameraDetail{Camera: entities.Camera{Host: "cam.local", Port: 8080}}
	if got := healthProbeAddress(cam); got != "cam.local:8080" {
		t.Fatalf("expected host:port fallback, got %q", got)
	}

	cam = &CameraDetail{Camera: entities.Camera{Host: "cam.local"}}
	if got := healthProbeAddress(cam); got != "cam.local:80" {
		t.Fatalf("expected default port 80, got %q", got)
	}

	if got := healthProbeAddress(&CameraDetail{}); got != "" {
		t.Fatalf("expected empty address with no host, got %q", got)
	}
}

func TestNormalizeHealthSettingsClampsUnsafeValues(t *testing.T) {
	got := normalizeHealthSettings(HealthSettings{
		Enabled:           true,
		IntervalMs:        0,  // too small -> default 30000
		TimeoutMs:         99, // below 1000 -> default 5000
		FailureThreshold:  0,  // -> 3
		RecoveryThreshold: 0,  // -> 2
	})
	if got.IntervalMs != 30000 {
		t.Errorf("IntervalMs = %d, want 30000", got.IntervalMs)
	}
	if got.TimeoutMs != 5000 {
		t.Errorf("TimeoutMs = %d, want 5000", got.TimeoutMs)
	}
	if got.FailureThreshold != 3 {
		t.Errorf("FailureThreshold = %d, want 3", got.FailureThreshold)
	}
	if got.RecoveryThreshold != 2 {
		t.Errorf("RecoveryThreshold = %d, want 2", got.RecoveryThreshold)
	}

	// A huge timeout is capped; valid values pass through unchanged.
	capped := normalizeHealthSettings(HealthSettings{IntervalMs: 10000, TimeoutMs: 120000, FailureThreshold: 5, RecoveryThreshold: 4})
	if capped.TimeoutMs != 60000 {
		t.Errorf("TimeoutMs = %d, want capped at 60000", capped.TimeoutMs)
	}
	if capped.IntervalMs != 10000 || capped.FailureThreshold != 5 || capped.RecoveryThreshold != 4 {
		t.Errorf("valid values mutated: %+v", capped)
	}
}

func TestFormatHealthDuration(t *testing.T) {
	cases := map[int64]string{
		5:    "5s",
		59:   "59s",
		60:   "1m",
		150:  "2m",
		3600: "1h",
		3660: "1h1m",
		7380: "2h3m",
	}
	for seconds, want := range cases {
		if got := formatHealthDuration(seconds); got != want {
			t.Errorf("formatHealthDuration(%d) = %q, want %q", seconds, got, want)
		}
	}
}
