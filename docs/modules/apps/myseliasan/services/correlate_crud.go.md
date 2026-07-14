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
- `Save(ctx, req)` — creates or updates a rule, replacing its clauses **wholesale** (delete-all-then-insert) rather than diffing: a rule is a small declarative document, and an edit that half-applies is worse than one that fully replaces. On update, preserves `LastTriggeredAt` and `CreatedAt` from the existing row — the cooldown must survive an edit, or editing a rule mid-incident re-arms it and it fires again immediately. Drops any in-flight `armed`/`seen` state for the rule (a rule armed under the OLD clauses must not fire under the new ones), then calls `Reload`.
- `Delete(ctx, id)` — deletes the rule's clauses then the rule, clears any `armed` state for it, and reloads.

## Notes

- Clause replacement and the rule row itself are two separate repo calls (not one wrapped transaction as of this writing) — see the "Save is wholesale, not diffed" note above for why the intent is atomic even though a full DB transaction wrapper isn't yet in place.
- `NewFleetRulesApi` (`apis/fleet_rules_api.go.md`) is the only HTTP entry point onto `Save`/`Delete`, and both are superadmin-only there — a correlation rule is a security control that spans the whole estate, and whoever can write one can also write one that never fires.
