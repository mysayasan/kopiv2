package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// machineMitigationMinPurgeGap throttles the on-demand retention purge so a
// sustained near-full disk does not trigger a purge every sample.
const machineMitigationMinPurgeGap = 10 * time.Minute

// MachineHealthMonitor samples host CPU, memory, and disk usage on an interval,
// raises debounced warning/critical notifications on threshold crossings (and
// recovery notices), and runs automatic disk mitigation (early retention purge
// and, as a last resort, pausing NVR recording) so a full volume cannot break all
// writes including the database.
//
// It mirrors CameraHealthMonitor: settings are read live each sample, the state
// machine is debounced (a metric must breach for N consecutive samples before it
// alerts), and side effects run outside the lock.
type MachineHealthMonitor struct {
	settings  IMachineHealthSettingsService
	notifier  INotificationPublisher
	recorder  *recording.Manager
	recording IRecordingService
	// autoPaths are the volumes the app writes to (recordings, logs, working dir),
	// always monitored in addition to the user's custom disk paths.
	autoPaths []string

	mu          sync.Mutex
	states      map[string]*machineMetricState
	pausedByUs  bool
	pauseStreak int
	resumStreak int
	lastPurge   time.Time
	lastWipe    time.Time
}

// machineMetricState is the debounced level tracking for one metric (cpu, memory,
// or a specific disk). announced is the currently-notified level: 0 normal, 1
// warning, 2 critical.
type machineMetricState struct {
	announced int
	upStreak  int
	dnStreak  int
}

var machineHealthFallback = DefaultMachineHealthSettings()

// NewMachineHealthMonitor builds the host health monitor. recorder/recording may
// be nil to disable mitigation; notifier may be nil to disable notifications.
func NewMachineHealthMonitor(
	settings IMachineHealthSettingsService,
	notifier INotificationPublisher,
	recorder *recording.Manager,
	recordingService IRecordingService,
	autoPaths []string,
) *MachineHealthMonitor {
	return &MachineHealthMonitor{
		settings:  settings,
		notifier:  notifier,
		recorder:  recorder,
		recording: recordingService,
		autoPaths: normalizePathList(autoPaths),
		states:    map[string]*machineMetricState{},
	}
}

// Start launches the monitor loop in its own goroutine; it stops when ctx is done.
// Supervised: a panic here would silently stop the disk guard that keeps a full volume
// from breaking every write, including the database.
func (m *MachineHealthMonitor) Start(ctx context.Context) {
	safego.Supervise(ctx, "mymatasan.health.machine", m.run)
}

func (m *MachineHealthMonitor) run(ctx context.Context) {
	timer := time.NewTimer(2 * time.Second)
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

func (m *MachineHealthMonitor) currentSettings(ctx context.Context) MachineHealthSettings {
	if m.settings != nil {
		if cfg, err := m.settings.Get(ctx); err == nil {
			return cfg
		}
	}
	return machineHealthFallback
}

// diskPaths returns the union of auto-monitored volumes and the user's custom
// disk paths.
func (m *MachineHealthMonitor) diskPaths(cfg MachineHealthSettings) []string {
	paths := make([]string, 0, len(m.autoPaths)+len(cfg.Disk.Paths))
	paths = append(paths, m.autoPaths...)
	paths = append(paths, cfg.Disk.Paths...)
	return normalizePathList(paths)
}

// Sample returns a one-shot host metrics snapshot (used by the live readout API).
func (m *MachineHealthMonitor) Sample(ctx context.Context) MachineMetrics {
	cfg := m.currentSettings(ctx)
	metrics := sampleHostMetrics(ctx, m.diskPaths(cfg))
	if m.recorder != nil {
		metrics.RecordingPaused = m.recorder.IsPaused()
	}
	return metrics
}

// ReadinessStatus returns a compact host-health summary for the /ready payload,
// derived from the monitor's in-memory debounced state (no sampling): the worst
// announced level across CPU/memory/disk — "ok", "warning", or "critical".
func (m *MachineHealthMonitor) ReadinessStatus() string {
	m.mu.Lock()
	worst := 0
	for _, st := range m.states {
		if st != nil && st.announced > worst {
			worst = st.announced
		}
	}
	paused := m.pausedByUs
	m.mu.Unlock()

	if paused {
		return "critical (recording paused)"
	}
	switch worst {
	case 2:
		return "critical"
	case 1:
		return "warning"
	default:
		return "ok"
	}
}

// tick samples once, evaluates thresholds + mitigation, and returns the delay
// until the next sample.
func (m *MachineHealthMonitor) tick(ctx context.Context) time.Duration {
	cfg := m.currentSettings(ctx)
	interval := time.Duration(cfg.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if !cfg.Enabled {
		return interval
	}

	metrics := sampleHostMetrics(ctx, m.diskPaths(cfg))

	m.evaluate(ctx, "cpu", "CPU", metrics.CPUPercent, cfg.CPU, cfg)
	m.evaluate(ctx, "memory", "Memory", metrics.MemoryPercent, cfg.Memory, cfg)
	diskThreshold := MachineThreshold{WarnPercent: cfg.Disk.WarnPercent, CriticalPercent: cfg.Disk.CriticalPercent}
	for _, d := range metrics.Disks {
		label := "Disk " + d.Mountpoint
		m.evaluate(ctx, "disk:"+d.Mountpoint, label, d.UsedPercent, diskThreshold, cfg)
	}

	m.runMitigation(ctx, cfg, metrics)
	return interval
}

// evaluate advances the debounced level state for one metric and emits a
// notification on a level change.
func (m *MachineHealthMonitor) evaluate(ctx context.Context, key, label string, percent float64, t MachineThreshold, cfg MachineHealthSettings) {
	inst := metricLevel(percent, t.WarnPercent, t.CriticalPercent)

	m.mu.Lock()
	st := m.states[key]
	if st == nil {
		st = &machineMetricState{}
		m.states[key] = st
	}
	changed, newLevel := stepMachineLevel(st, inst, cfg.SustainedSamples, cfg.RecoverySamples)
	m.mu.Unlock()

	if !changed {
		return
	}
	m.notifyLevel(ctx, label, newLevel, percent)
}

// runMitigation applies the disk-space protective actions: an early retention
// purge, and pausing/resuming NVR recording when a volume is dangerously full.
func (m *MachineHealthMonitor) runMitigation(ctx context.Context, cfg MachineHealthSettings, metrics MachineMetrics) {
	if !cfg.Mitigation.Enabled || len(metrics.Disks) == 0 {
		return
	}
	worst := metrics.Disks[0]
	for _, d := range metrics.Disks {
		if d.UsedPercent > worst.UsedPercent {
			worst = d
		}
	}
	maxPercent := worst.UsedPercent
	maxMount := worst.Mountpoint

	// Early retention purge of expired recordings (throttled).
	if m.recording != nil && maxPercent >= float64(cfg.Mitigation.PurgeAtPercent) {
		m.mu.Lock()
		due := time.Since(m.lastPurge) >= machineMitigationMinPurgeGap
		if due {
			m.lastPurge = time.Now()
		}
		m.mu.Unlock()
		if due {
			if deleted, err := m.recording.PurgeOldSegments(ctx); err == nil && deleted > 0 {
				m.notify(ctx, notification.Info, "Disk space mitigation",
					fmt.Sprintf("%s at %.1f%% — purged %d expired recording segment(s).", maxMount, maxPercent, deleted),
					map[string]any{"mountpoint": maxMount, "usedPercent": maxPercent, "purgedSegments": deleted})
			}
		}
	}

	// Pause / resume NVR recording as a last resort to stop the volume filling.
	// With overwrite-oldest enabled, the oldest footage is wiped first so
	// recording stays continuous; pausing only happens when the wipe frees
	// nothing (all remaining footage is inside the keep floor).
	if m.recorder == nil {
		return
	}
	overwrite := cfg.Mitigation.OverwriteOldest && m.recording != nil
	pauseAt := float64(cfg.Mitigation.PauseRecordingAtPercent)
	resumeAt := float64(cfg.Mitigation.ResumePercent)

	// Pausing the recorder or overwriting footage can only relieve the volume the
	// recordings actually live on — not the global worst disk, which may well be the
	// OS/DB volume. Basing this last-resort decision on the fullest RECORDINGS disk is
	// what makes overwrite-oldest actually trim footage (and pause-mode pause for the
	// right reason) instead of pausing because some unrelated disk is full — the wipe
	// can never lower that disk, so it would otherwise fall through to Pause(). Falls
	// back to the global worst when the recordings volume can't be resolved.
	decDisk := worst
	if rd, ok := m.recordingsDisk(ctx, metrics); ok {
		decDisk = rd
	}
	decPercent := decDisk.UsedPercent
	decMount := decDisk.Mountpoint

	m.mu.Lock()
	var doAct, doResume bool
	switch {
	case decPercent >= pauseAt && !m.pausedByUs:
		m.pauseStreak++
		m.resumStreak = 0
		if m.pauseStreak >= cfg.SustainedSamples {
			m.pauseStreak = 0
			doAct = true
		}
	case m.pausedByUs && decPercent < resumeAt:
		m.resumStreak++
		m.pauseStreak = 0
		if m.resumStreak >= cfg.RecoverySamples {
			m.pausedByUs = false
			m.resumStreak = 0
			doResume = true
		}
	default:
		m.pauseStreak = 0
		m.resumStreak = 0
	}
	stillPaused := m.pausedByUs
	m.mu.Unlock()

	if doAct {
		if overwrite && m.overwriteOldestFootage(ctx, cfg, decDisk) {
			return
		}
		m.mu.Lock()
		m.pausedByUs = true
		m.mu.Unlock()
		m.recorder.Pause()
		body := fmt.Sprintf("%s reached %.1f%%. NVR recording is paused to protect the system; it resumes automatically below %d%%.", decMount, decPercent, cfg.Mitigation.ResumePercent)
		if overwrite {
			body = fmt.Sprintf("%s reached %.1f%% and overwriting could not free space (all remaining footage is newer than the %d-day keep floor). NVR recording is paused; it resumes automatically below %d%%.",
				decMount, decPercent, cfg.Mitigation.OverwriteMinKeepDays, cfg.Mitigation.ResumePercent)
		}
		m.notify(ctx, notification.Critical, "Recording paused — low disk space", body,
			map[string]any{"mountpoint": decMount, "usedPercent": decPercent, "action": "pause-recording"})
	} else if doResume {
		m.recorder.Resume()
		m.notify(ctx, notification.Info, "Recording resumed",
			fmt.Sprintf("%s dropped below %d%% — NVR recording resumed.", decMount, cfg.Mitigation.ResumePercent),
			map[string]any{"mountpoint": decMount, "usedPercent": decPercent, "action": "resume-recording"})
	} else if overwrite && stillPaused && decPercent >= resumeAt {
		// Paused with overwrite on: keep retrying (throttled). Once the wipe frees space
		// — because footage has aged past the keep floor, or the recordings disk is now
		// the one being evaluated — lift our pause immediately rather than waiting for the
		// disk to independently fall below the resume threshold.
		m.mu.Lock()
		due := time.Since(m.lastWipe) >= machineMitigationMinPurgeGap
		if due {
			m.lastWipe = time.Now()
		}
		m.mu.Unlock()
		if due && m.overwriteOldestFootage(ctx, cfg, decDisk) {
			m.mu.Lock()
			m.pausedByUs = false
			m.resumStreak = 0
			m.mu.Unlock()
			m.recorder.Resume()
			m.notify(ctx, notification.Info, "Recording resumed",
				fmt.Sprintf("%s — freed space by overwriting the oldest footage; NVR recording resumed.", decMount),
				map[string]any{"mountpoint": decMount, "usedPercent": decPercent, "action": "resume-recording"})
		}
	}
}

// recordingsDisk returns the fullest monitored disk that hosts recording storage, and
// whether one was resolved. Each camera's configured StoragePath is matched to the disk
// metric whose mountpoint contains it, so the pause/overwrite last resort targets the
// volume footage actually occupies rather than the global worst disk (which may be the
// OS/DB volume that deleting recordings can never relieve). Configs are read live, so a
// storage-path change applies on the next sample.
func (m *MachineHealthMonitor) recordingsDisk(ctx context.Context, metrics MachineMetrics) (MachineDiskMetric, bool) {
	if m.recording == nil || len(metrics.Disks) == 0 {
		return MachineDiskMetric{}, false
	}
	cfgs, err := m.recording.ListConfigs(ctx)
	if err != nil {
		return MachineDiskMetric{}, false
	}
	recMounts := map[string]bool{}
	for _, c := range cfgs {
		if c == nil {
			continue
		}
		p := absClean(c.StoragePath)
		if p == "" {
			continue
		}
		// The recordings disk is the metric whose mountpoint is the deepest ancestor of
		// this storage path (longest match wins so nested mounts resolve correctly).
		best := ""
		for _, d := range metrics.Disks {
			if pathHasPrefix(p, d.Mountpoint) && len(d.Mountpoint) > len(best) {
				best = d.Mountpoint
			}
		}
		if best != "" {
			recMounts[best] = true
		}
	}
	if len(recMounts) == 0 {
		return MachineDiskMetric{}, false
	}
	var pick MachineDiskMetric
	found := false
	for _, d := range metrics.Disks {
		if recMounts[d.Mountpoint] && (!found || d.UsedPercent > pick.UsedPercent) {
			pick = d
			found = true
		}
	}
	return pick, found
}

// absClean resolves a path to an absolute, cleaned form for mountpoint matching; a blank
// input stays blank.
func absClean(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	if a, err := filepath.Abs(p); err == nil {
		return filepath.Clean(a)
	}
	return filepath.Clean(p)
}

// overwriteOldestFootage deletes the oldest recorded segments (ignoring
// per-camera retention, but never inside the OverwriteMinKeepDays floor) until
// the given disk would drop below the resume threshold. Returns true when at
// least one segment was deleted.
func (m *MachineHealthMonitor) overwriteOldestFootage(ctx context.Context, cfg MachineHealthSettings, disk MachineDiskMetric) bool {
	keepAfter := time.Now().UTC().Add(-time.Duration(cfg.Mitigation.OverwriteMinKeepDays) * 24 * time.Hour).Unix()
	wantBytes := int64(float64(disk.TotalBytes) * (disk.UsedPercent - float64(cfg.Mitigation.ResumePercent)) / 100)
	if wantBytes <= 0 {
		wantBytes = 1
	}
	deleted, freed, err := m.recording.PurgeOldestSegments(ctx, keepAfter, wantBytes)
	if err != nil || deleted == 0 {
		return false
	}
	m.mu.Lock()
	m.lastWipe = time.Now()
	m.mu.Unlock()
	m.notify(ctx, notification.Warning, "Oldest footage overwritten — low disk space",
		fmt.Sprintf("%s reached %.1f%% — deleted the %d oldest recording segment(s) (%s freed) to keep recording continuous. Footage newer than %d day(s) is never auto-deleted.",
			disk.Mountpoint, disk.UsedPercent, deleted, formatMitigationBytes(freed), cfg.Mitigation.OverwriteMinKeepDays),
		map[string]any{"mountpoint": disk.Mountpoint, "usedPercent": disk.UsedPercent, "wipedSegments": deleted, "freedBytes": freed, "action": "overwrite-oldest"})
	return true
}

// formatMitigationBytes renders a byte count for notification bodies.
func formatMitigationBytes(n int64) string {
	const unit = 1024.0
	f := float64(n)
	for _, suffix := range []string{"B", "KB", "MB", "GB"} {
		if f < unit {
			return fmt.Sprintf("%.1f %s", f, suffix)
		}
		f /= unit
	}
	return fmt.Sprintf("%.1f TB", f)
}

// notifyLevel emits the warning/critical/recovery notification for a metric level
// change.
func (m *MachineHealthMonitor) notifyLevel(ctx context.Context, label string, level int, percent float64) {
	switch level {
	case 2:
		m.notify(ctx, notification.Critical, label+" critical",
			fmt.Sprintf("%s usage is %.1f%%.", label, percent),
			map[string]any{"metric": label, "usedPercent": percent, "level": "critical"})
	case 1:
		m.notify(ctx, notification.Warning, label+" high",
			fmt.Sprintf("%s usage is %.1f%%.", label, percent),
			map[string]any{"metric": label, "usedPercent": percent, "level": "warning"})
	default:
		m.notify(ctx, notification.Info, label+" recovered",
			fmt.Sprintf("%s usage is back to normal (%.1f%%).", label, percent),
			map[string]any{"metric": label, "usedPercent": percent, "level": "normal"})
	}
}

func (m *MachineHealthMonitor) notify(ctx context.Context, severity notification.Severity, title, body string, data map[string]any) {
	if m.notifier == nil {
		return
	}
	m.notifier.Publish(ctx, notification.Notification{
		Category: notification.CategorySystem,
		Severity: severity,
		Title:    title,
		Body:     body,
		Source:   "machine-health-monitor",
		RefType:  "machine",
		Data:     data,
	})
}

// metricLevel maps a percentage to a level: 0 normal, 1 warning, 2 critical.
func metricLevel(percent float64, warn, crit int) int {
	switch {
	case percent >= float64(crit):
		return 2
	case percent >= float64(warn):
		return 1
	default:
		return 0
	}
}

// stepMachineLevel advances a debounced metric state by one sample. A move to a
// worse level requires `sustained` consecutive samples; a move to a better level
// requires `recovery` consecutive samples. It returns whether the announced level
// changed and the new announced level. Callers hold the monitor lock.
func stepMachineLevel(st *machineMetricState, inst, sustained, recovery int) (bool, int) {
	if sustained < 1 {
		sustained = 1
	}
	if recovery < 1 {
		recovery = 1
	}
	switch {
	case inst > st.announced:
		st.upStreak++
		st.dnStreak = 0
		if st.upStreak >= sustained {
			st.announced = inst
			st.upStreak = 0
			return true, st.announced
		}
	case inst < st.announced:
		st.dnStreak++
		st.upStreak = 0
		if st.dnStreak >= recovery {
			st.announced = inst
			st.dnStreak = 0
			return true, st.announced
		}
	default:
		st.upStreak = 0
		st.dnStreak = 0
	}
	return false, st.announced
}
