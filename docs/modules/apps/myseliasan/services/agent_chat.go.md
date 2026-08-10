# Module: apps/myseliasan/services/agent_chat.go

## Purpose

"Ask the fleet" — the chat side of the AI agent (`ChatService`).

## Design: route → fetch → summarize, deliberately not an agentic tool loop

The models this must work with are 1–2B parameter CPU models, and those cannot reliably drive
multi-step tool calling; what they **can** do is answer a question over a compact document. So the
server assembles one grounding bundle (`BuildGrounding`) — fleet status, windowed stats, per-source
anomalies, the latest digest's findings, recent high-severity events — and the model gets exactly
one completion over it, under a system prompt that forbids answering from anything else.

**Grounding is central-first, with a single-node exception**: everything comes from the control
plane's own tables, EXCEPT that a question naming exactly one adopted node also pulls that node's
own recent events over the control tunnel (`chatNodeSender`, below). Fanning out to a whole fleet
is still deliberately not done — that could stall a reply behind a 30s-per-node timeout — but one
node, under a 5s cap, is safe: an offline/failed node yields `Unreachable: true` (an honest fact
the model can state) rather than blocking the answer.

## Timestamps in the grounding bundle are always pre-formatted strings

`GroundingBundle.GeneratedAt`, `GroundWindow.From`/`To`, `GroundNode.LastSeenAt`, and
`GroundNotif.CreatedAt` are `"2006-01-02 15:04"` strings (`groundTime`), never raw unix integers.
A live bench had a 7B model "convert" a raw epoch and report a 2026-07-26 sighting as
"2023-12-01" — small models cheerfully mangle timestamp arithmetic, and a confidently wrong date
in a security answer is worse than no date at all. `groundTime(0)` (or any `<= 0`) renders `""`
("never"/unknown), not `"1970-01-01"`.

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
- **NodeDetail** (new) — the single-node drill-down (`*GroundNodeDetail{NodeId, Name,
  Unreachable, Recent}`), populated only when `matchNodeInQuestion` (below) finds exactly one
  adopted node named in the question and `c.sender` is wired. Its own `Recent` events carry
  `Id: 0` (node-local rows have no central feed id, so the model is steered to cite the node
  rather than a misleading `[notif N]`) and `Source: "node:<id>"`.

**Size enforcement**: `chatMaxBundleBytes`=8KiB (≈2.3k tokens at ~4 bytes/token — fits an 8k
context with prompt+history+completion room to spare) is enforced by dropping sections in reverse
priority — `recentEvents` → `anomalies` → `latestDigestFindings` → trim `fleet` to 10 — recording
each drop in `Truncated` so the model (and the UI) can say "I only see part of the data."

## The second grounding source: the manual

`ChatService.docs` (`*DocsService`, nil-safe — see `services/agent_docs.go.md`) retrieves sections
from the built-in manuals of **both** myseliasan and mymatasan on every question. Retrieval is
local, deterministic and sub-millisecond, so there is no mode to configure and nothing to fail: it
simply returns nothing when the manual has no answer, which is the common case for a pure
fleet-status question.

The excerpt block is appended to the system message after the bundle. **Combined ceiling** is the
fleet bundle's 8 KiB plus the excerpt block's 2.5 KiB ≈ 2.7k tokens, which still leaves an 8k
context room for the prompt, four history turns and a 768-token completion — so `BuildGrounding`'s
own cap is unchanged.

## `SystemPrompt(lang, withDocs bool)`

The strict-grounding instruction: answer only from FLEET DATA; cite `[notif <id>]`/`[node <id>]`
when referencing a record; every number must appear verbatim in FLEET DATA — never invented;
mention when a section was truncated; ≤8 sentences; reply in the requested language.

`withDocs` swaps in the two-source rules, and is **false when retrieval found nothing** — an
unused rule about a section that is not in the prompt is one more thing for a 1.5B model to
misapply. The added rule that matters states the separation before anything else: FLEET DATA is
what *this* fleet is doing now; MANUAL EXCERPTS are product documentation, possibly for a
different product in the suite; never state something from an excerpt as an observation about this
fleet, and never answer a current-state question from an excerpt. This is the specific way a
documentation-grounded answer goes wrong — the manual is written in the present tense about the
product, and a model that forgets what it is reading reports it as a fact about the installation.

## `Stream(ctx, req, emit) (llm.ChatResult, []DocExcerpt, error)`

`Validate` → `llm.Client()` (typed `ErrLLMDisabled`/`ErrLLMNotReady` on failure, mapped to
409/503 by `apis/agent.go`) → `BuildGrounding` → `docs.Ground` → one `client.ChatStream` call with
the system prompt (grounding bundle inlined as `FLEET DATA:`, excerpts appended as `MANUAL
EXCERPTS`) + history + the question. For a non-English `Lang`, the language instruction is
**repeated right next to the question** (not just in the system prompt) — small models weight the
final instruction heaviest and routinely ignore a reply-language rule buried earlier (live-bench
observed). Records `MetricAgentChatRequestsTotal{outcome}` and
`MetricAgentLLMRequestsTotal{purpose:"chat"}`.

The returned `[]DocExcerpt` is the excerpts offered to the model, each flagged with whether the
finished answer actually cited it (`MarkCited`). It is a **return value rather than a callback**
because that flag cannot exist until the answer does — including a partial answer cut short by a
timeout, where what it cited before it stopped is still what it cited.

## Single-node drill-down

`matchNodeInQuestion(question, nodes) *entities.ManagedNode` — case-insensitive substring match of
the question against every adopted node's `Name` and `NodeId`; names/ids shorter than 3 characters
never match (too many false hits on short ids), and when both a name and an id match, the
**longest** match wins (so `"gate camera 2"` beats a shorter `"gate"` match on a different node).
Deterministic, no ambiguity resolution beyond "longest wins" — a genuinely ambiguous question (two
nodes both named things the question contains) picks whichever match is longer, not both.

`fetchNodeDetail(ctx, node) *GroundNodeDetail` — when `c.connected(node.NodeId)` is false, returns
`{Unreachable: true}` immediately, no tunnel call. Otherwise sends `GET
/api/notifications?limit=25` over the control tunnel (`chatNodeSender.SendRequest`, `Role:
"viewer"`, `Actor: "control-plane:agent-chat"`) under `chatNodeFetchTimeout` (5s — deliberately far
under the tunnel's own 30s default, since a slow node must cost only the drill-down section, never
the whole answer). A non-2xx status, a transport error, or an unparseable body all degrade to
`Unreachable: true` — never an error surfaced to the caller. The response envelope is tolerated in
either shape a node might return (`{result:{items}}` or `{data:{result:{items}}}`), capped at the
first 10 items, titles capped `chatTitleCap`.

`chatNodeSender` is the one-method sliver of the control server this needs (`SendRequest`),
satisfied by `*services.ControlServer` itself — `app.go` passes `controlServer` directly as the
`sender` constructor argument.

## Constructor

`NewChatService(notif, fleet, digests *DigestService, connected func(nodeID string) bool, sender
chatNodeSender, docs *DocsService, llmMgr *LLMManager, metrics, logf)` — `connected` is the control
channel's liveness oracle (`ControlServer.IsConnected`), nil-safe. `sender` nil disables the
single-node drill-down entirely (grounding stays central-only); `app.go` wires it to the same
`*services.ControlServer` as `connected`. `docs` is the manual retriever (see above); nil-safe —
`docs.Ground` on a nil `*DocsService` simply returns no excerpts.

## Notes

- `ErrChatBadRequest` marks client errors (empty/oversized question) so `apis/agent.go` can map
  them to `400` before any SSE stream starts.
- See `apis/agent.go.md` for the SSE wire format (`event: meta/delta/done/error`) and the
  `?stream=false` JSON fallback used by tests/curl.
