# Module: apps/myseliasan/services/agent_findings.go

## Purpose

The deterministic narrator: every insight the fleet digest reports is computed here, in plain Go,
from data the control plane already holds. No LLM is involved — the language model (when enabled)
only **rephrases** these findings (`agent_digest.go`'s `polish`), so a wrong number in a digest is
a bug in this file, never a hallucination.

Findings are **structured** (a typed code + parameters + grounding ids) rather than prose, because
prose has a language and the suite has four. The frontend renders each finding through its own
i18n dictionary (key `agent.finding.<code>`), so one digest reads natively in en, ms, zh, and ar
alike.

## The control-plane-specific rule

Relayed node events keep their **origin** camera ids, and camera 3 on node A is not camera 3 on
node B. Per-camera analytics would silently merge unrelated cameras across the fleet, so every
per-thing finding here is keyed by **source** (`"node:<id>"`) instead, and the baseline band
(`FindingBaselineSpike`/`FindingBaselineQuiet`) is fleet-wide only — this is the same reason
`apis/notifications.go`'s new `baseline` endpoint refuses a `cameraId` filter on this app.

## `Finding`

`{Code, Severity, Params map[string]any, NotificationIds []int64, NodeIds []string}` — `Code`
selects the i18n key; `Params` are interpolated into the localized sentence; the id slices ground
the finding in clickable records.

### Finding codes (the taxonomy)

`volume_delta`, `critical_events`, `baseline_spike`, `baseline_quiet`, `source_anomaly`,
`top_source`, `noisy_source`, `node_offline`, `cert_expiring`, `cert_expired`, `fleet_rule_fired`,
`feed_growth`, `audit_highlight`, `all_quiet`. **Adding a code means adding its i18n key to all
four frontend dictionaries** — see `apps/myseliasan/views/react-webpack/src/views/i18n/*.js`
(`agent.finding.*`).

## `CollectFindings(ctx, in FindingsInput, notif, fleet, audit) ([]Finding, error)`

Computes the digest findings for the window ending at `in.Now` (`in.WindowHours`, default 24).
Each section collector degrades independently — a failing baseline query costs only the baseline
findings, not the whole digest:

- **Rows first** (`fetchWindowRows`, pages the feed newest-first up to `windowRowsFetchCap`=2000):
  also supplies `adjustedSeverityCounts`, the digest-exclusion-corrected severity tally the volume
  finding needs, since `Stats` cannot exclude a source and the digest's own critical-severity feed
  entry must never inflate the *next* digest's numbers (see "`digestOwnSource`" below).
- `volumeFindings` — total/critical/warning vs. the previous equal window; warns at ≥50% delta with
  ≥20 total events.
- `topSourceFindings` — top 3 sources by count (excluding the digest's own feed entries).
- `sourceAnomalyFindings` — per-source current-vs-previous-window volume swings (≥10 events and
  ≥3× the prior window, either direction). Shared with `agent_chat.go`'s grounding bundle.
- `baselineFindings` — flags chart buckets whose actual total breached the fleet-wide expected
  band (`notification.Baseline`, the same rollup-backed substrate `apis/notifications.go`'s
  `baseline` endpoint serves); learning buckets never flag.
- `rowFindings` — from the single window fetch: critical-severity ids, fleet-rule firings
  (`Source == "fleet-rule"`), and **noisy sources** (high volume, ≥80% unread — an alert nobody
  reads is noise, and noise is what gets a whole feed ignored).
- `feedGrowthFinding` — warns once the notifications table exceeds `feedGrowthWarnRows` (100k) —
  the retention-purge-is-off symptom (`app.go`'s `MetricNotificationsPurgedTotal`).
- `fleetFindings` — node offline / cert expiring / cert expired, capped `perCodeCap`=5 each; a
  quarter or more of the fleet dark escalates every `node_offline` finding to `critical`.
- `auditFindings` — sensitive `IAuditService` actions (`sensitiveAuditActions`: settings
  save/reset, node adopt/release/command/revoke, RBAC role/disable/elevate, fleet key rotate) that
  occurred ≥3 times in-window.
- A completely quiet window emits a single `all_quiet` finding rather than an empty list, so the
  digest always has *something* to say.

Findings are capped at `findingsMaxTotal`=30 and sorted by `sortFindings` (severity first, then a
fixed `codeOrder` taxonomy order within a tier). `MaxFindingSeverity` returns the highest severity
present (`"info"` floor) — this becomes `AgentDigest.Severity`.

## `digestOwnSource`

`const digestOwnSource = "ai-digest"` — the `Source` the digest publishes its own feed entry
under. **The narrator must never read its own output back as evidence**: yesterday's
critical-severity digest notification would otherwise count as today's "critical event", and every
digest from then on stays critical forever. Events come from the fleet; conclusions come from
here; the two must not be mixed — `Correlator` makes the identical exclusion for the identical
reason (see `services/correlate.go.md`). Every row-scanning collector (`adjustedSeverityCounts`,
`topSourceFindings`, `sourceAnomalyFindings`, `rowFindings`) skips `n.Source == digestOwnSource`.

## Interfaces

- `digestNotifSource` — the sliver of `*notification.Service` the narrator reads (`Stats`,
  `Baseline`, `List`); satisfied by the real service, faked in tests.
- `digestFleetSource` — the sliver of the node registry read (`List`, `FleetStatus`).

## Notes

- Tuning thresholds (`volumeDeltaWarnPct`, `sourceAnomalyMinCount`/`Factor`,
  `noisySourceMinCount`/`UnreadPct`, `feedGrowthWarnRows`, `auditHighlightMinCount`, …) are
  deliberately conservative constants at the top of the file: "a digest that cries wolf daily is
  deleted from the morning routine within a week."
- Shared with `services/agent_chat.go.md`: `sourceAnomalyFindings` backs the chat grounding
  bundle's `Anomalies` section too, so "ask the fleet" and the digest agree on what counts as an
  anomaly.
