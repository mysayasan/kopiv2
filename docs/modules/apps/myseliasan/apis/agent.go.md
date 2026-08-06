# Module: apps/myseliasan/apis/agent.go

## Purpose

HTTP surface for the fleet AI agent: the deterministic daily digest, "ask the fleet" chat, and
(superadmin-only) management of the optional LLM layer.

## Endpoints

All routes require a myseliasan session + accessrbac middleware (`/agent` subrouter). The SPA's
Roles page (`views/components/rbac_admin.js`) seeds `/api/agent` into `VIEWER_DEFAULT_PATHS`, so a
brand-new role gets read access to the digest by default; the `POST` grant (generate-now +
ask-the-fleet chat, which burn real CPU) is its own curated feature toggle (`ACCESS_FEATURES`'s
`agent` entry), not part of the read default.

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/agent/status` | LLM mode/readiness (`AgentLLMStatus`) + digest schedule state (`AgentDigestStatus`: `enabled`, `localHour`, `windowHours`, `lastRunDate`, plus `weeklyEnabled`/`weekday`/`lastWeeklyRunDate` for the weekly cadence) + download availability (`allowed`, `supported`). Matrix-gated read. |
| `GET` | `/api/agent/digests` | Stored digests, newest-first. `?limit=&offset=` (default 20). Returns `{items: []digestDTO, total}`. |
| `GET` | `/api/agent/digests/latest` | The most recent digest, or `null` when none exist yet. |
| `POST` | `/api/agent/digests/generate` | Generate a digest now, actor = caller. `?kind=weekly` produces the 7-day management-cadence digest (`kind: "weekly"`); anything else is the default 24h operational one (`kind: "manual"`) — `"daily"` is deliberately not accepted here, that kind belongs only to the scheduler. Matrix-gated write. Write deadline extended to 3 minutes (`extendWriteDeadline`) — the narrator is instant but the LLM polish is not. Audited (`agent.digest.generate`). |
| `POST` | `/api/agent/chat` | Ask-the-fleet. Default is a Server-Sent-Events stream (`event: meta/delta/done/error`); `?stream=false` returns one JSON `{answer, usage}` object for tests/curl. Pre-flight failures (bad request, LLM disabled/not-ready) map to `400`/`409`/`503` **before** any stream starts. Write deadline extended to 10 minutes. Audited (`agent.chat`, question truncated to 200 chars). |

Superadmin-only management (self-gated in-handler, like the settings editor):

| Method | Path | Notes |
|---|---|---|
| `POST` | `/api/agent/llm/test` | Probes a submitted `{endpoint, apiKey, model}` before saving it. |
| `POST` | `/api/agent/llm/install/binary` | Starts the pinned llama.cpp release download in the background. |
| `POST` | `/api/agent/llm/install/model` | Starts a pinned model download in the background. Body `{tier: "default"|"large"}` (empty/omitted = `"default"`, the 1.5B model; `"large"` is the 7B quality upgrade — `services/llm_catalog.go.md`). Audit detail records `download:<tier>`. |
| `GET` | `/api/agent/llm/install/status` | Install progress for both artifacts (UI polls ~1.5s during an install). |
| `POST` | `/api/agent/llm/import` | Installs an operator-picked file: `{kind: "binary"|"model", path}`. |
| `POST` | `/api/agent/llm/sidecar/restart` | Pause+Resume — kills a wedged child and nudges an immediate relaunch. |

## SSE chat wire format

`chatHandler` streams `event: meta` (model name + window) once, then `event: delta` per token
batch (`{text}`), then either `event: done` (`{usage, chars}`) or `event: error` (`{code,
message}`). A client disconnect mid-stream (`ctx.Err()` after the browser leaves) is a silent
return, not an error event — nobody is listening. `?stream=false` instead calls
`ChatService.Stream` synchronously and returns one JSON object.

## Authorization

- `auth.Middleware` + `session.Middleware` on the whole `/agent` subrouter.
- `requireSuper` additionally wraps every `llm/*` management route
  (`AccessSessionMidware.IsSuperadmin`) — these values can point the control plane at an arbitrary
  network endpoint or start downloading gigabytes, so they're never granted to a lesser role
  regardless of what the permission matrix says (same pattern as `apis/settings.go.md`).

## Constructor

`NewAgentApi(router, auth, session, digests *services.DigestService, chat *services.ChatService,
llmMgr *services.LLMManager, installer *services.LLMInstaller, sidecar *services.LLMSidecar,
audit services.IAuditService, digestCfg func() AgentDigestStatus)` — `digestCfg` is resolved by
the app wiring (which owns the config and the schedule state), not the API package, so `status`
never has to reach into `app.go`'s internals directly. `AgentDigestStatus` (exported — was the
unexported `agentDigestStatus`, since `app.go` now builds the whole struct itself rather than
returning a tuple the API package wrapped) is `{Enabled, LocalHour, WindowHours, LastRunDate,
WeeklyEnabled, Weekday, LastWeeklyRunDate}`. Registered in `app.go`'s `RegisterAppRoutes` right
after the digest/chat services and the LLM sidecar/installer are built; `digestCfg`'s closure
reads `deps.Config.Agent.Digest` and both persisted runtime-setting rows
(`agent.digest.lastRun`/`agent.digest.lastWeeklyRun`) via `runtimeSettingRepo.GetByUnique`.

## Audit trail

`record` writes a best-effort audit entry (`services.IAuditService.Record`) for
`agent.digest.generate`, `agent.chat`, `agent.llm.test`, `agent.llm.install.binary`,
`agent.llm.install.model`, `agent.llm.import`, `agent.llm.sidecar.restart` — target type
`"agent"`. Chat records only a truncated question, never the model's answer or the grounding
bundle.

## Notes

- Seeded as an `api_endpoint` row in `app.go`'s `Seeders` (`Title: "AI Agent"`, `Path:
  /api/agent`, `AccessTier: AuthOnly`).
- `maxAgentBody` caps every agent `POST` body at 64KiB — a chat request with history is a few KiB;
  nothing here needs more.
- `digestDTO` (`toDigestDTO`) decodes `AgentDigest.FindingsJson` into `[]services.Finding` for the
  response and blanks the raw JSON field, so the SPA never re-parses a string — see
  `entities/agent_digest.go.md`.
- See `services/agent_digest.go.md`, `services/agent_findings.go.md`, `services/agent_chat.go.md`,
  `services/llm_manager.go.md`, `services/llm_sidecar.go.md`, `services/llm_install.go.md` for the
  orchestration this API is a thin HTTP layer over.
