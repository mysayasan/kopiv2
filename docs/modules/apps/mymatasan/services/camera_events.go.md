# Module: apps/mymatasan/services/camera_events.go

## Purpose

`CameraEventMonitor`: what the **camera** noticed (W3-5b).

Everything else this appliance detects, it detects itself — it pulls a frame and runs a model
over it. This is the other direction. A camera has its own opinions and its own senses, and
the ones no amount of video analysis can substitute for are the **digital inputs** wired into
its terminal block: a door contact, a PIR, a beam across a gate, a panic button under a
counter. Those are facts about the physical world arriving on a wire.

The product already told the operator a camera supported events — `CameraCapabilities.Events`
has been rendered as a pill on the camera page since before this — and did nothing with it.

## What makes it hard is that the failure mode is silence

ONVIF's PullPoint transport is a **subscription with a lease**. The camera drops it without a
word if it is not renewed, and a door contact that has stopped reporting looks exactly like a
door nobody has opened.

So a subscription the monitor cannot keep alive is an **alertable fault**, not something to
retry quietly: after `LostAfterSeconds` it raises *"Camera events stopped"*, naming the camera
and saying plainly that anything wired to its inputs is not being reported. Same principle as
the recording-continuity and tamper monitors — what fails silently has to be instrumented, or
it is indistinguishable from working.

Renewal happens at **two thirds of the lease the device reported**, never later. A renewal
that is merely "before expiry" races the camera's clock, its rounding and the round trip —
and losing that race costs the whole subscription, silently. The lease is measured on the
*device's* clock (`EventSubscription.Lease()`), because a camera whose clock is wrong is
common enough that this app has a whole date/time screen.

## Initialized is not an event

On subscribing, a camera sends the **current state of every property it publishes**. A
building with four closed door contacts announces four closed door contacts the instant we
connect.

Treated as alerts, every restart, every renewal failure and every network blip would raise a
burst of alarms for doors nobody touched — at exactly the moments when an operator is least
able to tell a real one from noise. They are not discarded either: they are what tells us the
state to compare the next message against.

This is the trap the whole file is arranged around, and it is pinned by
`TestTheInitialStateOfAnInputIsNotAnAlarm` and by the live bench, which connects and asserts
nothing was raised.

## The feed is the home, and the alert log is not

The first version of this code also wrote a row into the AI alert log, "so a door contact is
searchable next to the detections". **The live bench found that it never appeared.**
`alert_event` is the *detection* log: every row references the rule that produced it,
`ValidateAlertEvent` requires a rule id, and the screens over it filter and label by rule. A
digital input has no rule and never will, so the write was refused, the notification arrived,
the log stayed empty, and the only symptom was a line in a log nobody reads — half of what
the function claimed to do simply did not happen.

The feed is also where the comparable events already live: the tamper monitor and both health
monitors publish notifications and write no alert rows, for exactly this reason. It is
filterable by camera and by category, so *"what happened on this camera at 02:14"* is still
one question with one answer.

**Known follow-up:** an input cannot yet be bookmarked into a case file, because case evidence
points at alert events. That needs a case item to be able to reference a notification, which
is a change to W3-3a rather than to this file.

## Rules the tests pin

| Rule | Why |
|------|-----|
| A door contact is `device.alert`, not `vision.alert` | It is a sensor reading, not a detection. Filing it as a detection makes "tell me when a door opens, but not about every person the AI sees" unexpressible for anyone routing notifications. mymatasan's `knownCategories` now registers the category — without that, `normalizeCategories` **drops** it, and a destination whose whole subscription is dropped falls back to *all categories*. |
| A return to normal is `Info`, never a warning | Otherwise a door closing wakes somebody as loudly as a door opening, and the feature is muted within a week. |
| The camera's own motion and analytics are **opt-in, off by default** | This appliance already runs its own detection over the same picture, with rules, zones, schedules and cooldowns the camera knows nothing about. A second unfiltered stream of "something moved" that no rule governs buries the first. Inputs and relays have no such overlap, so they are always on when the listener is. |
| Only cameras that **advertise** the event service are dialled | Asking one that does not is a guaranteed failure every backoff, forever — a fault that is a fact about the model. |
| Reaching `MaxCameras` is **reported** | A listener that quietly covers the first thirty-two cameras is worse than one that is off, because the screen says it is running and the doors it is not watching look like doors nobody has opened. |
| The listener is **off by default** | It opens a long-lived connection per camera. A feature that costs a socket per camera should be asked for. |
| `Unsubscribe` on the way out | A device has a small fixed number of subscription slots; an appliance that restarts a few times without releasing them runs out and can no longer subscribe at all. |

Metrics: `mymatasan_camera_events_total{kind}` and `mymatasan_camera_event_subscriptions`.

Settings live under the `onvifEvents` runtime-setting key, read live on every reconcile —
**a runtime setting rather than a per-camera column because this app's auto-migrator creates
tables from entities but does not ALTER existing ones**, so a new column on `camera_onvif`
would exist on fresh installs and be missing on every appliance already in the field.
