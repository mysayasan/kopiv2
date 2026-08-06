# Module: apps/myseliasan/services/correlate_enrich.go

## Purpose

The fleet-rule enricher: appends deterministic recurrence context to a fired rule's notification
body, wired into `Correlator` via `SetEnricher` (`services/correlate.go.md`). Turns "correlation
rule 4 fired" into "…and this is the 3rd time this week" — a fact that changes how fast an
operator moves at 03:00.

## Why deterministic, not the LLM

This runs **inside the alert path**, under the correlator's `enrichTimeout` (2s). An alert is
where facts live; the digest (`services/agent_digest.go.md`) is where language lives. Mixing the
two — routing an alert through a model call — would put an LLM's latency and failure modes
between a real event and the operator, which is exactly the invariant the whole fleet AI agent is
built to avoid ("the LLM is never in a critical path"). So the enricher is a couple of bounded
feed queries and string formatting, nothing else.

## `NewFleetRuleEnricher(notif ruleHistorySource) func(ctx context.Context, ruleName string) string`

Returns the closure `Correlator.SetEnricher` takes. On each call:

1. Pages `notif.List(ctx, page, offset, 0, false, "", "fleet-rule")` (`page`=200,
   `ruleHistorySource` is the sliver of `*notification.Service` this needs) newest-first, over
   `enrichLookback` (7 days), up to `enrichFetchCap` (1000 rows).
2. Counts rows whose `Title` case-insensitively equals `ruleName` — the fleet-rule feed entry's
   title is the rule's own name (`Correlator.explain`'s notification, `Source: "fleet-rule"`) — and
   tracks the most recent match's timestamp.
3. Stops paging once a row falls outside the lookback window or a page comes back short.

Returns `""` (no sentence appended) when `count == 0` — a rule's **first** firing needs no
history — or on any list error. Otherwise: `"Context: this rule also fired %d time(s) in the last
7 days, most recently %s."`. `count` is **prior** firings only — the firing that triggered this
very enrichment call has not been published yet, so it is never double-counted.

## Notes

- `ruleHistorySource` is satisfied by `*notification.Service` (just `List`), faked in
  `correlate_enrich_test.go`.
- Constructed once in `app.go`'s `RegisterAppRoutes`:
  `correlator.SetEnricher(services.NewFleetRuleEnricher(notificationService))`, right after the
  correlator is built and before `Reload`.
- See `services/correlate.go.md` for the `fire`/`SetEnricher` contract this closure fulfills, and
  `services/agent_findings.go.md`'s `suggestedRuleFindings` for the digest-side counterpart —
  `Correlator.HasRuleFor` — that also reads the rule cache this enricher's sibling
  (`fire`/`explain`) publishes into.
