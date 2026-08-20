# Module: apps/myseliasan/services/policy_catalog.go

## Purpose

The whitelist of node settings a fleet policy (`entities/fleet_policy.go.md`) may govern —
the ONLY thing that decides what a policy may read, compare, and (when enforcing) write
back to a node. Without it, a policy is a mechanism for posting operator-supplied JSON at an
arbitrary path on every appliance in the estate, on a timer, from a screen; the node would
still authorize the request (the tunnel asserts a role and the node evaluates its own
matrix), but "an admin may change this setting by hand" and "the control plane may rewrite
this setting unattended on fifty machines" are not the same permission.

Two whole categories are deliberately absent from the catalog:

- **Hardware/runtime settings** (GPU device, thread counts, ffmpeg path) — properties of the
  box, not decisions about the fleet. A fleet-wide GPU index is wrong on every machine that
  does not have that GPU.
- **Notification ROUTING** (webhook URLs, bot tokens, chat ids) — pushing those from the
  control plane is credential distribution, which needs sealed transport and a rotation
  story, not a settings comparison. Alert **retention** (how long a node keeps its own alert
  history) carries no secret and is exactly the kind of estate-wide standard a policy should
  express, so that part is in the catalog.

## Catalog sections

`policySections` (returned via `PolicySections()`, filtered per node kind via
`PolicySectionsForKind(kind)`), each a `PolicySection{Id, Label, NodeKinds, GetPath, PutPath,
ReadAt, Fields}`:

| Section | Node kinds | Node path | Fields (~count) |
|---|---|---|---|
| `continuity` | camera | `GET/PUT /api/settings/continuity` | enabled, minCoveragePercent, failureThreshold, recoveryThreshold |
| `health` | camera | `GET/PUT /api/settings/health` | enabled, timeoutMs, failureThreshold, recoveryThreshold |
| `tamper` | camera | `GET/PUT /api/settings/tamper` | enabled, failureThreshold |
| `machineHealth` | camera | `GET/PUT /api/settings/machine-health` | enabled, cpu/memory/disk warn+critical percent, mitigation.enabled |
| `notificationRetention` | camera | `GET /api/settings/notification` (read), `PUT /api/settings/notification/retention` (write), `ReadAt: "retention"` | days, onlyRead, intervalHours |

All five sections currently apply only to `"camera"` (mymatasan) nodes — ~21 fields total.
`notificationRetention`'s read/write paths are asymmetric on purpose: reading the whole
notification object and writing it back would round-trip the webhook URL and Telegram bot
token through the control plane on every reconcile, exactly the credential handling the
missing "routing" category is built to avoid; `ReadAt` descends into the `retention`
sub-object of the GET response so only that sub-object is ever compared or sent.

Each `PolicyField{Key, Kind, Min, Max, Unit, Label}` declares its own type
(`PolicyFieldBool`/`PolicyFieldInt`/`PolicyFieldFloat` — no string fields; none of the
managed settings needs one) and bounds. The bounds are the CONTROL PLANE's, not the node's:
the node normalizes/clamps whatever it is given (it must survive a hand-edited database),
but a policy whose desired value the node would silently adjust is a policy that reports
drift forever and can never be satisfied — so out-of-bounds values are rejected at save time
instead (`ParsePolicyValue` → `checkBounds`).

## Responsibilities

- `LookupPolicySection(id)` / `PolicySection.Field(key)` / `PolicySection.AppliesTo(kind)` —
  catalog lookups used by both `services/fleet_policy.go.md` (validation, resolution) and
  `services/fleet_policy_reconciler.go.md` (read/compare/write).
- `PolicyNodeKinds()` — the node kinds any catalog section covers, in display order; the UI
  offers only these when creating a policy, since a kind with no governable section would
  produce a policy that can never contain an item.
- `NormalizePolicyNodeKind(kind)` — blank maps to `"camera"`, matching `ManagedNode.Kind`'s
  own convention (every node adopted before kinds existed is a mymatasan recorder).
- `ParsePolicyValue(field, raw)` — decodes a stored value literal against the field's
  declared type and bounds. Strict about type (an int field rejects `30.5`) but tolerant
  about spelling (`30` and `"30"` both work, since a `<select>` element posts strings).
- `policyGetPath`/`policySetPath` — dotted-path get/set into a decoded `map[string]any`.
  `policySetPath` refuses to overwrite a non-object with an object, so it can never silently
  discard something the node had there.
- `policyValuesEqual(want, got)` — compares a typed desired value against a value decoded
  from JSON (where every number is `float64` and nothing carries its original Go type), so a
  naive `==`/`reflect.DeepEqual` would report drift on a node that agrees exactly (int64(95)
  vs float64(95)). Numbers compare numerically and exactly — no epsilon, since these are
  operator-typed thresholds, not measurements. Bools never equal a number. This is called out
  in the source as the single most load-bearing comparison in the reconciler: wrong in the
  lax direction and drift is never reported; wrong in the strict direction and every
  compliant node is flagged forever and the feature gets turned off.
- `formatPolicyValue(v)` — renders a value for the UI and the audit trail.

## Notes

- The catalog is served to the SPA via `GET /api/fleet-policies/catalog`
  (`apis/fleet_policy_api.go.md`) rather than hardcoded client-side, so the create/edit
  screen can never offer a field the server would refuse.
