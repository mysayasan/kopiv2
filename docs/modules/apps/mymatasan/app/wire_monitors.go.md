# Module: apps/mymatasan/app/wire_monitors.go

## Purpose

`startBackgroundWorkers` launches every long-lived background worker under one context, and
`startRetentionPurges` runs the three retention purge loops. Moved out of `app.go` (Tier 2
phase D2).

## Responsibilities

- `startBackgroundWorkers(ctx context.Context, w *wiring)`:
  - When `w.visionMonitorSettings.Enabled`: warms the detector model once at startup via
    `safego.Go("mymatasan.vision.warmup", ...)` (one-shot, 120s timeout, uncapped so the
    first live-detection inference doesn't hit the per-frame timeout on a cold GPU/CUDA
    model load — which would kill and restart the worker in a loop and never warm);
    starts `w.metadata.Start(ctx)` (shares the monitor's lifecycle — aggregates
    observations the monitor feeds it and flushes open intervals on shutdown); starts
    `services.NewVisionMonitor(w.camera, w.vision, w.settings, w.visionMonitorSettings).Start(ctx)`.
  - Always starts `w.cameraHealth.Start(ctx)` and `w.machineHealth.Start(ctx)` — both read
    their settings live on every sweep, so there is deliberately no startup `Enabled` gate;
    enabling/retuning from the Settings UI takes effect without a restart.
  - Always starts `services.NewRecordingContinuityMonitor(w.recording, w.camera,
    w.continuitySettings, w.notification, w.recorder, deps.Metrics).Start(ctx)` — the third
    health question after "is it reachable" (`cameraHealth`): "did we actually write video".
    Scores whole CLOSED hours, so it reads its settings live like the others but only ever
    acts on completed history. See `services/recording_continuity.go.md`.
  - Always starts `services.NewCameraTamperMonitor(w.recorder, w.camera, w.recording,
    w.tamperSettings, w.notification, deps.Metrics).Start(ctx)` — the fourth: the only one
    that notices a camera which answers, records, and is pointing at a wall. Reads the JPEG
    the recorder already siphons for the detector, so it costs a decode per camera per sweep
    and nothing else. See `services/camera_tamper_monitor.go.md`.
  - Starts `w.notificationRollup.Start(ctx)` — incrementally aggregates the notifications
    feed into the hourly rollup table backing dashboard analytics; the first sweep
    backfills existing history.
  - Starts `services.NewAnalyticsMonitor(w.notification, w.notification, w.anomalySettings,
    w.camera).Start(ctx)` — scores each closed hour against per-camera baselines and raises
    spike/"unusual silence" alerts; opt-in, reads settings live.
  - When `pairing.enabled` (default true): builds and runs the discovery responder
    (`infra/pairing.NewResponder`, gated by fleet key + `Discoverable()`, both read live)
    under `safego.Supervise(ctx, "mymatasan.pairing.responder", ...)`; then starts the
    three fleet loops — `safego.Supervise(ctx, "mymatasan.fleet.enrollment", w.enrollment.Run)`,
    `"mymatasan.fleet.control"`, `"mymatasan.fleet.media"`.
  - Calls `startRetentionPurges(ctx, w)`.
- `startRetentionPurges(ctx context.Context, w *wiring)` — three `periodic(...)` calls
  (the shared helper in `app.go`):
  - `mymatasan.purge.segments` (fixed 6h): `w.recording.PurgeOldSegments` and
    `w.observation.PurgeOldObservations`. Errors are logged via `deps.Logger.Warnf`.
  - `mymatasan.purge.notifications` (`notification.purgeIntervalHours`, default 6h): reads
    retention (days/onlyRead) live from `w.notificationSettings` each run, so UI changes
    take effect without a restart.
  - `mymatasan.purge.alerts` (`vision.alertPurgeIntervalHours`, default 6h, read off
    `w.appCfg.Vision` — mymatasan's own config since Tier 2 phase C, previously
    `deps.Config.Vision`): purges `alert_event` — diagnostic rows when
    `vision.diagnosticRetentionDays > 0`, real detection alerts too when
    `vision.alertRetentionDays > 0`. Both unlink snapshot image files for removed rows.

## Notes

- **Latent bug fix**: the three fleet loops (`enrollment.Run`/`control.Run`/`media.Run`)
  were previously bare `go` calls in `app.go`. A panic in any of them took the whole
  process down, and their death was otherwise silent — the node would simply stop
  enrolling, stop answering the parent, or stop relaying live video, with nothing to say
  why. They are now `safego.Supervise`d alongside the discovery responder, which was
  already supervised.
- Being supervised matters specifically for the purge loops too: a panic inside one used
  to kill the process, and simply recovering it would be no better, since nothing else
  notices a dead purge loop — the disk quietly fills (database writes included) until the
  next restart happens to bring it back.
- Takes the whole `wiring` struct (`wire_services.go.md`) so a single `stopMonitor()` call
  (in `app.go`) quiesces everything started here — exactly what a factory reset needs
  before it can shred the files ffmpeg is holding open.
- Pure move from `app.go` plus the supervision fix above; every interval, gate, and
  ordering is otherwise unchanged.
