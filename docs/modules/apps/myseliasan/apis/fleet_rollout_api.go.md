# Module: apps/myseliasan/apis/fleet_rollout_api.go

## Purpose

Registers the HTTP surface for staged version rollout (flagship hardening plan W2-5, F-07) —
moving the fleet to a specific `mymatasan` version a ring at a time. See
`apps/myseliasan/entities/fleet_rollout.go.md` for the data model and
`apps/myseliasan/services/fleet_rollout.go.md` for the ring/health-gate/halt engine.

## Endpoints

`NewFleetRolloutApi(router, auth, session, rollouts, audit)` mounts on `/fleet-rollouts`, behind
`auth.Middleware` + `session.Middleware`:

| Method | Path | Access | Notes |
|---|---|---|---|
| GET | `/api/fleet-rollouts` | Any authenticated session | Every rollout, newest first (`IFleetRolloutService.List`). |
| POST | `/api/fleet-rollouts` | **Superadmin only** | Plans one (`RolloutPlanRequest` body, 64 KiB cap, `DisallowUnknownFields`) — returns it in `draft`. |
| GET | `/api/fleet-rollouts/{id}` | Any authenticated session | One rollout with its per-node progress (`RolloutView`: the rollout row, `Nodes`, and a state→count `Counts` map). |
| POST | `/api/fleet-rollouts/{id}/start` | **Superadmin only** | Moves a `draft` rollout to `running` and drives the first ring immediately. |
| POST | `/api/fleet-rollouts/{id}/cancel` | **Superadmin only** | Stops a rollout (optional `{"reason": "..."}` body). Nodes already updated stay updated. |

## Why writes are superadmin-only

Reading is available to any role that can reach the fleet — which appliances are on which
version, and whether an upgrade is in trouble, is health information and hiding it helps nobody.
WRITING is a superadmin power, the same reasoning as a fleet policy (`apis/fleet_policy_api.go.md`)
or a correlation rule (`apis/fleet_rules_api.go.md`): starting a rollout replaces the software on
every recorder in the estate, and there is no undo (`requireSuper` checks
`session.IsSuperadmin(r)`, `controllers.ErrLimitedAccess` otherwise — a hard gate independent of
the general accessrbac permission matrix).

## Errors

`sendServiceError` maps `services.ErrRolloutNotFound` to `404` (so a stale link in a browser tab
reads as gone rather than the control plane being broken); every other service error is a `400`
with its message (e.g. "rollout %d is already %s" for a double-start, or the plan-time "none of
the N matching nodes can replace their own binary" refusal).

## Notes

- `list`/`get` are available to any authenticated session (not superadmin-gated) — the same
  "seeing is not the same power as authoring" pattern the fleet policy and fleet rules APIs
  document.
- Registered in `apps/myseliasan/app/app.go` alongside the service, immediately after
  `nodeSender` exists — the service drives the node's own self-update primitive over the same
  tunnel `apis/node_proxy.go.md` uses. A leader-gated 30s `leaderTicker` calls
  `IFleetRolloutService.Advance` in the background; this API only plans/starts/cancels/reads, it
  never drives a ring itself.
- Every write (`plan`/`start`/`cancel`, plus the terminal halt/complete transitions the driver
  itself reaches) is audited (`fleet.rollout.*`, `TargetType: "rollout"`) — see
  `services/fleet_rollout.go.md`.
