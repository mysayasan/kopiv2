# Module: apps/mymatasan/services/ptz_journal.go

## Purpose

`PTZJournal`: the one place that knows a camera's view changed **because we changed it**
(W3-5).

## Why it exists

Two features that never referred to each other are physically the same event.

The tamper monitor's `MOVED` verdict is "this camera is no longer showing what it used to
show" — which is the literal, intended, successful outcome of every preset recall, every tour
step and every jog of the PTZ ring. Without this journal, turning on a guard tour makes the
appliance alert that somebody has re-aimed the camera, every few minutes, forever; and the
operator's fix is to switch tamper detection off, after which it protects nothing. The same
is true, once, for every manual move.

It is also the arbiter between the operator and the automation. A tour that steps while
somebody is driving the camera takes it away mid-look; an alarm recall a tour immediately
overrides shows the alarm scene for three seconds. Both are settled by one question — *does a
person currently have this camera?* — and this is where the answer lives.

## Shape

One object, constructed once in `app.go` and handed to everything that moves a camera or
judges its picture:

| Holder | Uses it for |
|--------|-------------|
| `cameraService` | records jogs (`ClaimManual`) and preset/home moves (`NoteCommandedMove`) |
| `ptzService` | tour steps and recalls; reads `ManualHeld` to defer to a person |
| `CameraTamperMonitor` | reads `Motion` to forget a baseline it can no longer trust |

A shared object rather than a call between services, because the alternative is a dependency
cycle — the tamper monitor already depends on the camera service — and because a fact
recorded in one place cannot be recorded in only *some* of the paths that cause it.

Every method is **nil-safe**. An appliance with no PTZ wiring passes nil, every reader sees
"never moved, not touring", and behaves exactly as it did before this file existed.

## Rules

| Rule | Why |
|------|-----|
| `ClaimManual` never **shortens** an existing claim | Two operators on the same camera, or a long hold followed by a short one, must not hand the camera back early. |
| The hold is refreshed on every manual command, not set once | An operator working a camera for two minutes keeps it for two minutes; the tour resumes a fixed interval after they *stop*, not after they start. |
| `touring` is a distinct fact from `lastCommandedAt` | A tour means the view is *supposed* to keep changing indefinitely, which no settling period covers. See the tamper interlock. |
| `NoteCommandedMove` is recorded only on **success** | A refused move did not change the view; telling the tamper monitor to forget a baseline for a move that never happened blinds it for half a window for nothing. |
| `Forget` on camera delete | Otherwise the map grows for the life of the process, and a deleted camera stays marked as patrolling — which blinds the tamper monitor on an id that no longer exists. |

## The tamper interlock, in one paragraph

On a commanded move the monitor **forgets** that camera's rolling baselines rather than
suppressing the verdict for a while. Suppression alone defers the alert, it does not prevent
it: the old reference survives the quiet period and the first sample after it is still a long
way from a view that changed a minute ago. Both windows go, not just the histograms — a
camera re-aimed at a plain wall has legitimately lost its edge energy too, and leaving the
edge baseline in place turns every move onto a blank surface into a `COVERED` alert.

While a camera is **touring**, neither scene verdict is judged at all. The cost is stated
rather than hidden, in the code, in this document and on the tour screen: a patrolling camera
cannot be reported as re-aimed or covered, because a camera that is supposed to keep changing
what it looks at has no normal view to be measured against. `FROZEN` still works — it asks
whether the picture is changing at all, which is a question about the **stream** rather than
the scene — so a patrolling camera whose feed dies is still caught.
