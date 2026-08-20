# Module: apps/myseliasan/services/fleet_policy.go

## Purpose

CRUD for `FleetPolicy`/`FleetPolicyItem` (`entities/fleet_policy.go.md`) plus
`ResolveEffectivePolicy`, the pure function that merges every policy applicable to one node
into a single desired configuration. See `services/policy_catalog.go.md` for what a policy
may govern and `services/fleet_policy_reconciler.go.md` for what compares the result against
a live node.

## Responsibilities

- `IFleetPolicyService` — `List`, `Save(req, actor)`, `Delete(id)`, `Resolve(ctx, node)` (one
  node's effective policy), `MarkEvaluated(ctx, policyIds, at)` (stamps `LastEvaluatedAt` on
  the policies a reconcile pass actually compared against a reached node — never on policies
  whose target was unreachable, so "checked 2 minutes ago" is never shown next to a
  comparison that never happened).
- `NewFleetPolicyService(db)` builds its own repos from `dbsql.IDbCrud`, the same convention
  every other myseliasan service follows.
- `validateFleetPolicy(req)` — refuses policies that cannot mean anything, and more
  importantly, policies that would produce drift no node can ever clear: an empty name, an
  invalid scope, a site/node scope with no target, a `NodeKind` with no governable catalog
  section, zero items, a section the node kind does not have, an unknown field, a field set
  twice, or a value that fails `ParsePolicyValue` (type/bounds). Everything is checked at
  save time so a bad policy is a dialog error, not a permanently red node nobody can explain.
- `Save` replaces a policy's items wholesale on every save (same convention as a correlation
  rule's clauses) rather than diffing — a policy is a small declarative document and a
  half-applied edit is worse than a full replace. `LastEvaluatedAt` is deliberately NOT
  carried over on an edit.
- `Delete` removes the policy's items before the policy row.

## `ResolveEffectivePolicy(node, policies) EffectivePolicy`

The interesting part, and deliberately a pure function of `(node, policies)` — no database,
no node, no control channel — so it is unit-testable in isolation
(`fleet_policy_test.go`).

1. Filters to policies that are `Enabled`, match the node's (normalized) kind, and match
   scope: fleet always matches; site matches only when the node has a non-zero `SiteId`
   equal to the policy's `TargetId` (a node with no site is matched by NO site policy — a
   wildcard here would leak one site's standard onto every standalone recorder); node
   matches only an exact `NodeId`.
2. Sorts the applicable policies weakest-first: fleet, then site, then node
   (`entities.PolicyScopeRank`), and within one scope by ascending `Id` — so the merge is a
   straight last-write-wins overwrite per field rather than a comparison at every field, and
   the tie-break (higher id / more recently created policy wins a same-scope contest) is
   deterministic regardless of database row order — which matters because a clustered
   control plane's instances must agree on what the fleet wants without comparing notes.
3. Merges every applicable policy's items into a `map[section\x00field]ResolvedField`,
   skipping any section/field the catalog does not recognize (a future-version row, or one
   whose catalog entry no longer exists) and any value that fails to re-parse (a hand-edited
   or stale-bounds row) — dropping rather than pushing an unparseable value.
4. `ResolvedField` records not just the winning value but which policy won it and why
   (`PolicyId`/`PolicyName`/`Scope`) and that policy's `Enforce` flag — `Enforce` is tracked
   PER FIELD, not per node, because a report-only fleet policy and an enforcing node override
   legitimately coexist (how an operator pins one appliance without arming the whole
   estate).

`EffectivePolicy.Empty()` reports whether the fleet has no opinion at all about a node — the
`unmanaged` compliance state in the reconciler.

## Notes

- `itemsFor` uses `Get` + an `Equal` filter, never `GetByForeign` — that helper returns
  exactly ONE row (see `docs/modules/../getbyforeign-limit1-bug.md`-class trap noted
  elsewhere in the suite), so a policy with four governed settings would silently become a
  policy with one.
- `maxPolicies = 500` caps how many policies are resolved in one pass; resolution is
  O(policies × nodes) and runs on a timer, so the cap keeps a runaway import from turning the
  reconciler into the busiest thing in the process.
- Covered by `fleet_policy_test.go` (validation edge cases + `ResolveEffectivePolicy`
  precedence, including the same-scope tie-break and the no-site-policy-wildcard case).
