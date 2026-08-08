# Module: apps/myseliasan/services/correlate.go

## Purpose

The cross-domain correlator. **This is the reason the fourth app (`myiotsan`) exists — and the
fifth (`mypintusan`) makes the flagship example literally true.**

    motion on Camera 3 (a mymatasan node)
    AND a door contact opening (a myiotsan node)
    AND no badge accepted (a mypintusan door node, category access.granted;
        or a badge reader wired through a myiotsan hub)
    -> intrusion

`mymatasan` cannot see your door sensors. `myiotsan` cannot see your cameras. `mypintusan` cannot
see either. A cloud IoT platform can see none of them. Only the control plane — which already
receives every node's events in one feed over the fleet control channel — is in a position to
notice the conjunction. And the conjunction is where the signal is: a camera's motion alert at
03:00 is a moth; a door contact at 03:00 is a cleaner; the two together with no badge accepted is
an intrusion. Correlation is how a fleet of noisy sensors becomes one trustworthy signal.

## The grace delay — the hard part

The canonical rule is "door opened AND no badge swipe". But a badge reader reports over a
network, through a controller, into a hub — it is routinely a second or two BEHIND the door
contact it just authorised. Fire the moment the door opens and the rule cries intrusion on
EVERY legitimate badge entry, all day, until somebody turns it off — and then the one real
intrusion is not alerted on either.

So `Correlator` **never fires on an event**. When a rule's required clauses are all satisfied
it **ARMS** (`Observe`), waits out `GraceSeconds`, and only then (`Sweep`, on a 1-second ticker
started in `app.go`) asks whether the absent clauses really held. A badge swipe arriving inside
the grace period **DISARMS** it — that was an authorised entry and no alert may ever be raised.
An absence you have not waited for is not an absence; it is a race with the badge reader.
Thirteen tests in `correlate_test.go` pin this state machine; the late-arriving-badge-swipe test
is the one that matters most. Three of them
(`TestCorrelate_DoorNodeBadgeAcceptedDisarms`, `TestCorrelate_DoorNodeBadgeDeniedDoesNotDisarm`,
`TestCorrelate_DoorScopedAbsenceIgnoresOtherKinds`) pin the identical state machine against a
REAL `mypintusan` door node: a badge accepted on the door node (category `access.granted`)
disarms within grace; a badge DENIED (`access.denied`) does not, because a denial is not
authorisation; and a `door`-scoped absence clause is not satisfied by an `access.granted`-shaped
event from a node of a DIFFERENT kind (e.g. an IoT hub relaying something that merely shares the
category string).

## Type: `Correlator`

### Constructor

```go
func NewCorrelator(db dbsql.IDbCrud, notify *notification.Service,
    nodeKind func(ctx context.Context, nodeId string) string, logf func(string, ...any)) *Correlator
```

`nodeKind` resolves a node ID to its `"camera"`/`"iot"`/`"door"` kind. `apps/myseliasan/app/app.go`
wires this to a closure over `registry.List` — i.e. **the ADOPTED NODE'S RECORD**, never anything
carried in the event itself. Trusting a kind in the event body would let any node claim to be a
camera and satisfy a camera-scoped clause.

### `SetMetrics`

```go
func (c *Correlator) SetMetrics(m telemetry.Metrics)
```

A setter, not a constructor argument — the ten existing tests in `correlate_test.go` all call
`NewCorrelator` directly and would otherwise every one grow a `nil` metrics arg. Optional: a
correlator with none wired still works, `fire` nil-guards it. `app.go` calls it once, right after
construction, before `Reload`.

### `SetEnricher` / `HasRuleFor`

```go
func (c *Correlator) SetEnricher(fn func(ctx context.Context, ruleName string) string)
func (c *Correlator) HasRuleFor(nodeId, category string) bool
```

`SetEnricher` wires an optional string provider appended to a fired rule's notification body
(`fire`, below) — `apps/myseliasan/services/correlate_enrich.go.md`'s `NewFleetRuleEnricher`
appends deterministic recurrence context ("this rule also fired 3 times in the last 7 days, most
recently …"). The contract is strict because this sits in the **alert path**: the enricher must be
deterministic (DB reads only, never an LLM — the digest is where language lives, an alert is
where facts live), bounded by `enrichTimeout` (2s, `context.WithTimeout` wraps every call), and
any failure or timeout costs only the extra sentence, never the alert itself. Optional: `nil`
(the default) means `fire` publishes exactly the `explain` body, unchanged from before.

`HasRuleFor` reports whether any cached rule already carries a `"required"` clause matching this
`(nodeId, category)` pair — the digest's suggested-rule detector
(`services/agent_findings.go.md`'s `suggestedRuleFindings`, wired via
`DigestService.SetRuleChecker(correlator.HasRuleFor)` in `app.go`) uses it to avoid proposing a
rule the operator already wrote. Advisory only: it reads the same `cached` rule set `Observe`/
`Sweep` use, so its freshness is exactly `Reload`'s — good enough for a suggestion, not a
guarantee.

### `NodeEvent`

The flattened shape a rule matches on: `NodeId`, `Kind`, `Category`, `Title`, `Body`, `At`.
Built by `apps/myseliasan/app/correlate_bridge.go`'s `observeForCorrelation` from the raw event
frame a node pushes up the control channel — **the node's own event, never the control plane's
own re-published copy of it** (see `correlate_bridge.go.md` for why mixing the two would let one
fleet rule's alert satisfy another fleet rule's clause and let two rules trigger each other
forever).

### Methods

| Method | Description |
|---|---|
| `Reload(ctx)` | Refreshes the in-memory rule+clause cache from the DB (up to 200 rules / 50 clauses per rule). Called at startup and after every `Save`/`Delete`. |
| `Observe(ctx, e NodeEvent)` | Advances every cached rule against one event: records which clauses it satisfies (`seen`), disarms a pending rule if it satisfies one of that rule's `"absent"` clauses within the window, and arms a rule (starts its grace timer) once every `"required"` clause has been seen within `WindowSeconds`. Never fires here. |
| `Sweep(ctx)` | Runs on a ticker (`app.go`, every 1s). Fires every rule whose grace period has elapsed with its absent conditions still holding, subject to `CooldownSeconds`/`LastTriggeredAt`. This is what makes an ABSENCE decidable — nothing ever arrives to say "the badge was never swiped", so the passage of time has to. |

## `requiredSatisfied` — the anti-nothing-rule guard

A rule with zero `"required"` clauses returns `false` unconditionally, even if every clause
technically "matched" — a rule made only of absences would arm immediately and fire on nothing
at all, forever, and a rule that fires on nothing is worse than no rule because somebody will
trust it. (Defense in depth: `correlate_crud.go`'s `validateFleetRule` also refuses to save such
a rule.)

## `fire` / `explain`

On fire: persists `LastTriggeredAt` (survives restart, so the cooldown holds across one),
increments `MetricFleetRuleFiredTotal` (`myseliasan_fleet_rule_fired_total`, labeled `severity`;
`services/metrics.go.md`) when a metrics recorder is wired, updates the in-memory cache to match,
and publishes a `notification.Notification`
(`CategorySystem`, `Source: "fleet-rule"`, `Data["fleetRuleId"]`) whose body is built by
`explain` — the sentence an operator reads at 03:00. It names what DID happen and what did
NOT, because "correlation rule 4 fired" tells nobody anything and the absence is half the
finding (e.g. `"Person detected on cam-3, and Front door opened on node-7 — with no Badge
swipe (within 30s)"`). When `c.enrich` is wired (`SetEnricher`, above), its output — when
non-empty — is appended as a second line under a hard `enrichTimeout`; a slow or failing enricher
never delays or drops the alert itself.

## `clauseMatches`

A clause matches an event when every non-empty field it specifies matches: `NodeId`
(case-insensitive exact), `Kind` (exact), `Category` (exact), `Match` (case-insensitive
substring against `Title + " " + Body`). An empty field on the clause matches anything.

## Notes

- All correlator state (`seen`, `armed`) is in-memory only and is dropped on restart — a rule that was armed under old clauses is explicitly cleared on `Save`/`Delete` too, so an edit never fires under stale conditions.
- `isNoResultErr` mirrors the same repo "row not present" sentinel-message check used elsewhere in the codebase (e.g. `domain/shared/fleetnode.isNoResultFoundErr`).
- See `correlate_crud.go.md` for `List`/`Save`/`Delete` and rule validation, `apps/myseliasan/apis/fleet_rules_api.go.md` for the HTTP surface, and `apps/myseliasan/app/correlate_bridge.go.md` + `apps/myseliasan/app/app.go.md` for wiring.
