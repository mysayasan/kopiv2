# Module: apps/mymatasan/services/camera_tamper_monitor.go

## Purpose

The third health question: **is this camera still showing what it is supposed to show?**

The other two both answer "yes" while this one fails. `CameraHealthMonitor` asks whether the camera answers — a covered lens answers fine. `RecordingContinuityMonitor` asks whether footage is being written — footage of a wall is still footage. Only this monitor notices that somebody put a bag over the dome, turned it to face the ceiling, knocked it out of focus, or that the video froze while the connection stayed up. That is what an attacker arranges *before* doing anything worth recording.

No ML and no dependency on the detector: it reads the JPEG the recorder already siphons for the AI pipeline and runs arithmetic from `infra/vision/tamper.go`.

## What it detects

| Kind | Signal | Guard against false positives |
|---|---|---|
| `frozen` | Mean per-pixel difference between two frames at or below `FrozenMaxDifference` (essentially zero) | Judged over `FrozenSeconds`, not samples. A live camera watching an empty room still has sensor noise, so an exact zero is a stopped source — but a few identical frames happen, and a minute of them does not. Also requires the siphon's capture timestamp to have advanced, so a stalled siphon is not mistaken for a stopped camera |
| `covered` | Edge energy (variance of a discrete Laplacian) collapses below `CoveredRatio` × this camera's own recent median | **Suppressed entirely in low light** — see below. The baseline is per-camera and rolling |
| `moved` | Luma-histogram distance ≥ `MovedDistance` | Position-blind on purpose: a person crossing frame barely moves the histogram, a camera turned to a wall changes all of it. Plus the `FailureThreshold` debounce |

## The two decisions that make it usable

**Low-light suppression.** At night a scene loses most of its edge energy legitimately, and under infrared it loses contrast as well. Without this guard every camera in the fleet reports a covered lens at dusk and the whole feature is muted by morning — after which it protects nothing. The trade is explicit: a lens covered overnight is not reported until first light. That is the right side of the trade, and `TestTamperStaysQuietOnADarkNightScene` fails if the guard is removed.

**A per-camera rolling baseline, and alerting samples excluded from it.** An absolute edge-energy threshold cannot be right for both a busy loading bay and a plain corridor, nor for the same camera at noon and at dusk; a median over `tamperBaselineSize` recent readings tracks daylight's gradual change while a hand over the lens is a step change away from it. Readings taken *while already alerting* are deliberately not folded in — otherwise a lens left covered for an hour becomes the camera's new normal and the alert clears itself with the lens still covered.

## Notes

- Cameras with recording disabled are skipped: they have no siphon frame, so the monitor has nothing to say about them, and silence is more honest than reporting them healthy.
- The first sweep is delayed 90 seconds after boot — recorders have not produced a siphon frame yet, and a camera with no frame must not read as a fault.
- Alerts ride `notification.CategoryHealthCheck` so they land in the feed, breakdowns and destinations an operator already watches, rather than a new category nobody has configured. Notification text is written for the person reading it at 2am: *"the lens may be covered, sprayed or out of focus"*, not *"edge energy below baseline ratio"*.
- Edge-triggered per kind: raised once, cleared once. `frozen`, `covered` and `moved` track independently, so a camera can be covered without also being reported as moved.
- Metric `mymatasan_camera_tamper_total{kind}`.
- Settings under the `tamper` runtime-setting key, read live each sweep — retuning this one is done by watching it on a real site, so a restart would be in the way. Defaults lean conservative in every direction, because the failure mode that kills the feature is not a missed tamper, it is an operator muting it after a week of false alarms.
- The analysis primitives are pure functions over a `Fingerprint` and are tested separately in `infra/vision/tamper_test.go` against generated scenes (sharp / blurred / flat / shifted / person-crossing) rather than checked-in binary fixtures — the properties under test are mathematical, and a generator states that intent more clearly than a blob nobody can inspect in review.
- Covered by `camera_tamper_monitor_test.go`: a covered lens is detected, a dark night scene is not, a covered lens does not become the new normal, recovery clears, a frozen picture is detected, a quiet-but-live scene is not, a stalled siphon is not, alerts fire once rather than per sweep, disabled cameras are skipped, and nonsense settings normalize.
- **Not yet live-benched.** See `docs/FLAGSHIP_BENCH_CHECKLIST.md`.
