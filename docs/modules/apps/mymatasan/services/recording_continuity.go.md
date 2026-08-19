# Module: apps/mymatasan/services/recording_continuity.go

## Purpose

The recording-continuity monitor: does each camera that is supposed to be recording actually have footage on disk?

`CameraHealthMonitor` (`services/camera_health_monitor.go.md`) probes reachability. It is well built and it answers a different question — a camera can be perfectly reachable while ffmpeg is wedged, the disk is full, the remux queue is quarantining every segment, or the stream URL silently changed. Every one of those records nothing and reports green, and the operator finds out at the worst possible moment: when somebody asks for footage of an incident.

The data was already there. `RecordingSegment` carries `StartedAt`/`EndedAt` under a `cam_time` composite index, and `RecordingConfig.Enabled` says which cameras are supposed to be recording. This is a query, not a new pipeline. The coverage maths lives in `services/recording_coverage.go.md`.

## Responsibilities

- `RecordingContinuityMonitor` — mirrors `CameraHealthMonitor`'s shape: `Start(ctx)` under `safego.Supervise`, settings read live on every sweep, per-camera debounce, and a notification only on a transition.
- `Sweep(ctx, cfg)` — scores one closed hour for every recording-enabled camera. Exported so a test can drive a sweep without a timer.
- Publishes `Recording gap` (critical) and `Recording resumed` (info) into `notification.CategoryHealthCheck`, so they land in the same feed, breakdowns and destinations an operator already watches for "this camera has a problem" rather than a new category nobody has configured.
- Metrics: `mymatasan_recording_coverage_percent` (per camera, last scored hour) and `mymatasan_recording_gap_cameras` (the single number worth alerting on).
- Configuration in `services/continuity_settings.go` under the `continuity` runtime-setting key. Default **on**: an NVR that is not checking whether it recorded anything is the failure this exists to catch, and an operator who has to discover the feature and switch it on will not.

## The decisions that make it usable rather than noisy

A monitor that cries wolf gets muted, and a muted monitor protects nothing. Five things stop that:

- **Only CLOSED hours are scored.** An hour in progress is legitimately under-covered for the whole of it; scoring it would raise a gap on every healthy camera, every sweep — the same trap `AnalyticsMonitor` documents for its baselines.
- **Each hour is scored once.** The sweep interval (10 min) is shorter than an hour, so the same closed hour is visited repeatedly. Without `lastScoredHour` the bad streak would pass any threshold within the first hour, turning a debounce meant to span hours into no debounce at all.
- **95% default, not 100%.** Segment rollover, a recorder restart and the remux queue cost a few seconds an hour legitimately. 95% still catches a camera that recorded 40 minutes of 60.
- **The disk guard's pause suppresses the alert.** `machine_health_monitor` pauses every recorder when the volume is nearly full and raises its own alert; scoring through that would blame the whole fleet for one disk problem. The streak still advances, so a camera already failing before the pause does not have its history reset by it.
- **Gaps are attributed.** If the reachability monitor already has the camera offline, the notification says so (`reason: "camera-offline"`) instead of raising a second independent fault. One incident, one story.

## Notes

- **Detect-only cameras are excluded for free.** `Manager.EnsureDetectionStream` builds its own config and sets `Enabled` on that copy only — never on the stored `RecordingConfig` row — so a detect-only AI frame source is correctly absent from the sweep rather than being blamed for writing no segments by design.
- The first sweep is delayed two minutes after boot: recorders have not started yet, and the previous hour was recorded by the previous process. Scoring it immediately would alert on an hour this instance was never responsible for.
- `recorderPauseState` is a one-method interface over `recording.Manager` on purpose — the monitor needs one bool from the recorder and should not be able to reach anything else.
- Covered by `recording_continuity_test.go`: the threshold fires only after consecutive bad hours, the alert is edge-triggered (once, not per sweep), it clears on recovery, a normal hour stays silent, a gap on an offline camera is attributed, a paused recorder suppresses per-camera gaps, disabled cameras are never scored, re-scoring one hour cannot satisfy a multi-hour threshold, and the window scored is the previous closed hour rather than the current one.
- **Not yet live-benched.** The plan's acceptance test (W1-3) is: kill a camera's ffmpeg child without stopping the recorder and confirm a `recording.gap` alert names the camera and window within two sweeps, the coverage strip shows the hole, and restoring it clears the alert.
