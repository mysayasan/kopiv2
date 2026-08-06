# Module: apps/myseliasan/services/llm_manager.go

## Purpose

`LLMManager` is the one façade the digest and chat layers see for the optional language layer. It
owns the "which client, if any" decision so `DigestService`/`ChatService` stay mode-agnostic:

- mode `"off"` (default) → `Enabled()` false, `Client()` errors `ErrLLMDisabled`.
- mode `"external"` → a client for the operator's own OpenAI-compatible endpoint (any
  `llama-server`/Ollama/vLLM/LM Studio instance).
- mode `"sidecar"` → a client for the supervised `llama-server` child process
  (`llm_sidecar.go.md`), but **only** while it reports `StateReady`; otherwise `Client()` errors
  `ErrLLMNotReady`.

Nothing here retries or blocks beyond the configured timeout — callers degrade deterministically
when `Client()` errors or a completion fails, never by waiting longer.

## `AgentLLMStatus`

`{Mode, Ready, SidecarState, LastError, Model, Endpoint}` — the UI-facing summary
(`GET /api/agent/status`). `Endpoint` is **host-only** for external mode (`hostOnly`, strips
path/query/credentials from the display value) and empty for sidecar mode (loopback is an
implementation detail, not something to leak).

## Methods

- `Mode()` — normalized configured mode, `"off"` when unset/blank.
- `Enabled()` — external: non-empty endpoint; sidecar: `sidecar.Status() == StateReady`.
- `Client() (*llm.Client, error)` — builds an `infra/llm.Client` for the active mode (external:
  `llm.New(endpoint, apiKey, model, timeout)`; sidecar: `llm.New(sidecar.Endpoint(), "",
  ModelName(), timeout)`, no API key — loopback needs none).
- `MaxTokens()` — configured cap, default 768.
- `ModelName()` — the configured name, else (sidecar only) the model file's base name with its
  extension stripped — `llama-server` ignores the request's `model` field, but logs and digests
  still need to record what actually answered.
- `Status() AgentLLMStatus` — resolves `Ready`/`SidecarState`/`LastError`/`Endpoint` per mode.
- `Test(ctx, endpoint, apiKey, model) error` — probes the **submitted** values (not the saved
  config), the same "verify before Save" contract as `TestCache`
  (`services/settings.go.md`). A blank submitted `endpoint` tests the **active** configuration
  instead (covers testing a running sidecar); a blank submitted `apiKey` falls back to the stored
  one, mirroring the settings editor's keep-if-blank semantics so a saved key can be re-tested
  without re-typing it.

## Notes

- `ErrLLMDisabled` vs. `ErrLLMNotReady` are the two typed errors `apis/agent.go`'s chat handler
  maps to distinct HTTP statuses (`409` vs `503`) — "turned off" and "starting up" are different
  operator-facing situations.
- Constructed once in `app.go`'s `RegisterAppRoutes` (`services.NewLLMManager(agentCfg.LLM,
  llmSidecar)`) and threaded into `DigestService`, `ChatService`, and `apis.NewAgentApi`.
