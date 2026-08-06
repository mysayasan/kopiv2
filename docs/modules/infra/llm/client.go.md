# Module: infra/llm/client.go

## Purpose

A minimal, stdlib-only OpenAI-compatible chat-completions client (`package llm`). It exists so
the suite can talk to **any** local inference server that speaks the de-facto
`/v1/chat/completions` wire format — llama.cpp's `llama-server`, Ollama, vLLM, LM Studio — without
taking an SDK dependency. First (and currently only) consumer: `myseliasan`'s fleet AI agent
(`apps/myseliasan/services/llm_manager.go.md`).

## Design constraints

1. The caller is an air-gapped control plane: the client never decides *where* to connect (the
   base URL is operator configuration) and never retries on its own — the features above it
   (digest polish, ask-the-fleet chat) degrade deterministically instead of the client masking a
   failure with a retry loop.
2. An LLM must never wedge the app: every request carries the caller's `ctx`, response bodies are
   size-capped (`maxResponseBytes` non-streaming, `maxStreamLineBytes` per SSE line), and a
   malformed stream chunk is skipped rather than fatal.
3. Small models on CPU are slow, so streaming is first-class: `ChatStream` surfaces deltas the
   moment the server flushes them, rather than buffering a whole completion before the caller sees
   anything.

## Types

- `Message{Role, Content}` — one chat turn (`"system"`/`"user"`/`"assistant"`).
- `ChatRequest{Model, Messages, Temperature, MaxTokens}` — `Model` overrides the client's default
  when set; `MaxTokens` 0 lets the server decide.
- `Usage{PromptTokens, CompletionTokens}` — zeroed when the server doesn't report it (some proxies
  omit it; `llama-server` includes it).
- `ChatResult{Content, Usage}` — a completed (or partially-streamed-then-errored) answer.
- `Client{BaseURL, APIKey, Model, HTTP}` — one endpoint. Construct with `New`, never the struct
  literal, so `BaseURL` is always normalized.

## Constructor

`New(baseURL, apiKey, model string, timeout time.Duration) *Client` — `timeout <= 0` defaults to
60s. `NormalizeBaseURL` trims trailing slashes and appends `/v1` when absent, so an operator can
paste `http://host:11434`, `http://host:11434/`, or `http://host:11434/v1` interchangeably; every
server this client targets mounts the OpenAI surface at `/v1`.

## Methods

- `Chat(ctx, req) (ChatResult, error)` — one non-streaming completion. Reads the body capped at
  `maxResponseBytes`, rejects a response with zero `choices` or a wire-level `error` field.
- `ChatStream(ctx, req, onDelta func(delta string) error) (ChatResult, error)` — streams
  `data: {chunk}` SSE lines (terminated by `data: [DONE]`, but a server that just closes the
  connection instead is tolerated). A malformed chunk line is skipped, not fatal. Returning an
  error from `onDelta` aborts the request and that error is returned. If `ctx` is cancelled
  mid-stream, the accumulated text so far is returned alongside `ctx.Err()` so a caller (the
  digest polish call) can keep partial output on a timeout rather than discarding it.
- `Probe(ctx) error` — cheaply verifies the endpoint is alive: `GET {base}/models`, falling back
  to a one-token completion for servers that don't implement the models listing (llama-server does
  not). Bounded by its own `probeTimeout` (8s) regardless of the client's configured request
  timeout, since a probe is meant to be a quick "is anything there" check, not a full-length call.
  A `401`/`403` is reported distinctly ("endpoint rejected the API key") so the settings UI's
  **Test** button can tell a wrong key apart from an unreachable server.

## Notes

- `post` builds the wire request (`wireRequest`/`wireResponse` — the narrow schema subset this
  client reads/writes), sets `Authorization: Bearer <APIKey>` when `APIKey` is non-empty, and
  treats any `>= 400` status as an error carrying a truncated (`errBodyPreview`, 300 bytes) body
  preview.
- No SDK, no reflection-based JSON schema — everything here is `encoding/json` over the plain
  request/response shapes actually used. Extending it to another provider quirk (e.g. a field only
  some servers send) means widening `wireResponse`, not adding a dependency.
- Consumers: `apps/myseliasan/services/llm_manager.go` builds a `*Client` for whichever mode
  (external endpoint or the supervised sidecar) is active; `services/agent_digest.go`'s `polish`
  and `services/agent_chat.go`'s `Stream` are the only two call sites that actually issue a
  completion.
