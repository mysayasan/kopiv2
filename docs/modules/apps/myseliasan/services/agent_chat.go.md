# Module: apps/myseliasan/services/agent_chat.go

## Purpose

"Ask the fleet" — the chat side of the AI agent (`ChatService`).

## Design: route → fetch → summarize, deliberately not an agentic tool loop

The models this must work with are 1–2B parameter CPU models, and those cannot reliably drive
multi-step tool calling; what they **can** do is answer a question over a compact document. So the
server assembles one grounding bundle (`BuildGrounding`) — fleet status, windowed stats, per-source
anomalies, the latest digest's findings, recent high-severity events — and the model gets exactly
one completion over it, under a system prompt that forbids answering from anything else.

**Grounding is central-only this release**: everything comes from the control plane's own tables.
Fanning out to nodes over the control tunnel could stall a reply behind a 30s-per-node timeout;
per-node drill-down is a documented follow-up, not built.

## `ChatRequestBody`

`{Question, History []llm.Message, Lang, WindowDays, TZOffsetMin}` — the `POST /api/agent/chat`
payload. `Validate` normalizes it: trims/length-caps `Question` (`chatMaxQuestionChars`=2000),
clamps `WindowDays` to `1..chatMaxDays`(31, default `chatDefaultDays`=7), whitelists `Lang` to
`en`/`ms`/`zh`/`ar` (else `en`), truncates `History` to the last `chatMaxHistoryTurns`=4 turns
(each capped `chatMaxHistoryChars`=400), and **forces every history role to `"assistant"` or
`"user"`** — a client must not be able to smuggle a second system prompt in via a spoofed history
role.

## `GroundingBundle`

`BuildGrounding(ctx, req)` assembles `{GeneratedAt, Window, Fleet, FleetTotal, Stats, Anomalies,
LatestDigest, Recent, Truncated}`:

- **Fleet** — every adopted node's live state (capped `chatFleetCap`=50), `Connected` from the
  control-channel liveness oracle, `CertDaysLeft` when the node has a cert.
- **Stats** — `notif.Stats` over the window, capped top-10 `BySource`/`ByCategory`.
- **Anomalies** — `sourceAnomalyFindings` (shared with `agent_findings.go.md`), capped
  `chatAnomalyCap`=10.
- **LatestDigest** — the most recent digest's findings (**codes + params only, never the
  narrative** — the narrative is prose and would double-spend tokens for no grounding benefit).
- **Recent** — `recentSevere`: newest warning/critical events in-window (`chatRecentCap`=15),
  titles capped `chatTitleCap`=80, ids included so the model can cite `[notif <id>]`. Skips
  `digestOwnSource` — the digest's own feed entries are conclusions, not evidence, for the exact
  reason documented in `agent_findings.go.md`.

**Size enforcement**: `chatMaxBundleBytes`=8KiB (≈2.3k tokens at ~4 bytes/token — fits an 8k
context with prompt+history+completion room to spare) is enforced by dropping sections in reverse
priority — `recentEvents` → `anomalies` → `latestDigestFindings` → trim `fleet` to 10 — recording
each drop in `Truncated` so the model (and the UI) can say "I only see part of the data."

## `SystemPrompt(lang)`

The strict-grounding instruction: answer only from FLEET DATA; cite `[notif <id>]`/`[node <id>]`
when referencing a record; every number must appear verbatim in FLEET DATA — never invented;
mention when a section was truncated; ≤8 sentences; reply in the requested language.

## `Stream(ctx, req, emit func(delta string) error) (llm.ChatResult, error)`

`Validate` → `llm.Client()` (typed `ErrLLMDisabled`/`ErrLLMNotReady` on failure, mapped to
409/503 by `apis/agent.go`) → `BuildGrounding` → one `client.ChatStream` call with the system
prompt (grounding bundle inlined as `FLEET DATA:`) + history + the question. For a non-English
`Lang`, the language instruction is **repeated right next to the question** (not just in the
system prompt) — small models weight the final instruction heaviest and routinely ignore a
reply-language rule buried earlier (live-bench observed). Records
`MetricAgentChatRequestsTotal{outcome}` and `MetricAgentLLMRequestsTotal{purpose:"chat"}`.

## Constructor

`NewChatService(notif, fleet, digests *DigestService, connected func(nodeID string) bool, llmMgr
*LLMManager, metrics, logf)` — `connected` is the control channel's liveness oracle
(`ControlServer.IsConnected`), nil-safe.

## Notes

- `ErrChatBadRequest` marks client errors (empty/oversized question) so `apis/agent.go` can map
  them to `400` before any SSE stream starts.
- See `apis/agent.go.md` for the SSE wire format (`event: meta/delta/done/error`) and the
  `?stream=false` JSON fallback used by tests/curl.
