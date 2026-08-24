package app

import (
	"context"
	"log"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/pairing"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// startBackgroundWorkers launches every long-lived worker under one context, so a single
// stopMonitor() call quiesces all of them (which is exactly what a factory reset needs
// before it can shred the files ffmpeg is holding open).
//
// Everything here is supervised. These are the workers whose silent death is invisible:
// nothing re-creates them, so a panic used to take the whole process down and a bare
// recover would have left the subsystem quietly dead instead. See infra/safego.
func startBackgroundWorkers(ctx context.Context, w *wiring) {
	deps := w.deps

	if w.visionMonitorSettings.Enabled {
		// Warm the detector model once at startup, uncapped, so the first live-detection
		// inference doesn't hit the per-frame timeout on a cold GPU/CUDA model load — which
		// would kill and restart the worker in a loop and never warm. Backgrounded so boot
		// isn't blocked by the (potentially 10–15s) model load.
		if w.objectBackend != nil {
			// One-shot: guarded but never restarted — if warmup panics, live detection simply
			// warms on the first real inference instead.
			safego.Go("mymatasan.vision.warmup", func() {
				warmCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
				defer cancel()
				if err := services.WarmupInference(warmCtx, w.objectBackend); err != nil {
					log.Printf("vision: detector warmup failed (live detection will warm on first inference): %v", err)
				} else {
					log.Printf("vision: detector model warmed up")
				}
			})
		}
		// The metadata recorder shares the monitor's lifecycle: it aggregates the
		// observations the monitor feeds it and flushes open intervals on shutdown.
		w.metadata.Start(ctx)
		// WithPTZ lets a rule point a camera at what it just detected. Optional by
		// design: an appliance with no PTZ camera passes nothing and every rule behaves
		// as it did before.
		services.NewVisionMonitor(w.camera, w.vision, w.settings, w.visionMonitorSettings).
			WithPTZ(w.ptz).WithRelays(w.relays).Start(ctx)
	}

	// The health monitors read their settings live on every sweep, so enabling or retuning
	// them from the Settings UI takes effect without a restart — there is deliberately no
	// startup Enabled gate on either.
	w.cameraHealth.Start(ctx)
	w.machineHealth.Start(ctx)

	// Recording continuity. The health monitors above answer "is the camera reachable",
	// which is a different question from "did we actually write video" — a wedged ffmpeg,
	// a full disk or a quarantine loop all leave a camera reachable and record nothing.
	// It scores whole CLOSED hours, so it reads its settings live like the others but only
	// ever acts on completed history.
	services.NewRecordingContinuityMonitor(
		w.recording, w.camera, w.continuitySettings, w.notification, w.recorder, deps.Metrics,
	).Start(ctx)

	// Camera tamper. The third health question, and the only one that notices a camera
	// which answers, records, and is pointing at a wall. It reads the JPEG the recorder
	// already siphons for the detector, so it costs a decode per camera per sweep and
	// nothing else.
	//
	// It takes the PTZ motion journal so it can tell a camera that was re-aimed by an
	// intruder from one that was re-aimed by us. Without it, a guard tour raises a MOVED
	// alert every few minutes on the camera it is patrolling — and the operator's fix is
	// to turn tamper detection off, after which it protects nothing.
	services.NewCameraTamperMonitor(
		w.recorder, w.camera, w.recording, w.tamperSettings, w.notification, deps.Metrics,
	).WithPTZ(w.ptzJournal).Start(ctx)

	// The camera event listener (W3-5b): what the CAMERA noticed, including whatever is
	// wired into its terminal block. Opt-in — it opens a long-lived connection per camera —
	// and it reads that setting live, so turning it on takes effect without a restart.
	services.NewCameraEventMonitor(
		w.camera, onvif.NewClient(), w.eventSettings, w.notification, deps.Metrics,
	).Start(ctx)

	// PTZ guard tours: the patrol loop. It resumes tours that were running before a
	// restart, which is why IsRunning is a persisted column — an appliance that reboots at
	// 03:00 must come back doing what it was doing, with nobody awake to restart it.
	w.ptz.Start(ctx)

	// Incrementally aggregates the notifications feed into the hourly rollup table that
	// backs dashboard analytics. The first sweep backfills existing history.
	w.notificationRollup.Start(ctx)

	// Scores each closed hour against per-camera baselines (built from the rollup) and
	// raises spike / "unusual silence" alerts. Opt-in; reads its settings live.
	services.NewAnalyticsMonitor(w.notification, w.notification, w.anomalySettings, w.camera).Start(ctx)

	if boolValue(deps.Config.Pairing.Enabled, true) {
		// Answers authenticated pairing probes while the node is unpaired and a fleet key is
		// set, then goes silent once adopted. The fleet key and discoverability are read live
		// per probe, so setting a key or adopting the node takes effect without a restart.
		responder := pairing.NewResponder(pairing.ResponderConfig{
			FleetKey:      func() []byte { k, _ := w.pairing.FleetKey(ctx); return k },
			Discoverable:  func() bool { return w.pairing.Discoverable(ctx) },
			AnnounceInfo:  func() pairing.AnnounceInfo { return w.pairing.AnnounceInfo(ctx, w.httpsPort) },
			MulticastAddr: deps.Config.Pairing.MulticastAddr,
			ReplayWindow:  time.Duration(deps.Config.Pairing.ReplayWindowSeconds) * time.Second,
			Logf:          func(format string, args ...any) { deps.Logger.Infof("mymatasan.pairing", format, args...) },
		})
		safego.Supervise(ctx, "mymatasan.pairing.responder", func(ctx context.Context) {
			if err := responder.Run(ctx); err != nil && ctx.Err() == nil {
				deps.Logger.Warnf("mymatasan.pairing", "discovery responder stopped: %v", err)
			}
		})

		// The three fleet loops. These were bare `go` calls: a panic in any of them took the
		// whole process down, and their death is otherwise silent — the node simply stops
		// enrolling, stops answering the parent, or stops relaying live video, with nothing
		// to say why.
		safego.Supervise(ctx, "mymatasan.fleet.enrollment", w.enrollment.Run)
		safego.Supervise(ctx, "mymatasan.fleet.control", w.control.Run)
		safego.Supervise(ctx, "mymatasan.fleet.media", w.media.Run)
	}

	startRetentionPurges(ctx, w)
}

// startRetentionPurges runs each retention sweep once at startup and then on its interval.
//
// A dead purge loop is invisible: nothing re-creates it, and the disk quietly fills until
// every write fails — the database's included. So they are supervised, and the errors they
// used to discard silently are logged.
func startRetentionPurges(ctx context.Context, w *wiring) {
	deps := w.deps

	interval := func(hours int) time.Duration {
		if d := time.Duration(hours) * time.Hour; d > 0 {
			return d
		}
		return 6 * time.Hour
	}

	// Expired segments + metadata observations. Observation retention aligns with each
	// camera's recording retention.
	periodic(ctx, "mymatasan.purge.segments", 6*time.Hour, func(ctx context.Context) {
		if _, err := w.recording.PurgeOldSegments(ctx); err != nil {
			deps.Logger.Warnf("mymatasan.recording", "segment retention purge failed: %v", err)
		}
		if _, err := w.observation.PurgeOldObservations(ctx); err != nil {
			deps.Logger.Warnf("mymatasan.recording", "observation retention purge failed: %v", err)
		}
	})

	// Expired notifications. Retention (days / onlyRead) is read live from the notification
	// settings each run, so UI changes take effect without a restart.
	periodic(ctx, "mymatasan.purge.notifications", interval(deps.Config.Notification.PurgeIntervalHours), func(ctx context.Context) {
		days, onlyRead := w.notificationSettings.Retention(ctx)
		if days <= 0 {
			return
		}
		if deleted, err := w.notification.PurgeOlderThanDays(ctx, days, onlyRead); err != nil {
			deps.Logger.Warnf("mymatasan.notification", "notification purge failed: %v", err)
		} else if deleted > 0 {
			deps.Logger.Infof("mymatasan.notification", "purged %d expired notifications", deleted)
		}
	})

	// Expired AI detection alerts. DiagnosticRetentionDays trims the noisy vision-monitor
	// diagnostics; AlertRetentionDays (0 = keep forever) trims real detections too. Both
	// also unlink the snapshot image files of the rows they remove.
	periodic(ctx, "mymatasan.purge.alerts", interval(w.appCfg.Vision.AlertPurgeIntervalHours), func(ctx context.Context) {
		if days := w.appCfg.Vision.DiagnosticRetentionDays; days > 0 {
			if deleted, err := w.vision.PurgeAlertsOlderThanDays(ctx, days, true); err != nil {
				deps.Logger.Warnf("mymatasan.vision", "diagnostic alert purge failed: %v", err)
			} else if deleted > 0 {
				deps.Logger.Infof("mymatasan.vision", "purged %d diagnostic alerts older than %d day(s)", deleted, days)
			}
		}
		if days := w.appCfg.Vision.AlertRetentionDays; days > 0 {
			if deleted, err := w.vision.PurgeAlertsOlderThanDays(ctx, days, false); err != nil {
				deps.Logger.Warnf("mymatasan.vision", "alert purge failed: %v", err)
			} else if deleted > 0 {
				deps.Logger.Infof("mymatasan.vision", "purged %d alerts older than %d day(s)", deleted, days)
			}
		}
	})
}
