# Module: apps/myseliasan/services/failover.go

## Purpose

The control-plane half of N+1 node failover (W3-7). Owns `FailoverPlan`
(`entities/failover_plan.go.md`), the three-step camera handover, the drill, the takeover and
the fail-back, plus the unattended sweep that keeps plans copied, tested and acted on. The
appliance half is `apps/mymatasan/services/standby.go.md`.

## The control plane carries an envelope it cannot open

Moving a camera set means moving camera credentials. The obvious implementation — a "list my
cameras with passwords" endpoint relayed through here — would turn this service into a
fleet-wide credential vault and that endpoint into a bulk dump readable by anything that can
call it.

Instead: the **spare** mints a one-exchange key, the **protected** appliance seals its set to
that key and binds it to the spare's node id, and this service relays the result. It never
holds a camera password, never stores one, and could not decrypt a bundle if it wanted to; a
bundle intercepted here cannot be staged onto any other appliance. See `infra/handoff`.

## What it deliberately will not do

- **Never tells a returning appliance to stand down.** A control plane that cannot reach a
  node cannot tell "dead" from "on the far side of a partition and recording perfectly".
  Fencing means being willing to stop the only thing recording on evidence that is
  definitionally incomplete. Failover here is **additive**: at worst two appliances record
  the same camera until an operator fails back. Nothing recording is the one unrecoverable
  outcome, and no path here can produce it.
- **Never fails back automatically.** A recorder that returns for thirty seconds and dies
  again would thrash every camera in the building between two appliances. The return is a
  notification and a banner; the handover back is a decision.
- **Does not recover the footage** that was on the failed appliance. Only the clips the
  critical-clip archive (W2-3) already pulled off it exist anywhere else. The screen says so
  in those words.

## `IFailoverService`

`List` / `Get(id)` / `Save(req, actor)` / `Delete(id, actor)` /
`Stage(id, actor)` / `Drill(id, actor)` / `Activate(id, actor, automatic)` /
`Release(id, actor)` / `Sweep(ctx)`.

`NewFailoverService(db, nodes, sender, audit, notify, logf)` for the app;
`newFailoverServiceWith(repo, …)` is the injectable constructor the tests use, so the
behaviour that decides whether a building keeps recording is exercised without a database
between the assertion and the thing asserted.

`FailoverNodeSource` is narrowed to `List` alone, the same way the policy reconciler narrows
it: `INodeRegistry` is the adoption, enrollment and **revocation** surface, and a component
that can take a building's cameras over should not be one refactor away from releasing a node.

## `Save` refuses, at save time, what would otherwise be found during an outage

Self-standby; either end not in the fleet; either end not a camera recorder (a door
controller has no cameras to hand over and would show as permanently unready — an alarm that
cannot be cleared, which is how operators learn to ignore the ones that matter); a second
plan for an already-protected recorder; and **chains** in either direction. A hold-down below
`failoverMinHoldDown` is refused with the reason, because a shorter wait would be a promise
the system cannot keep.

Editing keeps everything the plan **learned** — staging, drill, activation timestamps —
**unless the pairing itself changed**, in which case what the old spare holds says nothing
about the new one and carrying the drill result over would be a green tick earned by a
different machine. Repointing a plan that is currently carrying cameras is refused.

`Delete` refuses while active and tells the spare to `forget` the staged copy; otherwise one
appliance keeps another site's camera credentials for a plan that no longer exists, and
nothing anywhere says why.

## `Stage` — the three steps

1. `GET /api/standby/handoff-key` on the **spare**.
2. Assert the responding node id **is** the spare this plan names. A tunnel that delivered
   elsewhere would otherwise seal a site's camera credentials to a machine nobody chose,
   silently, because every later step would still succeed.
3. `POST /api/standby/handoff` on the **protected** recorder with `{recipientNodeId,
   publicKey}` → an opaque sealed blob.
4. `POST /api/standby/stage` on the spare with that blob, unchanged.

Records `LastStagedAt`, clears `LastStageError`, stores `CameraCount`. Staging never advances
a plan out of `active` (re-copying the list does not change that the spare is carrying the
cameras) and **never claims readiness**, which only a drill can establish.

## `Drill`

`POST /api/standby/{protected}/drill` on the spare; mirrors its `readiness`/`reachable`/
`total`. A drill that ran and found the spare cannot open the cameras is **not an error** —
it is the drill working — but it is audited with an error outcome anyway, because what an
investigator wants later is "when did we last know this would work", and a success row next
to 3-of-40 answers that wrongly. It also raises a notification.

## `Activate` / `Release`

`Activate` refuses when nothing was ever staged, with the honest reason: the failure an
operator must never meet is pressing this in an emergency and being told afterwards that
nothing was copied. Records `automatic`, counts how many cameras the spare reported as
actually `recording`, audits both, notifies. `Release` refuses when the spare is not carrying
the set, and its audit line says in words that the footage recorded during the outage stays
on the spare.

## `Sweep`

Leader-gated by the caller (two instances would stage the same plan twice and, worse, could
both decide to take the same recorder over). Serialised internally by `sweeping` for the same
reason the policy reconciler serialises its passes.

Per enabled plan:

- **active** → the only thing worth watching is the protected appliance coming back, at which
  point both may be recording and somebody must be told. Edge-triggered, and nothing is
  changed.
- **protected lost** → `considerTakeover`.
- **both online** → clear the "about to fail over" edge, re-stage if the last copy is older
  than `failoverStageInterval` (1h), re-drill when `drillIsDue`. The drill interval matters:
  a plan proved six months ago has not been proved, and the failures that develop while
  nothing is happening (a VLAN change, a camera password rotated on the camera, a spare moved
  to a different switch) are exactly what an unattended drill catches.

- **anything else** (self-dropped, unknown, spare away) → nothing to copy and nothing to
  prove.

`drillIsDue` exists because **"never drilled" is not "overdue"**, and one subtraction cannot
tell them apart. `now - LastDrillAt` on a plan that has never been drilled is fifty-five
years, so the sweep drilled every new plan on its first tick — and an operator watched the
badge they had just seen say "never tested" turn green by itself half a minute later, beside
a sentence telling them to press Test. The product and its own screen disagreed, and the
distinction between COPIED and PROVED was invisible in ordinary use. A plan that has never
been drilled now waits `failoverFirstDrillDelay` (15 min) from its last staging; one that has
been drilled goes by the 24h interval. (Found by the screen pass.)

`considerTakeover` measures the hold-down from `LastSeenAt` — the last time the fleet *heard*
from the appliance — not from when this sweep noticed. A sweep that has just started up has
noticed everything at once and would otherwise treat a restart of the control plane as every
site failing simultaneously. A plan with nothing staged raises `activate-failed` rather than
doing nothing silently: that is the case where the promise cannot be kept, and it must reach
a human.

## `view` — the one number the screen leads with

`Ready` is true **only** when a drill proved every staged camera reachable from the spare —
never on the strength of a successful copy. Copying proves the two appliances can talk to
each other; it says nothing about whether the spare can reach the **cameras**, which is a
different network path with different credentials and the thing that actually fails.

`ReadyState` is a machine-readable token (`disabled`, `active`, `standby-down`, `not-staged`,
`untested`, `ready`, `blind`, `partial`), rendered into a sentence by the SPA. **A sentence
composed here would arrive in English on an Arabic screen** — the exact defect W3-4 and W3-6
each shipped once and each had found by the screen pass. `standby-down` outranks the drill
result, because a spare off the network makes every other reading stale.

**Every action returns the appliance's own per-camera report** (`viewWith`), not just the
plan. A takeover's per-camera outcome exists for one moment — the appliance computes it while
taking over and does not store it, because it is a result rather than a state — so rebuilding
the view from the database afterwards dropped it, and an operator who had just pressed the
button in an emergency was told "active" and nothing about which of their cameras were
actually recording. Every status code on that path was 200 and the audit trail even had the
outcomes in it. **Found by the live bench; the same shape as the redact flag W3-6 dropped
between the screen and the service — a field that existed at both ends and never crossed the
middle.**

`Get(id)` additionally reads the per-camera detail **live** from the spare
(`GET /api/standby`), best effort. It is not mirrored into a column for the same reason PTZ
presets are not mirrored: the spare is the only thing that knows what it is holding, and two
answers part company the first time one is written by anything else. It is not done in
`List` because that would make the screen as slow as the slowest appliance.

## Transport

`send` makes one tunneled call and unwraps the node's `{message,result}` envelope. It asserts
`Role: "admin"`, `Actor: "failover"` — a role **name** the node resolves against its own
roles and matrix, exactly as the policy reconciler does. 404 is reported as "this appliance
does not support failover; it is running an older build"; 429 as a rate-limit that will
retry; any other non-2xx carries the node's own message (`standbyErrorText`), because the
reason the appliance gave is usually the whole answer and discarding it wastes the one moment
somebody needs it.

`standbySetPayload` mirrors mymatasan's `StandbySet` rather than importing it: the control
plane must not depend on an appliance app's packages, and the two are joined by a wire
format, not by a type.

## Notifications

`FailoverNotifier` is a **function**, injected from `app/app.go` (`failoverNotifier`), so the
one place that decides what a fleet event sounds like sits next to every other fleet
notification. Kinds: `ready-to-activate`, `activated`, `activate-failed`, `protected-back`,
`released`, `drill-failed`.

## Auditing

`ActionFailoverPlanSave` / `PlanDelete` / `Stage` / `Drill` / `Activate` / `Release`, target
type `failover`, target id the protected node. Recorded through the audit **service**, not a
request-scoped auditor, because the automatic takeover has no HTTP request behind it — and
that is the one entry that would otherwise leave no trace but a log line.

## Tests

`failover_test.go` fakes the repo, the node source and the tunnel. Covers: every refusal in
`Save`; chain refusal in both directions; the three-step order and the sealed blob reaching
the spare unchanged; the unexpected-respondent refusal; **staged is not ready**; the three
drill verdicts mapping to distinct states; `standby-down` outranking a passed drill; activate
refused with nothing staged; the hold-down being waited out and the alarm being
edge-triggered; automatic takeover when armed; the unkeepable promise being reported;
the returning appliance notifying but changing nothing; delete refused while active; delete
telling the spare to forget; repointing clearing the drill result; and a disabled plan being
completely inert.

It also carries the two defects the live run found as regression tests: the per-camera
outcome reaching the caller of `Activate` and of `Drill`, and a brand-new plan not being
drilled the moment it is created.

Mutation-checked: treating a successful copy as ready, ignoring the hold-down, dropping the
respondent check, dropping the per-camera report, and collapsing never-drilled into overdue
— each makes the matching test fail with a message that names the defect.
