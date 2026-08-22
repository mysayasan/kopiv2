# Module: apps/myseliasan/entities/fleet_rollout.go

## Purpose

`FleetRollout` + `RolloutNode` — the data model behind staged version rollout (flagship
hardening plan W2-5, F-07): moving the fleet's `mymatasan` nodes to a specific version a RING
at a time (canary first) instead of pressing "update" on every appliance at once, which is how
an estate discovers a bad build all at the same time. Driven by
`services/fleet_rollout.go.md`'s `FleetRolloutService`; the HTTP surface is
`apis/fleet_rollout_api.go.md`.

The unit of a rollout is the ring, not the node: nodes are updated a few at a time, and each
ring must prove itself — every node in it back on the control channel, REPORTING the target
version, and holding that state through a settle window — before the next ring starts.

## `FleetRollout` Fields

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `TargetVersion` | The exact app version every node in this rollout is asked to install (`"1.128.0"`, stored without a leading `v`). Never "latest" — a ring is only meaningful if every node in it installs the same thing, and "latest" is a moving target that can change between the first ring and the last. |
| `NodeKind` | Restricts the rollout to one kind of appliance. Only camera nodes (`mymatasan`) have the self-update primitive this drives, so this is `"camera"` today; stored rather than assumed so the row still says what it meant when another kind gains a self-update primitive. |
| `State` | One of the `RolloutState*` constants (`draft`/`running`/`halted`/`completed`/`cancelled`) — see `services/fleet_rollout.go.md`. |
| `RingSize` | How many nodes are updated together. `1` is a true canary. |
| `CurrentRing` | The ring being worked (1-based; `0` before the rollout starts). |
| `RingCount` | How many rings the plan has, fixed at creation. |
| `SettleSeconds` | How long a ring's nodes must hold online, on the target version, before the next ring is allowed to start. An upgrade that crashes on the second startup, or wedges a minute after boot, looks exactly like a success without this. |
| `NodeTimeoutSeconds` | Bounds how long one node may take to come back on the target version before it is called failed. |
| `HaltReason` | Why a halted rollout stopped, in words an operator can act on — the field that makes a halt useful rather than merely safe. |
| `RingReadyAt` | Unix timestamp of when the current ring first had every node reporting the target version. The settle window is measured from here and persisted (not held in memory) so a control-plane restart mid-settle does not restart the clock, or skip it. |
| `Note` | Free-text operator note captured at plan time. |
| `CreatedBy`/`CreatedAt`/`StartedAt`/`FinishedAt`/`UpdatedAt` | Standard audit/lifecycle columns. |

## `RolloutNode` Fields

One node's place in a rollout, and what became of it.

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `RolloutId` | Indexed FK (`idx:"rollout"`) to the owning `FleetRollout`. |
| `NodeId` / `NodeName` | The fleet node id, plus a snapshotted name so a completed rollout still reads correctly after a node is renamed or released. |
| `SiteId` | The node's site at plan time, for grouping in the UI. |
| `Ring` | 1-based; ring 1 is the canary. **Ring `0` means "not in any ring"** — see `RolloutNodeUnsupported` below. |
| `State` | One of the `RolloutNode*` constants: `pending`, `updating` (accepted the command, applying), `succeeded` (reported the target version), `failed` (did not, in time, or refused the command), `unsupported` (probed at plan time as unable to self-update at all — a container image or package-managed install — and excluded from every ring rather than left in one to fail), `skipped` (already on the target version when its turn came — distinct from `succeeded` on purpose, since this rollout did not upgrade it). |
| `FromVersion` | What the node reported it was running when it was asked to update — captured at that moment, not at plan time, because a node can be upgraded by hand in between. |
| `Detail` | The failure/skip reason in words, when `State` is `failed`, `unsupported`, or `skipped`. |
| `AskedAt` / `FinishedAt` | Unix timestamps. |

## Notes

- Both entities are registered for DB bootstrap in `apps/myseliasan/app/app.go`'s `Entities()`.
- **Halt, not rollback, is deliberate.** Rolling a binary back is easy; rolling a database back
  is not, because migrations in this suite are forward-only (`infra/db/bootstrap.Migration` has
  no down step). An automatic rollback would hand a node that has already migrated its schema a
  binary that has never seen it. A failed ring stops the rollout with a `HaltReason` and leaves
  the rest of the fleet on the version it was already running; the fleet is recovered by rolling
  FORWARD to a fixed build, which the same machinery does.
