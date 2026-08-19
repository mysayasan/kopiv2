package services

import "github.com/mysayasan/kopiv2/infra/telemetry"

// The metrics an operator needs to diagnose a site they cannot log into.
//
// Prometheus was already shipped and wired to /metrics, but nothing outside the shared
// API middleware ever recorded anything — so the questions that actually matter on a
// customer box ("is the detector keeping up?", "is ffmpeg thrashing?", "are segments
// failing to finalize?", "are notifications being dropped?") had no answer at all.
//
// Naming: kopiv2_* for metrics emitted by shared infra (they are app-neutral, and
// myiotsan will emit the same ones), mymatasan_* for app-specific ones.
//
// Label discipline: every label below is bounded (a camera id, a small outcome enum).
// Never label with an error string, a file path or a timestamp — each distinct value is
// a series held for the life of the process. The recorder caps cardinality and says so
// in the scrape, but the cap is a backstop, not a licence.
const (
	// MetricInferenceDurationMs is how long one detector pass took, per camera. The
	// first number to look at when detection "feels slow" or frames are being skipped.
	MetricInferenceDurationMs = "mymatasan_inference_duration_ms"
	// MetricFramesTotal counts sampled frames by outcome (ok | capture_failed |
	// detect_failed). A camera silently failing every capture shows up here first.
	MetricFramesTotal = "mymatasan_frames_total"
	// MetricAlertsTotal counts emitted alerts by kind (detection | diagnostic), so a
	// diagnostic flood is distinguishable from real activity.
	MetricAlertsTotal = "mymatasan_alerts_total"
	// MetricCameraOnline is 1 when a camera's last health probe succeeded, else 0.
	MetricCameraOnline = "mymatasan_camera_online"
	// MetricCamerasOffline is how many cameras are currently unreachable — the single
	// number worth alerting on.
	MetricCamerasOffline = "mymatasan_cameras_offline"
	// MetricDiskUsedPercent is per-mount disk usage, including the recordings volume.
	MetricDiskUsedPercent = "mymatasan_disk_used_percent"
	// MetricRecordingPaused is 1 while the machine-health disk guard has recording
	// paused. Footage is not being written while this is 1.
	MetricRecordingPaused = "mymatasan_recording_paused"
	// MetricDiskMitigationTotal counts disk-guard actions (purge | overwrite | pause |
	// resume).
	MetricDiskMitigationTotal = "mymatasan_disk_mitigation_total"
	// MetricAuditWriteFailuresTotal counts audit entries that could not be persisted.
	// The audit service swallows its own write errors on purpose (auditing must never
	// fail the action being audited), so this counter is the ONLY symptom a trail that
	// has stopped recording produces — every other signal stays green while the evidence
	// history quietly develops a hole.
	MetricAuditWriteFailuresTotal = "mymatasan_audit_write_failures_total"
	// MetricAuditRetentionPurgedTotal counts audit rows removed by age-based retention.
	MetricAuditRetentionPurgedTotal = "mymatasan_audit_retention_purged_total"
)

// DescribeMetrics registers help text so a scrape is readable by someone who did not
// write this code. Call once at startup.
func DescribeMetrics(m telemetry.Metrics) {
	if m == nil {
		return
	}
	m.Describe(MetricInferenceDurationMs, "Detector inference duration in milliseconds, per camera.")
	m.Describe(MetricFramesTotal, "Sampled frames by outcome (ok, capture_failed, detect_failed).")
	m.Describe(MetricAlertsTotal, "Alerts emitted by kind (detection, diagnostic).")
	m.Describe(MetricCameraOnline, "1 when the camera's last health probe succeeded, else 0.")
	m.Describe(MetricCamerasOffline, "Number of cameras currently unreachable.")
	m.Describe(MetricDiskUsedPercent, "Disk usage percent per mount.")
	m.Describe(MetricRecordingPaused, "1 while the disk guard has recording paused (no footage is being written).")
	m.Describe(MetricDiskMitigationTotal, "Disk guard actions taken (purge, overwrite, pause, resume).")
	m.Describe(MetricRecordingCoveragePercent, "Percentage of the last scored hour that has footage on disk, per camera.")
	m.Describe(MetricRecordingGapCameras, "Cameras currently alerting for missing footage.")
	m.Describe(MetricAuditWriteFailuresTotal, "Audit entries that could not be persisted. The audit service swallows its own write errors, so this is the only symptom a trail that stopped recording produces.")
	m.Describe(MetricCameraTamperTotal, "Camera tamper alerts raised, by kind (frozen, covered, moved).")
	m.Describe(MetricAuditRetentionPurgedTotal, "Audit rows removed by age-based retention.")
}
