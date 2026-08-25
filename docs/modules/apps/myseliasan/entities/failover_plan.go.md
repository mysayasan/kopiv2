# Module: apps/myseliasan/entities/failover_plan.go

## Purpose

`FailoverPlan` — which spare appliance covers which recorder if that recorder stops (W3-7).
Table `failover_plan`, created by the auto-migrator.

The control plane is the only thing that can arrange this, and that is not an architectural
accident: it is the only party that talks to both appliances, knows which are alive, and is
still running when one of them is not. `apis/deployment.go` is right that a mymatasan node
cannot cluster — it is pinned to its own disks and its own capture hardware. Failover is
therefore not clustering. It is a **rehearsed handover**, arranged in advance by the one
component that outlives the failure.

## One plan per protected appliance

Enforced by `ukey:"failover_protected"` on `ProtectedNodeId`. Two plans naming the same
recorder would mean two spares racing to take it over on the same outage — three copies of
every camera stream and two sets of footage nobody can reconcile afterwards. A spare may
cover **several** recorders (that is the "+1" in N+1), so the uniqueness is on the protected
side only. `services/failover.go` additionally refuses chains (a spare that is itself
protected, a protected appliance that is somebody's spare).

## Fields worth the comment they carry

- `AutoActivate` — **default false**, same reasoning as the fleet policy's `Enforce` but with
  more at stake. An automatic takeover is correct exactly when the recorder is really dead
  and wrong when the control plane merely cannot see it: a site behind a flapping link would
  have its cameras taken over, handed back and taken over again, each cycle starting a second
  stream on every camera. Report-and-wait is already most of the value — the operator learns
  within a minute, from a screen and a notification, and presses one button.
- `HoldDownSeconds` — how long the recorder must have been out of contact, **counted from
  last contact** rather than from the moment it was declared lost. That subsumes the liveness
  grace window (three heartbeats, floor 90s) instead of stacking on it: one clock, and the
  number an operator types is the seconds of silence they are willing to accept. It must
  therefore exceed that window to mean anything, which the service enforces
  (`failoverMinHoldDown` = 120; default 300).
- `LastStageError` — **stored, not just logged**. A plan that has been failing to stage for
  three weeks is invisible everywhere else: the screen would show a plan, the fleet would
  show two healthy nodes, and the spare would be holding a camera list from before the site
  was extended.
- `DrillReadiness` / `DrillReachable` / `DrillTotal` — mirrored from the **spare's own**
  answer. Not computed here: the spare is the only thing that actually tried to open the
  cameras.
- `ActivatedAutomatically` — distinguishes a takeover nobody chose from one somebody did. It
  is the first thing asked about an unexpected handover.
- `NotifiedLostAt` / `NotifiedBackAt` — make the two operator notifications **edge-triggered**.
  The sweep runs every thirty seconds; without these a recorder down over a weekend produces
  a notification every thirty seconds, and the feed that was supposed to carry the alarm is
  the reason nobody sees it.

## States

`FailoverStatePending` → `FailoverStateStaged` → `FailoverStateActive` →
`FailoverStateReleased`. They describe what the **spare** is doing, not what the protected
node is: node liveness has its own vocabulary and duplicating it here would give the fleet
two answers to "is this recorder up". `pending` is deliberately distinct from `staged` — a
plan that has never staged protects nothing.
