# Module: apps/myseliasan/entities/fleet_policy.go

## Purpose

`FleetPolicy` + `FleetPolicyItem` — a statement of INTENT about how some part of the fleet's
node settings *ought* to be configured, and (separately) whether the control plane is
allowed to act on a disagreement. This is the data model behind fleet configuration policy
+ drift detection (flagship hardening plan W2-1, F-06): the control plane could already
change any node's settings (`apis/node_proxy.go` tunnels an authenticated request to the
node's own API), but it had no way to say what the settings *ought* to be, so a node that
was reimaged, restored from an old backup, or retuned by a site engineer at 2am simply
stopped matching its siblings with nothing anywhere noticing. A policy is compared against
reality on a schedule by `services/fleet_policy_reconciler.go.md`; the comparison — not the
push — is the product.

## Policy scopes

`PolicyScopeFleet` / `PolicyScopeSite` / `PolicyScopeNode`, most general first. A node's
effective configuration is resolved by applying every matching policy in scope order, so a
more specific scope overrides a more general one FIELD BY FIELD (see
`services.ResolveEffectivePolicy`) — "every camera node keeps 30 days of alerts" (fleet)
except "the airport site, which keeps 90 for the regulator" (site) except "this one node
whose disk is half the size" (node).

`PolicyScopeRank(scope)` orders scopes by specificity (node=3, site=2, fleet=1, unknown=0)
so a row written by a future version this build does not understand can never quietly
outrank one it does.

## `FleetPolicy` Fields

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `Name` / `Description` | Required name, free-text description. |
| `Enabled` | `false` parks a policy without deleting it: it stops being resolved, so it neither reports drift nor enforces. |
| `Scope` / `TargetId` | One of the `PolicyScope*` values; `TargetId` names what it applies to (a site id for site scope, a `ManagedNode.NodeId` for node scope, empty for fleet). |
| `NodeKind` | **Required**, normalized to `"camera"` when empty on save (same convention as `ManagedNode.Kind`). A policy without a kind would be permanently, unfixably "drifted" against every node of a kind that has no governed section — an alarm that cannot be cleared. |
| `Enforce` | Whether the control plane WRITES the desired value back to a disagreeing node, or merely reports the disagreement. **Default FALSE**, and stays false unless deliberately turned on — an enforcing policy silently reverts a local change an engineer made in front of the appliance, possibly the change that stopped it alarming at 3am, minutes later, from a screen nobody is looking at. Report-only is already the valuable half. |
| `LastEvaluatedAt` | Unix timestamp of the last reconcile pass that actually compared this policy against a reached node; deliberately NOT carried over on edit (`services/fleet_policy.go`'s `Save`) — an edited policy has not been compared against anything yet. |
| `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt` | Standard audit columns. |

## `FleetPolicyItem` Fields

One governed setting inside a policy — a section, a field within it, and the desired value.
A policy names FIELDS, not whole sections: most of a section is a per-appliance tuning knob,
not a fleet decision (a sweep interval), so a section-shaped policy would force an operator
to have an opinion about every knob to have one about a single threshold.

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `PolicyId` | Indexed FK (`idx:"policy"`) to the owning `FleetPolicy`. |
| `Section` | A catalog section id — see `services/policy_catalog.go.md` ("continuity", "health", "tamper", "machineHealth", "notificationRetention"). |
| `Field` | A dotted path within that section's JSON object (`minCoveragePercent`, `disk.criticalPercent`). Only paths the catalog declares are accepted — a policy cannot post arbitrary JSON at a node. |
| `Value` | The desired value as a JSON scalar literal (`95`, `true`, `"high"`), stored as text because the managed fields are int/float/bool and a column per type is mostly null. The catalog's type decodes it on the way in (`ParsePolicyValue`) — an unparseable value is rejected at save time, not discovered by the reconciler at midnight. |

## Notes

- Both entities are registered for DB bootstrap in `apps/myseliasan/app/app.go`'s `Entities()`.
- `FleetPolicy` and `FleetPolicyItem` are both included in the `.selbackup` `policies`
  section (`services/backup.go.md`) — without them a restored control plane reports every
  node "unmanaged", since the estate's configuration standard is exactly the kind of thing
  nobody has written down anywhere else.
