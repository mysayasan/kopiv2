package services

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/rtsp"
)

// Camera health states persisted on the camera row and used in notifications.
const (
	cameraHealthOnline  = "online"
	cameraHealthOffline = "offline"
)

// CameraHealthMonitor periodically probes every active camera's network
// reachability and raises offline/recovery notifications on state transitions.
//
// Probing is TCP-first with an RTSP deep-check: a fast TCP dial to the camera's
// stream/host port decides reachability, and only when the dial fails is one
// RTSP DESCRIBE attempted to distinguish a genuinely-down camera from a quirky
// closed port. The state machine is debounced (N consecutive failures before
// offline, M consecutive successes before online) and edge-triggered, so a
// single network blip never fires an alert and the feed only carries real
// transitions.
type CameraHealthMonitor struct {
	camera   ICameraService
	rtsp     rtsp.Client
	settings IHealthSettingsService
	notifier INotificationPublisher

	// Tunables refreshed from the settings service at the start of each sweep.
	// Read by the per-camera probe goroutines; only ever written by the single
	// run loop before those goroutines are spawned, so no extra synchronization
	// is needed.
	timeout   time.Duration
	failureN  int
	recoveryN int

	mu    sync.Mutex
	state map[int64]*cameraHealthState
}

// cameraHealthState is the in-memory liveness tracking for one camera.
type cameraHealthState struct {
	// status is the announced state: "online", "offline", or "" (not yet decided).
	status string
	// failStreak / okStreak count consecutive failed / successful probes.
	failStreak int
	okStreak   int
	// offlineSince is the unix timestamp when the camera was declared offline.
	offlineSince int64
}

// healthFallbackSettings is used when the settings service is unavailable or
// returns an error, so the monitor still runs with safe defaults.
var healthFallbackSettings = HealthSettings{
	Enabled:           true,
	IntervalMs:        30000,
	TimeoutMs:         5000,
	FailureThreshold:  3,
	RecoveryThreshold: 2,
}

// NewCameraHealthMonitor builds a health monitor. Tunables (interval, timeout,
// thresholds, enabled) are read live from the settings service on every sweep,
// so changes made in the Settings UI take effect without a restart.
func NewCameraHealthMonitor(camera ICameraService, rtspClient rtsp.Client, settings IHealthSettingsService, notifier INotificationPublisher) *CameraHealthMonitor {
	return &CameraHealthMonitor{
		camera:   camera,
		rtsp:     rtspClient,
		settings: settings,
		notifier: notifier,
		state:    map[int64]*cameraHealthState{},
	}
}

// Start launches the monitor loop in its own goroutine; it stops when ctx is done.
func (m *CameraHealthMonitor) Start(ctx context.Context) {
	go m.run(ctx)
}

func (m *CameraHealthMonitor) run(ctx context.Context) {
	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(m.tick(ctx))
		}
	}
}

// currentSettings reads live settings from the service, falling back to safe
// defaults when the service is unavailable or errors.
func (m *CameraHealthMonitor) currentSettings(ctx context.Context) HealthSettings {
	if m.settings != nil {
		if cfg, err := m.settings.Get(ctx); err == nil {
			return cfg
		}
	}
	return healthFallbackSettings
}

// tick reads live settings, probes every active camera concurrently (so one
// slow/unreachable camera does not delay the rest of the sweep), and returns the
// interval until the next sweep. When the monitor is disabled it skips the sweep
// but still re-checks after the interval so it can be re-enabled on the fly.
func (m *CameraHealthMonitor) tick(ctx context.Context) time.Duration {
	cfg := m.currentSettings(ctx)
	interval := time.Duration(cfg.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if !cfg.Enabled {
		return interval
	}

	m.timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	if m.timeout <= 0 {
		m.timeout = 5 * time.Second
	}
	m.failureN = cfg.FailureThreshold
	if m.failureN <= 0 {
		m.failureN = 3
	}
	m.recoveryN = cfg.RecoveryThreshold
	if m.recoveryN <= 0 {
		m.recoveryN = 2
	}

	cameras, _, err := m.camera.Get(ctx, 1000, 0)
	if err != nil {
		return interval
	}
	var wg sync.WaitGroup
	for _, cam := range cameras {
		if cam == nil || !cam.IsActive {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(cam *CameraDetail) {
			defer wg.Done()
			up := m.probe(ctx, cam)
			m.updateState(ctx, cam, up)
		}(cam)
	}
	wg.Wait()
	return interval
}

// ReadinessStatus returns a compact camera-health summary for the /ready payload,
// derived from the monitor's in-memory state (no probing). "ok" when no camera is
// offline, otherwise "degraded (N offline)".
func (m *CameraHealthMonitor) ReadinessStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	offline := 0
	for _, st := range m.state {
		if st != nil && st.status == cameraHealthOffline {
			offline++
		}
	}
	if offline > 0 {
		return "degraded (" + strconv.Itoa(offline) + " offline)"
	}
	return "ok"
}

// probe reports whether a camera is reachable. It dials TCP first and only falls
// back to a full RTSP DESCRIBE when the dial fails, so a healthy camera costs one
// cheap connection per sweep.
func (m *CameraHealthMonitor) probe(ctx context.Context, cam *CameraDetail) bool {
	return m.probeWithTimeout(ctx, cam, m.timeout)
}

// probeWithTimeout is probe with an explicit per-attempt timeout, used by the
// on-demand probe so it does not depend on or mutate the sweep's m.timeout field.
func (m *CameraHealthMonitor) probeWithTimeout(ctx context.Context, cam *CameraDetail, timeout time.Duration) bool {
	if addr := healthProbeAddress(cam); addr != "" && m.tcpReachableWithTimeout(ctx, addr, timeout) {
		return true
	}
	rtspURL := healthRTSPProbeURL(cam)
	if rtspURL == "" {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := m.rtsp.Probe(probeCtx, rtspURL, rtsp.OpenOptions{ReadTimeout: timeout, WriteTimeout: timeout})
	return err == nil
}

// tcpReachableWithTimeout dials a host:port with the given timeout and reports success.
func (m *CameraHealthMonitor) tcpReachableWithTimeout(ctx context.Context, addr string, timeout time.Duration) bool {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// CameraHealthSnapshot is a single-shot reachability result for one camera.
type CameraHealthSnapshot struct {
	CameraId  int64  `json:"cameraId"`
	Status    string `json:"status"`
	CheckedAt int64  `json:"checkedAt"`
}

// ProbeAllNow runs a one-shot concurrent reachability probe of every active
// camera and returns the raw result, so callers (e.g. the UI at login) learn
// which cameras are offline immediately instead of waiting for the debounced
// background sweep. Probes run in parallel under a short capped timeout, so N
// offline cameras cost roughly one timeout window rather than N. Results are
// persisted so the stored badge reflects the latest check; the debounced state
// machine that drives offline/recovery notifications is intentionally left
// untouched (it keeps owning alert suppression).
func (m *CameraHealthMonitor) ProbeAllNow(ctx context.Context) []CameraHealthSnapshot {
	cfg := m.currentSettings(ctx)
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	// Cap the on-demand timeout so an unreachable camera cannot stall login UX.
	if timeout <= 0 || timeout > 3*time.Second {
		timeout = 3 * time.Second
	}

	cameras, _, err := m.camera.Get(ctx, 1000, 0)
	if err != nil {
		return nil
	}
	now := time.Now().UTC().Unix()
	results := make([]CameraHealthSnapshot, len(cameras))
	var wg sync.WaitGroup
	for i, cam := range cameras {
		if cam == nil || !cam.IsActive {
			continue
		}
		wg.Add(1)
		go func(i int, cam *CameraDetail) {
			defer wg.Done()
			status := cameraHealthOffline
			if m.probeWithTimeout(ctx, cam, timeout) {
				status = cameraHealthOnline
			}
			results[i] = CameraHealthSnapshot{CameraId: cam.Camera.Id, Status: status, CheckedAt: now}
			_ = m.camera.UpdateHealth(ctx, cam.Camera.Id, status, now)
		}(i, cam)
	}
	wg.Wait()

	out := make([]CameraHealthSnapshot, 0, len(results))
	for _, r := range results {
		if r.CameraId != 0 {
			out = append(out, r)
		}
	}
	return out
}

// updateState advances the debounced state machine for one camera and, on a
// state transition, persists the new status and emits the matching notification.
// Notifications and persistence run outside the lock.
func (m *CameraHealthMonitor) updateState(ctx context.Context, cam *CameraDetail, up bool) {
	id := cam.Camera.Id
	now := time.Now().UTC().Unix()

	m.mu.Lock()
	st := m.state[id]
	if st == nil {
		st = &cameraHealthState{}
		m.state[id] = st
	}
	prevStatus := st.status
	transition, offlineFor, newStatus := m.decide(st, up, now)
	m.mu.Unlock()

	// Persist whenever the announced status changes — including the initial
	// "" -> online, which emits no notification. Without this, a camera that is
	// online from startup and never fails keeps a blank ("unknown") stored badge
	// until an on-demand ProbeAllNow happens to run, so a perfectly healthy
	// camera can look un-probed indefinitely.
	if newStatus != "" && newStatus != prevStatus {
		if err := m.camera.UpdateHealth(ctx, id, newStatus, now); err != nil {
			// Persistence is best-effort; the in-memory state remains authoritative
			// for transition detection, so a failed write only loses the stored badge.
			_ = err
		}
	}

	if transition == "" {
		return
	}

	name := ""
	if m.camera != nil {
		name = m.camera.DisplayName(ctx, id)
	}
	switch transition {
	case "offline":
		NotifyCameraOffline(ctx, m.notifier, cam, name)
	case "recovery":
		NotifyCameraRecovered(ctx, m.notifier, cam, name, offlineFor)
	}
}

// decide advances the debounced state machine for one camera and returns the
// resulting transition ("offline", "recovery", or "" for none), the downtime in
// seconds for a recovery, and the new announced status. It mutates st; callers
// must hold m.mu. Side effects (persistence, notifications) are the caller's job.
func (m *CameraHealthMonitor) decide(st *cameraHealthState, up bool, now int64) (transition string, offlineFor int64, newStatus string) {
	if up {
		st.okStreak++
		st.failStreak = 0
		if st.status != cameraHealthOnline && st.okStreak >= m.recoveryN {
			// Only announce recovery if we had previously announced offline; the
			// initial "" -> online transition is the normal healthy state.
			wasOffline := st.status == cameraHealthOffline
			if wasOffline && st.offlineSince > 0 {
				offlineFor = now - st.offlineSince
			}
			st.status = cameraHealthOnline
			st.offlineSince = 0
			if wasOffline {
				transition = "recovery"
			}
		}
	} else {
		st.failStreak++
		st.okStreak = 0
		if st.status != cameraHealthOffline && st.failStreak >= m.failureN {
			st.status = cameraHealthOffline
			st.offlineSince = now
			transition = "offline"
		}
	}
	return transition, offlineFor, st.status
}

// healthProbeAddress returns the host:port to dial for a camera, preferring the
// resolved RTSP stream endpoint and falling back to the device host/port.
func healthProbeAddress(cam *CameraDetail) string {
	if addr := rtspHostPort(cam.RTSPUrl); addr != "" {
		return addr
	}
	host := strings.TrimSpace(cam.Host)
	if host == "" {
		return ""
	}
	port := cam.Port
	if port <= 0 {
		port = 80
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// rtspHostPort extracts host:port from an RTSP URL, defaulting the port to 554.
func rtspHostPort(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		port = "554"
	}
	return net.JoinHostPort(parsed.Hostname(), port)
}

// healthRTSPProbeURL returns the camera's RTSP URL with credentials injected, or
// "" when the camera has no RTSP stream to deep-check.
func healthRTSPProbeURL(cam *CameraDetail) string {
	raw := strings.TrimSpace(cam.RTSPUrl)
	if raw == "" {
		return ""
	}
	return RTSPURIWithCredentials(raw, cam.Username, cam.Password)
}
