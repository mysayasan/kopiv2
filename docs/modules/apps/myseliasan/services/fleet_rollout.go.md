# Module: apps/myseliasan/services/fleet_rollout.go

## Purpose

`FleetRolloutService` — staged version rollout (flagship hardening plan W2-5, F-07): moves the
fleet's `mymatasan` nodes to a specific version a RING at a time, canary first, gated on each
ring genuinely proving itself before the next one starts. See `entities/fleet_rollout.go.md`
for the data model and `apis/fleet_rollout_api.go.md` for the HTTP surface.

## What "proves itself" means, and why it is not just "the command returned 200"

The node accepts the update, downloads it, swaps its binary and restarts, so a successful reply
means only that it agreed to try. A ring passes when every node in it has:

1. come back on the control channel, and
2. REPORTED the target version itself (never assumed to have it — read from
   `ManagedNode.Version`, which is populated only by what the node's own control-channel hello
   said, see `services/node_registry.go.md`'s `RecordVersion`), and
3. held that state for the settle window (`FleetRollout.SettleSeconds`).

Each catches a different failure: (1) a node that never comes back, (2) a node that came back on
the OLD version because the swap silently failed, and (3) a node that boots, looks healthy, and
dies a minute later. Checking only (1) is the version of this feature that reports success while
the fleet burns.

## Halt, not rollback

A deliberate departure from a naive design. Rolling a binary back is easy; rolling a database
back is not — migrations in this suite are forward-only (`infra/db/bootstrap.Migration` has no
down step). An automatic rollback would hand a node that has already migrated its schema a
binary that has never seen it, on every node in the ring at once. A failed ring instead STOPS
the rollout with a `HaltReason`, leaves the rest of the fleet untouched on its current version,
and waits for a human. The fleet is recovered by rolling FORWARD to a fixed build, which this
same machinery does.

## Responsibilities

- `IFleetRolloutService` — `Plan(req, by)`, `Start(id)`, `Cancel(id, reason)`, `Get(id)`,
  `List()`, `Advance(ctx)` (drives the active rollout one step; safe to call on a timer).
- `NewFleetRolloutService(db, nodes, sender, presence, audit, logf)` — `nodes` is the narrow
  `rolloutNodeLister` slice of `INodeRegistry` (just `List`); `sender` is a `ControlSender`;
  `presence func(nodeID) bool` reports whether a node currently holds a live control channel
  (wired to `ControlServer.IsConnected` in `app.go`); `audit`/`logf` are optional.
- **`Plan(ctx, req, by)`** — works out the rings and records the rollout in `draft`.
  - Refuses a second plan/run while one is already `draft` or `running` (`activeRollout`) — two
    rollouts driving the same fleet would fight over which version each node should be on.
  - Candidates are every camera-kind node (optionally filtered to one `SiteId`), sorted
    DETERMINISTICALLY by `NodeId` — a rollout an operator can predict is one they can schedule
    around, and re-planning the same fleet twice must not produce different canaries.
  - **`canSelfUpdate(ctx, nodeID)` probes each reachable node at PLAN time** (`GET
    /api/system/update`, reading `canSelfUpdate`/`managed`) and routes a node that cannot
    replace its own binary — a container image or package-managed install — to
    `RolloutNodeUnsupported` (ring `0`, excluded from every real ring) with the real remedy
    ("deploy a new image" / "upgrade through the package manager") in `Detail`, rather than
    letting it land in a ring and halt the whole rollout on its first turn. An unreachable node
    is treated as CAPABLE — excluding every node that merely happens to be offline while
    planning would quietly shrink the rollout and still report full coverage; an unreachable
    node that turns out to be un-updatable fails its own ring later, loudly, on its own.
  - Persists the `FleetRollout` row plus one `RolloutNode` per candidate (ring-assigned,
    `pending`) and one per unsupported node (ring `0`, `unsupported`) — the unsupported rows
    exist so the plan SAYS which appliances it is leaving behind and why, rather than silently
    covering nine nodes out of twelve and reporting complete success.
  - Audits `fleet.rollout.plan`.
- **`Start(ctx, id)`** — moves a `draft` rollout to `running` (`CurrentRing = 1`), audits
  `fleet.rollout.start`, and calls `Advance` once immediately so the first ring does not wait
  for the next timer tick.
- **`Cancel(ctx, id, reason)`** — stops a rollout (any state except already-terminal). Nodes
  already updated STAY updated — there is no undo, and pretending otherwise is the dangerous
  half of this feature.
- **`Advance(ctx)`** — the driver, called on a timer (`app.go`'s 30s leader-gated `leaderTicker`)
  and once synchronously after `Start`. Serialized by an internal `driving sync.Mutex` — the
  timer and an operator action can both call it, and two drivers working one rollout would ask
  the same node to update twice. Loops `advanceOnce` up to `maxRolloutStepsPerPass` (64) times
  per call so a ring that passes lets the next one start in the SAME pass rather than costing one
  full tick of dead time per ring boundary — on a fifty-node estate that is most of the rollout's
  wall clock otherwise spent doing nothing.
  - `advanceOnce` — one pass over the CURRENT ring:
    - `pending` nodes are asked (`askNode`): `POST /api/system/update/apply` with
      `{"version": TargetVersion}` over the tunnel (`Role: "admin"`, `Actor: "fleet-rollout"`). A
      node already on the target is marked `skipped` (not `succeeded` — this rollout did not
      move it). A node not currently connected is left `pending` (retried next pass) unless its
      own `NodeTimeoutSeconds` has elapsed, in which case it is failed. A non-2xx or transport
      error fails it immediately with the node's own error text folded in
      (`nodeErrorLine` — extracts `message` from the node's JSON error envelope, truncated,
      single-line, so a halt reason never carries a raw JSON blob).
    - `updating` nodes are judged (`judgeNode`) against `ManagedNode.Version`
      (`nodeVersions`, snapshotted once per pass): reporting the target version AND back on the
      control channel → `succeeded`; past `NodeTimeoutSeconds` with no match → `failed`, with a
      detail that distinguishes "never reported" from "came back running the OLD version" (the
      latter is the most informative failure there is — a swap that did not take, not a node
      that died).
    - **Any `failed` node in the ring halts the whole rollout** (`RolloutStateHalted`), reason
      naming the ring, the failure count, and the first node's own detail.
    - Once every node in the ring is `succeeded`/`skipped`, `RingReadyAt` is stamped (once) and
      the settle window (`SettleSeconds`) is measured from it. **During the settle window**, a
      node that drops off the control channel, or is found reporting a version other than the
      target, is failed LATE and halts the rollout — a node that dies a minute after boot is
      caught here, not reported as a success.
    - Past the settle window, `CurrentRing` advances (or the rollout completes if it was the
      last ring).
- `finish(ctx, rollout, state, reason)` — closes a rollout terminally, audits
  `fleet.rollout.<state>`.
- `nodeVersions(ctx)` — `NodeId → reported version` map, built fresh each `advanceOnce` pass from
  `INodeRegistry.List`; a node with no reported version is ABSENT from the map (not
  present-with-empty), so "unknown" is never mistaken for "old".

## Notes

- `RolloutPlanRequest` — `TargetVersion` (required), `RingSize`/`SettleSeconds`/
  `NodeTimeoutSeconds` (0 takes the shipped defaults: ring 1, settle 300s, node timeout 900s),
  `SiteId` (0 = whole fleet), `Note`.
- `RolloutView` wraps a `*entities.FleetRollout` with its `Nodes` and a `Counts` (state → count)
  map, so the UI's header line doesn't need the full node list.
- Every plan/start/cancel/finish is audited via `IAuditService` (`TargetType: "rollout"`).
- Wired in `apps/myseliasan/app/app.go`: `NewFleetRolloutService(deps.Db, registry, nodeSender,
  controlServer.IsConnected, auditService, logf)`, driven by a leader-gated 30s
  `leaderTicker` calling `Advance` — the same multi-instance-safety pattern as the fleet policy
  reconciler and every other scheduled singleton in that file, so N control-plane instances never
  race each other asking the same node to update twice.
- **Built, live-benched** — `tools/fleetbench/bench_w25_rollout.py` exercises the halt path
  fully real (a version that was never published, the node's own updater refuses it, the node's
  words come back in the halt reason) and the success path against a real binary swap and
  container restart. See `docs/FLAGSHIP_HARDENING_PLAN.md` (W2-5, F-07).
