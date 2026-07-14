# Module: apps/myseliasan/entities/fleet_rule.go

## Purpose

`FleetRule` + `FleetRuleClause` — the cross-domain correlation rule and its conditions. This is
the reason the suite has a fourth app: no single node can see across domains, but the control
plane, which already receives every node's events in one feed, can.

    motion on Camera 3 (a mymatasan node)
    AND a door contact opening (a myiotsan node)
    AND no badge swipe (a myiotsan node)
    -> intrusion

A single camera's motion alert at 03:00 is noise (a moth, a spider, headlights through a
window); a door contact opening at 03:00 is noise (cleaners, deliveries, wind); the two
together, with no badge swipe, is not noise. Evaluated by `apps/myseliasan/services/correlate.go`.

## `FleetRule` Fields

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `Name` | Required. What an operator reads at 03:00 — the alert title. |
| `Enabled` | Disabled rules are skipped entirely by `Correlator.Observe`. |
| `WindowSeconds` | How close in time the required clauses must all have fired to count as one incident — the difference between "a door opened and, separately, a camera saw motion last Tuesday" and one correlated event. |
| `GraceSeconds` | How long to WAIT, once the required clauses are satisfied, before deciding an "absent" clause really is absent. **The field that makes this a usable product rather than a nuisance** — a badge reader is routinely a second or two behind the door contact it just authorised; firing the instant the door opens would cry intrusion on every legitimate entry. Defaults to 5s when unset (`graceOf`). |
| `CooldownSeconds` | Suppresses re-firing. Survives restart via `LastTriggeredAt` — the identical alert-storm bug class `mymatasan` once shipped and `myiotsan` carries a regression test for. |
| `Severity` | `"critical"` (default) / `"warning"` / `"info"` — maps to `notification.Severity` via `severityOf`. |
| `LastTriggeredAt` | Unix timestamp, persisted on every fire so the cooldown holds across a restart. |
| `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt` | Standard audit columns. |

## `FleetRuleClause` Fields

One condition in a rule: an event that must be present, or one that must be absent.

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `RuleId` | Indexed FK (`idx:"rule"`) to the owning `FleetRule`. |
| `Mode` | `"required"` (this must happen) or `"absent"` (this must NOT have happened). The absent clause is what lets a rule express INNOCENCE as well as guilt — without it, "the door opened at 03:00" is an alert every night a cleaner works late; with it, the rule says what everyone actually means: the door opened and nobody was authorised to open it. |
| `NodeId` | Scopes the clause to one node. Empty means any node — what you want for "a badge swipe anywhere on this site". |
| `Kind` | Scopes to a node TYPE (`"camera"` / `"iot"`). Empty means either. Resolved against `ManagedNode.Kind` (the adopted node's own record), never against anything an event body claims — see `correlate.go.md`. |
| `Category` | Matches the event's notification category (`"vision.alert"`, `"device.alert"`, ...). |
| `Match` | Case-insensitive substring matched against the event's title and body — in practice, the rule name that fired on the node ("Person detected", "Front door opened"). A substring rather than a structured field, because a node's alert IS its rule's name, and the operator who wrote that rule is the one writing this one; a required taxonomy would mean the feature is never used. |

## Notes

- A rule made only of `"absent"` clauses is refused at save time (`validateFleetRule` in `correlate_crud.go`) and again defensively at evaluation time (`Correlator.requiredSatisfied`) — it would fire on nothing at all, forever, and a rule that fires on nothing is worse than no rule because somebody will trust it.
- Both entities are registered for DB bootstrap in `apps/myseliasan/app/app.go`'s `Entities()`.
