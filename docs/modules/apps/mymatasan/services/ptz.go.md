# Module: apps/mymatasan/services/ptz.go

## Purpose

`ptzService` / `IPTZService`: PTZ **guard tours** and **alarm recall** (W3-5).

A PTZ camera that only jogs is a camera that needs somebody watching it. The two things that
make it an unattended device are here: it **patrols** a route on its own, and it **goes
somewhere** when something happens. Both are expressed in the same currency — a named
position stored on the camera — which is why they share a file and a runner.

The positions themselves are not here. They live on the **device**, as ONVIF presets,
addressed by a token the camera issues; this file stores only itineraries of tokens it does
not own. See `infra/onvif/ptz.go` and `entities/ptz_tour.go`.

## The three claims on one camera, in order

A camera can be wanted by a person at the PTZ ring, by an alarm, and by its own patrol, and
they will collide. The order is fixed:

1. **A person wins.** Somebody driving a camera is tracking something the appliance cannot
   see, and taking the camera off them loses it. An alarm arriving while they hold it does
   **not** move the camera — it is reported, and they decide.
2. **An alarm beats the patrol.** A patrol is a way of looking at nothing in particular; an
   alarm is something in particular.
3. **The patrol runs** when nobody else wants the camera.

Getting this wrong is not cosmetic. A patrol that steps during an alarm rotates the camera
away from the incident three seconds after pointing it there, and the recording shows the
empty corridor next door.

The arbiter is `PTZJournal` (`ptz_journal.go`), which is also what the tamper monitor reads.

## Rules the tests pin

| Rule | Why |
|------|-----|
| A tour needs **at least two** stops | One stop is a preset recall wearing a patrol's clothes; a tour of none is a row that runs forever and visits nowhere. Refused at save time, where somebody is reading the answer. |
| Every stop must name a preset the **device** has | The device is the only authority on where a camera can point. Checked against it, and **skipped when the camera cannot be reached** — a tour stays editable while its camera is down, which is when somebody is most likely fixing it. |
| Dwell 5–3600s, refused not clamped | Below the minimum a dome is still slewing when told to leave; above it a "patrol" is a fixed camera. |
| Tour names unique per camera, case-insensitively | Two rows nobody can tell apart in the list that chooses between them. |
| `IsRunning` is a **persisted column** | An appliance that reboots at 03:00 must come back doing what it was doing; nobody is awake to restart it. |
| Editing a running tour keeps it running, but restarts the route from stop 0 | An operator adjusting a dwell must not silently stop the patrol; the route has changed, so an index into the old list means nothing. |
| A step that fails waits a **dwell** and retries the **same** stop | A rebooting dome would otherwise be hammered every two seconds and the log filled with one line — and a skipped stop is a place nobody is watching. |
| A camera that cannot be asked is **not** a camera with no presets | `knownPresets` returns `nil` (unreachable) or a map (answered), and every caller checks which. Reading unreachable as empty would stop every patrol in the building the moment the network hiccupped. |
| A tour whose presets are gone **stops, persists that, and raises it once** | A patrol that has quietly stopped patrolling is a security failure: the screen still says "running" and nobody is told. |
| A stop, edit or delete invalidates a step **already in flight** | See below. |

## The last-moment check

A tick reads the tour rows, then asks the camera what presets it has — an ONVIF round trip —
and only then commands the move. A stop landing inside that gap is written to a row the tick
has already read, so the move goes out **after** the operator was told the patrol had
stopped. The live bench caught exactly this.

`ptzService.gen` counts how many times a tour has been stopped, edited or deleted.
`stepTour` captures it before the slow work and re-checks it, together with the operator's
claim, immediately before commanding the camera. Re-reading the row would cost another query
per tick; a counter bumped by every path that invalidates a step is exact and free.

A camera that swings away one beat after the screen said the patrol had stopped is worse than
one that never stopped, because the screen and the dome disagree about who has the camera —
and that is precisely when somebody is reaching for the ring.

## Deleting a camera takes its tours

`DeleteToursForCamera` is registered in the camera-deletion cascade. Without it the runner
keeps commanding a device that is no longer configured, every dwell, forever, and the tours
are listed under an id nothing can render. W3-2 shipped this exact shape with its appearance
descriptors, and it was found only when a bench finally deleted a camera.

## Recall rides in `ruleConfig`

`ParseRulePTZRecall` reads `ruleConfig.ptzRecall` (`{cameraId, preset, holdSeconds}`), beside
the destinations list and for the same reasons: it is routing for the rule's **outcome**, it
is edited with the rule, and it costs no migration on an appliance already in the field. A
`cameraId` of 0 means the rule's own camera, which is the common case.

`ApplyRulePTZRecall` is the single implementation, called from **both** paths that raise an
alert for a rule — the vision monitor and the manual create-alert API. The API path already
goes out of its way to give a hand-raised alert parity with the monitor (the recorder trigger
and the notification are both there); a recall missing from it would leave "what happens when
this rule fires" with two answers depending on which code raised the alert, and would make
the rule editor's own Test button prove nothing about the half of the rule that moves a
camera.

**Diagnostics are deliberately not a caller.** `emitDiagnostics` raises alert rows too, and a
dome that swings to the gate because the detector failed to capture a frame would be a camera
driven by the health of the software rather than by what it saw.

## Encoding

`Stops` is `"presetToken:dwellSeconds"` per stop, comma-separated; a stop with dwell 0 uses
the tour's `DwellSeconds`. A join table would need an order column and buy nothing — nothing
queries a stop, nothing joins on it, and the whole itinerary is read and written as one unit.
A token containing `:` or `,` is **refused** rather than encoded into something that decodes
differently: silent corruption of a patrol route is the failure this format could produce.
