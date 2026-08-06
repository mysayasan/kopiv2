# Module: apps/myseliasan/services/agent_digest.go

## Purpose

`DigestService` orchestrates the fleet digest: collect findings (deterministic,
`agent_findings.go.md`), optionally polish them with the LLM (best-effort), persist
(`entities.AgentDigest`), publish a summary to the notification feed.

**The invariant**: `Generate` never fails because of the LLM. A model that is off, still loading,
crashed, timing out, or answering garbage costs exactly one thing — the prose narrative — and the
digest ships without it.

## `Generate(ctx, kind, actor) (*entities.AgentDigest, error)`

`kind` is `"daily"` or `"weekly"` (scheduler, `agent_schedule.go.md`) or `"manual"` (an operator's
Generate-now, `?kind=weekly` selects the 7-day one there too); `actor` is the operator's user id
for manual runs, `0` for the scheduler.

1. `CollectFindings` over the window: `agent.digest.windowHours` (default 24) for `daily`/
   `manual`, or a **fixed 168h (7-day)** window for `kind == "weekly"` regardless of the
   configured daily look-back — the weekly digest is the management-cadence summary and its
   window is not a settings knob. `d.ruleFor` (below) is threaded through so the suggested-rule
   detector can skip patterns an existing fleet rule already covers.
2. `polish` — best-effort LLM rewrite (below); any failure returns `("", "", "none")`, never an
   error.
3. Persists an `entities.AgentDigest` row (`Severity` from `MaxFindingSeverity`).
4. Publishes a best-effort, **English**, single-line feed entry (`Source: "ai-digest"`,
   `RefType: "agent_digest"`, `Link: "#insight"`) — the localized rendering lives on the Insight
   page the entry links to, exactly like every other server-published notification stays English.
5. Records `MetricAgentDigestRunsTotal{outcome, narrative}` and
   `MetricAgentDigestDurationMs`.

## `SetRuleChecker(fn func(nodeId, category string) bool)`

Wires `d.ruleFor` — the correlator's `Correlator.HasRuleFor` — post-construction (the correlator
is built later in `app.go`'s `RegisterAppRoutes` than the digest service). Optional: `nil` means
the suggested-rule finding never dedups against existing fleet rules (suggests anyway). Passed
through to both `CollectFindings` (via `FindingsInput.RuleFor`) and `GenerateBriefing`.

## `GenerateBriefing(ctx, windowHours) (Briefing, error)`

The report-facing sibling of `Generate`: runs the identical `CollectFindings` narrator over an
arbitrary window and (optionally) polishes it with the LLM, but **persists nothing** — no
`AgentDigest` row, no feed entry, no last-run guard touched. `reports.go`'s `executiveSummary`
calls this once per report build (`services/reports.go.md`).

`Briefing{Findings, Lines, Narrative, Model}`: `Lines` are the deterministic English sentences
(`findingEnglish`, one per finding) and always render; `Narrative`/`Model` are populated only when
`polish` actually reached an LLM (`source == "llm"`) — a disabled/unreachable/failed model leaves
them blank and the caller renders `Lines` alone. Never returns an error for LLM trouble, same
invariant as `Generate`.

**The narrative is always requested in English**, overriding `cfg.Digest.Language` before calling
`polish` — `domain/report` renders PDF text in cp1252/Helvetica, which cannot represent `zh`/`ar`
glyphs, and a blank or mojibake section in a report is worse than an English one. This is
independent of the digest's own configured language, which the scheduled/manual digest still
honors for its own narrative and the Insight page's rendering.

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
