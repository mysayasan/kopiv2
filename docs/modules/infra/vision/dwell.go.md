# Module: infra/vision/dwell.go

## Purpose

The three detection rules that ask a question about TIME rather than about a frame (W3-4):

| Type | Fires when |
|------|------------|
| `loitering` | a tracked object stays inside the zone for `dwellSeconds` |
| `left_behind` | a tracked object stops moving for `stillSeconds`, with nobody beside it |
| `direction` | a tracked object travels `minTravel` across the zone on a wanted heading |

They are evaluators over the tracker that already exists (`track.go`), not a new pipeline:
the candidates and the ByteTrack ids are the same ones line crossing uses, and no extra
inference runs for any of them.

## The one idea the whole file is about

A frame rule can be wrong for one frame and right on the next, and the min-frames streak
absorbs it. **A time rule that is wrong for one frame loses its timer**, and a thirty-second
threshold then never fires at all on a real camera, where confidence dips and people walk
behind pillars. Almost every decision here follows from that:

- **Not being seen is not the same as being seen somewhere else.** A missed detection is
  missing information and is absorbed by the grace; being observed OUTSIDE the zone is
  information, and resets the dwell immediately. Conflating them either resets on every
  flicker or lets somebody who steps out and back accumulate a dwell they never had.
- **The grace is counted in MISSED SAMPLES, not seconds** (see `track.go`). What is being
  tolerated is a dropped detection, and detections only happen when the detector runs.
- **A track is only born inside the zone.** Starting one outside and letting it walk in
  would date the dwell from wherever the object first appeared on camera — for a doorway
  rule, that means the timer starts in the car park.
- **Prune after matching, using what this pass matched.** Ageing by wall clock let an
  expired track be matched back to life on the pass that should have retired it.

## The timestamp means when it STARTED

`dwellStartedAt` / `stillSince` / `since` are in the metadata of every alert these rules
raise. An alert that says only "loitering at 14:05" sends somebody to 14:05 in the footage,
and the person arrived at 14:04:30 — the thirty seconds the rule spent waiting are exactly
the interesting ones. Same trap W2-2 found in the availability numbers: a threshold between
the event and the notice is a bias, and here it is also a wrong timestamp on evidence.

## Per-object latch, not just a per-rule cooldown

`track.fired` stops ONE object alerting repeatedly; the rule's cooldown limits how often the
RULE speaks. They are different things: without the latch, a person who sits down for an hour
re-fires on every cooldown expiry. The cooldown is checked LAST so a suppressed alert does
not also consume the track's single chance.

## Left behind is about movement, not presence

An object is "still" while it stays within `driftTolerance` of an anchor; moving further
re-anchors it and restarts the timer. A bag being carried through the frame is not a bag left
behind, and a rule that fired on presence would alert on every passenger.

`requireUnattended` (default **on**) suppresses the alert while a person is within
`personRadius`. People are collected from the candidate list regardless of the rule's own
classes, because "person" is rarely one of the classes such a rule watches for.

## Direction is measured up the IMAGE

`bearingDegrees` negates y, because image y grows downward. Without that, "up" and "down" are
swapped and a wrong-way rule fires on exactly the traffic it should ignore. `minTravel` exists
because jitter has a random bearing: without a minimum distance a stationary object eventually
satisfies any heading by accident.

## A rule with no classes is refused

`ruleLabelAllowed` falls back to a static per-detection-type class map for legacy rules, and
these three types have no entry in it — so a rule saved with no classes would match nothing,
forever, silently. **A rule that cannot fire is worse than no rule: somebody believes an area
is being watched.** Likewise a `direction` rule with no heading, which has no wrong way.

## Related

- `infra/vision/track.go.md` — the shared tracker.
- `infra/vision/line_crossing.go.md` — the other track-based rule.
