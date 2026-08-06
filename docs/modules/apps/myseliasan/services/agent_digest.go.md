# Module: apps/myseliasan/services/agent_digest.go

## Purpose

`DigestService` orchestrates the fleet digest: collect findings (deterministic,
`agent_findings.go.md`), optionally polish them with the LLM (best-effort), persist
(`entities.AgentDigest`), publish a summary to the notification feed.

**The invariant**: `Generate` never fails because of the LLM. A model that is off, still loading,
crashed, timing out, or answering garbage costs exactly one thing — the prose narrative — and the
digest ships without it.

## `Generate(ctx, kind, actor) (*entities.AgentDigest, error)`

`kind` is `"daily"` (scheduler, `agent_schedule.go.md`) or `"manual"` (an operator's
Generate-now); `actor` is the operator's user id for manual runs, `0` for the scheduler.

1. `CollectFindings` over the configured window (`agent.digest.windowHours`, default 24).
2. `polish` — best-effort LLM rewrite (below); any failure returns `("", "", "none")`, never an
   error.
3. Persists an `entities.AgentDigest` row (`Severity` from `MaxFindingSeverity`).
4. Publishes a best-effort, **English**, single-line feed entry (`Source: "ai-digest"`,
   `RefType: "agent_digest"`, `Link: "#insight"`) — the localized rendering lives on the Insight
   page the entry links to, exactly like every other server-published notification stays English.
5. Records `MetricAgentDigestRunsTotal{outcome, narrative}` and
   `MetricAgentDigestDurationMs`.

## `polish(ctx, findings, cfg) (narrative, model, source string)`

Asks the LLM (`d.llm.Client()`) to rewrite the findings as an operator briefing, under
`digestLLMTimeout` (45s — independent of the chat timeout, since the scheduler must never hang a
morning digest on a wedged model). Any failure (LLM disabled/not-ready/error/timeout/empty
response) returns `("", "", "none")`.

The model receives **pre-rendered English lines** (`renderFindingsForLLM`/`findingEnglish`), not
the findings JSON: small models copy raw unix timestamps and field soup into garbled prose
("17:85 UTC on 2021-01-01", live-bench verbatim) when given structured data, but rewrite plain
pre-formatted sentences with the numbers already correct essentially faithfully.

`digestSystemPrompt(lang)` is the polish instruction — the grounding rule is absolute: "use ONLY
the facts, numbers, names, and times present in the findings — never add, estimate, or infer
anything." `languageName(lang)` maps `en`/`ms`/`zh`/`ar` to the instruction's target-language name.

## `List`/`Latest`/`PurgeOld`

- `List(ctx, limit, offset)` — newest-first (`limit` 0 or >100 clamps to 20).
- `Latest(ctx)` — `nil` when no digest exists yet (not an error).
- `PurgeOld(ctx, retentionDays)` — `retentionDays <= 0` is a no-op (keep everything); otherwise
  deletes rows older than the cutoff. Called daily from `app.go`'s `periodic` helper
  (`agent.digest.retentionDays`, default 180).

## Constructor

`NewDigestService(db, notif *notification.Service, fleet, audit, llmMgr *LLMManager, cfg func()
config.AgentConfigModel, metrics, logf)` — `cfg` is a getter (not a captured value) so the digest
always reads the live config block; `notif` satisfies both `digestNotifSource` (the narrator's
read seam) and `digestPublisher` (the feed's write seam), faked independently in tests.

## Notes

- `countBySeverity`/`count`/`countLLM` feed the metrics catalog documented in
  `services/metrics.go.md`'s "fleet AI agent" section.
- See `services/agent_schedule.go.md` for when `Generate("daily", 0)` fires, and
  `apis/agent.go.md` for `POST /api/agent/digests/generate` (the manual path).
