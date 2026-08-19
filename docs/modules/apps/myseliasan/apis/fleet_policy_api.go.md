# Module: apps/myseliasan/apis/fleet_policy_api.go

## Purpose

Registers the HTTP surface for fleet configuration policy — what the estate's node settings
ought to be, and how far each node currently is from it. See
`apps/myseliasan/entities/fleet_policy.go.md` for the data model,
`apps/myseliasan/services/policy_catalog.go.md` for what can be governed, and
`apps/myseliasan/services/fleet_policy_reconciler.go.md` for the comparison/enforcement
engine.

## Endpoints

`NewFleetPolicyApi(router, auth, session, policies, reconciler, audit)` mounts on
`/fleet-policies`, behind `auth.Middleware` + `session.Middleware`:

| Method | Path | Access | Notes |
|---|---|---|---|
| GET | `/api/fleet-policies` | Any authenticated session | Lists every policy with its items (`IFleetPolicyService.List`). |
| GET | `/api/fleet-policies/catalog` | Any authenticated session | The governable-settings catalog (`services.PolicySections()`, `services.PolicyNodeKinds()`) — served rather than hardcoded client-side so the UI can never offer a field the server would refuse. |
| GET | `/api/fleet-policies/compliance` | Any authenticated session | The LAST completed reconcile pass (`FleetPolicyReconciler.Last()`) — not a live sweep; see the reconciler doc for why. |
| POST | `/api/fleet-policies/compliance/refresh` | **Superadmin only** | Sweeps the whole fleet now and returns the fresh result (`ReconcileAll`). |
| POST | `/api/fleet-policies/compliance/{nodeId}` | **Superadmin only** | Sweeps one node now (`ReconcileNode`) — the "check now" button. |
| POST | `/api/fleet-policies` | **Superadmin only** | Create or update a policy (`SaveFleetPolicyRequest` body, 256 KiB cap, `DisallowUnknownFields`). Validation failures return `400` with the human-readable reason from `validateFleetPolicy`. |
| DELETE | `/api/fleet-policies/{id}` | **Superadmin only** | Deletes a policy and its items. |

## Why writes are superadmin-only

Reading the compliance report is available to any role that can reach the fleet — it is a
health view and hiding it helps nobody. WRITING a policy is a superadmin power, on the same
reasoning as a correlation rule (`apis/fleet_rules_api.go.md`): a policy is an estate-wide
control, and whoever can write one can write one that turns every monitor in the fleet off —
or, since a policy can also **enforce**, one that overwrites fifty machines' settings
unattended. `requireSuper` checks `session.IsSuperadmin(r)` and returns
`controllers.ErrLimitedAccess` otherwise, a hard gate independent of the general accessrbac
permission matrix; the two on-demand sweep endpoints (`compliance/refresh`,
`compliance/{nodeId}`) are gated the same way even though they are reads, since a sweep can
itself trigger an enforcing policy's write.

## Audit trail

`save` records `ActionPolicySave` and `remove` records `ActionPolicyDelete`
(`services/audit.go.md`) on every attempt, success or failure — `save`'s metadata includes
`enforce`, since that flag is the difference between a policy that only reports and one that
reaches out and changes fifty machines. Enforcement itself is audited separately, at the
point it happens, by the reconciler (`ActionPolicyEnforce`, `services/fleet_policy_reconciler.go.md`).

## Notes

- `list` and `catalog` and `compliance` are available to any authenticated session (not
  superadmin-gated), the same "seeing is not the same power as authoring" pattern
  `apis/fleet_rules_api.go.md` documents for correlation rules.
- Registered in `apps/myseliasan/app/app.go` right after `nodeSender` exists, because the
  reconciler needs it to read/write node settings through the same tunnel the operator's own
  node screens use.
