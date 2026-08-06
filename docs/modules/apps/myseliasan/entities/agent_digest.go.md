# Module: apps/myseliasan/entities/agent_digest.go

## Purpose

`AgentDigest` is one generated fleet digest: the structured findings the deterministic narrator
(`services/agent_findings.go.md`) produced over a time window, plus the OPTIONAL language
narrative an LLM wrote over those findings (`services/agent_digest.go.md`'s `polish`).

## Fields

- `Id`, `Kind` (`"daily"` scheduler / `"manual"` an operator's Generate-now), `PeriodStart`/
  `PeriodEnd` (unix seconds bounding the findings window).
- `FindingsJson` — a JSON array of `services.Finding` (typed code + params + grounding ids). This
  is the record of substance: the frontend localizes each finding through its own i18n dictionary
  (`agent.finding.<code>`), so a digest generated at 07:00 reads natively in en/ms/zh/ar. The
  server never bakes prose into a finding.
- `Severity` — the max finding severity (`info`/`warning`/`critical`), denormalized so history
  lists can badge rows without decoding `FindingsJson`.
- `Narrative`/`NarrativeLang`/`NarrativeSource`/`Model` — optional LLM prose. `NarrativeSource` is
  `"none"` (narrator only) or `"llm"`. A digest with an empty `Narrative` is **not** degraded data
  — it is the same findings, undecorated; the LLM layer only ever adds presentation on top.
- `DurationMs` — how long `Generate` took, including any LLM call.
- `GeneratedBy` — the operator's user id for a manual digest, `0` for the scheduler.
- `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt` — standard audit columns.

## Notes

- Registered for DB bootstrap in `app.go`'s `Entities()`.
- Written by `services.DigestService.Generate` (`services/agent_digest.go.md`), read back by
  `List`/`Latest`, and pruned by `PurgeOld` (daily retention sweep, `agent.digest.retentionDays`,
  default 180; `0` keeps everything).
- `apis/agent.go`'s `digestDTO` decodes `FindingsJson` into `[]services.Finding` for the HTTP
  response and blanks the raw JSON field, so the SPA never re-parses a string.
