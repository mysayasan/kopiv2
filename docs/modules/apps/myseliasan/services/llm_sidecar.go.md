# Module: apps/myseliasan/services/llm_sidecar.go

## Purpose

`LLMSidecar` supervises a llama.cpp `llama-server` child process on loopback (default port
49540) — the "managed" LLM mode (`agent.llm.mode == "sidecar"`), for operators who don't want to
run their own inference server.

## The supervision contract (learned the hard way elsewhere in the suite)

- **Every spawn goes through `procutil.HideWindow`** — a console child whose parent has no console
  gets a fresh console window per spawn on Windows, and a crash-restart loop would turn that into
  a window storm.
- **stderr is always a pipe drained by this code, never `os.Stderr`** — under a detached or
  service parent, `os.Stderr` is an invalid handle and the child dies at stdio init before it ever
  logs why.
- A missing binary or model parks the sidecar in `StateOff` with a reason; it must **not** enter
  the restart loop, because restarting cannot conjure files that aren't there.
- Crashes restart with exponential backoff (`sidecarBackoffFloor` 3s → `sidecarBackoffCeiling`
  30s), forever, because the operator may fix the cause (OOM, a replaced model) without touching
  the app.

## `SidecarState`

`off` (not configured, or binary/model missing — see `LastError`) → `starting` (spawned, waiting
for `/health`) → `ready` (`/health` passed; completions can be served) → `failed` (exited or never
became healthy; backoff then retry) — plus `paused` (an installer is replacing files) and
`stopped` (app shutdown).

## Constructor / lifecycle

- `NewLLMSidecar(cfg SidecarConfig, llmDir string, logf) *LLMSidecar` — `cfg.Port`/`CtxSize`
  default to `sidecarDefaultPort`(49540)/`sidecarDefaultCtxSize`(8192) when zero.
- `Start(ctx)` — no-op supervisor when `cfg.Enabled` is false (constructing it unconditionally
  keeps `app.go`'s wiring simple and lets `Status()` answer the UI in every mode); otherwise starts
  `run` under `safego.Supervise(ctx, "myseliasan.llm-sidecar", ...)`. Call once.
- `run` loop: paused → wait for a nudge; missing binary/model → `StateOff` + wait for the
  installer's nudge (not a crash, so no backoff spin); else spawn via `runChildOnce`, which blocks
  until the child exits, then restarts with backoff (reset to the floor if the previous run was
  ever healthy — a model that ran fine for a while deserves a prompt retry, not a full backoff).
- `runChildOnce` spawns `llama-server --model <model> --host 127.0.0.1 --port <port> --ctx-size
  <ctx> [--threads <n>] --no-webui` (`--no-webui`: the sidecar serves exactly one consumer, this
  app, on loopback — the bundled web UI would be an unaudited extra surface). Races `awaitHealthy`
  (polls `GET /health` every `sidecarHealthInterval`=2s up to `sidecarHealthTimeout`=120s — model
  load from a cold page cache on a slow disk is the long pole) against `cmd.Wait()`.

## Pause/Resume

`Pause()` kills the child and holds it down (state `paused`) so an installer can replace files a
running `llama-server` would otherwise keep locked on Windows; `Resume()` lifts the hold and
nudges the supervisor to relaunch. Always paired — `LLMInstaller.installBinaryArchive`/
`publishModel` bracket every file replacement with `Pause`/`defer Resume`.
`apis/agent.go`'s `POST /api/agent/llm/sidecar/restart` calls `Pause()`+`Resume()` back-to-back to
kill a wedged child and nudge an immediate relaunch.

## Path resolution

`ResolvedPaths()`/`resolveSidecarBinary`/`resolveSidecarModel` fall back to the default install
layout under `<llmDir>/bin` (searched recursively — `findFileUnder` — since the Linux release
archive nests everything under a `llama-<tag>/` directory) and `<llmDir>/models` (the pinned
`defaultModelFile`, else the single `.gguf` present — **multiple `.gguf` files with none
configured refuse to guess** rather than picking one). `SetPaths` (called by the installer after a
successful install/import) updates the config and nudges the supervisor, so pointing at a freshly
installed artifact doesn't need a full app restart.

## Notes

- `StderrTail()` returns the child's last `sidecarStderrTailLines`=100 stderr lines — the first
  place to look when state is `failed`; surfaced in the sidecar's `lastErr` on a spawn failure.
- `Restarts()` backs `MetricAgentSidecarRestartsTotal` via `SetOnRestart` (wired in `app.go`) — a
  climbing value is a model that doesn't fit the host (OOM) or a corrupt binary/model file.
- See `services/llm_install.go.md` for how the binary/model actually get onto disk, and
  `services/llm_catalog.go.md` for the pinned download coordinates.
