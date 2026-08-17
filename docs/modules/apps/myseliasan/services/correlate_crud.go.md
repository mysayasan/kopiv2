# Module: apps/myseliasan/services/correlate_crud.go

## Purpose

CRUD for `FleetRule`/`FleetRuleClause` (`Correlator.List`/`Save`/`Delete`) plus the validation
that refuses a rule that can never mean anything. See `correlate.go.md` for the evaluation
engine this feeds.

## Types

- `FleetRuleDetail` — `{Rule *entities.FleetRule, Clauses []*entities.FleetRuleClause}`, the shape returned to the frontend.
- `SaveFleetRuleRequest` — the save payload: rule fields plus `Clauses []SaveFleetClause`.
- `SaveFleetClause` — one clause: `Mode`, `NodeId`, `Kind`, `Category`, `Match`.

## Responsibilities

- `List(ctx)` — all rules with their clauses, sorted by `Id` ascending.
- `validateFleetRule(req)` — refuses to save a rule that:
  - has no `Name` ("the rule needs a name — it is what an operator reads at 03:00"),
  - has a clause whose `Mode` is neither `"required"` nor `"absent"`,
  - has a clause with neither `Match` nor `Category` set ("a clause that matches nothing in particular would match everything"),
  - has **zero `"required"` clauses** — the same guard `Correlator.requiredSatisfied` enforces defensively at evaluation time; a rule made only of absences fires on nothing at all, forever, and is refused here before it can ever be saved,
  - has `WindowSeconds <= 0` ("the rule needs a window: how close in time these events must be to count as one event").
- `Save(ctx, req)` — creates or updates a rule, replacing its clauses **wholesale** (delete-all-then-insert) rather than diffing: a rule is a small declarative document, and an edit that half-applies is worse than one that fully replaces. On update, preserves `LastTriggeredAt` and `CreatedAt` from the existing row — the cooldown must survive an edit, or editing a rule mid-incident re-arms it and it fires again immediately. Drops any in-flight `armed`/`seen` state for the rule (a rule armed under the OLD clauses must not fire under the new ones), then calls `Reload` and, on success, `c.announceRulesChanged()` (`correlate.go.md`) so the rest of a clustered deployment reloads too.
- `Delete(ctx, id)` — deletes the rule's clauses then the rule, clears any `armed` state for it, calls `Reload`, and — on success — `c.announceRulesChanged()`, the same as `Save`.

## Cross-instance reload — see `correlate.go.md`

`Save` and `Delete` both call `Reload` (refreshing THIS instance's own cache) and then
`announceRulesChanged` (`correlate.go.md`), which — over the event bus wired in `app.go` — tells
every other instance in a clustered deployment to reload its own cache too. See `correlate.go.md`'s
"Cross-instance rule reload (Phase 4)" note.

### Regression guard: `correlate_announce_test.go`

A source-level (go/ast) test, not a behavioral one: it parses `correlate_crud.go` and asserts
`Save` and `Delete` each contain a call to `announceRulesChanged`, failing with a clear message if
either doesn't. This exists because the call was, briefly during development, wired everywhere
(`SetOnRulesChanged`, the event bus, the subscriber) except actually invoked from `Save`/`Delete`
— and since Go does not complain about an unused method, that state compiled and passed every
other test while doing nothing. A behavioral/integration test could catch this too, but only by
actually standing up two instances and a shared bus; asserting on the call site is cheap, runs in
every unit-test pass, and can't come back green if a future refactor drops the call again.

## Notes

- Clause replacement and the rule row itself are two separate repo calls (not one wrapped transaction as of this writing) — see the "Save is wholesale, not diffed" note above for why the intent is atomic even though a full DB transaction wrapper isn't yet in place.
- `NewFleetRulesApi` (`apis/fleet_rules_api.go.md`) is the only HTTP entry point onto `Save`/`Delete`, and both are superadmin-only there — a correlation rule is a security control that spans the whole estate, and whoever can write one can also write one that never fires.
