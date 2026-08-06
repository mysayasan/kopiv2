# Module: apps/myseliasan/services/llm_install.go

## Purpose

`LLMInstaller` acquires the sidecar's two artifacts — the `llama-server` release archive and a
GGUF model — by either of two routes:

- **Download** (internet installs): fetches the **pinned** release/model
  (`llm_catalog.go.md`) and refuses anything whose SHA-256 doesn't match the pinned constants —
  a compromised CDN or a repointed release cannot slip a different binary onto an operator's
  control plane. Gated twice: the `agent.allowDownloads` config flag, and the
  `MYSELIASAN_AI_DOWNLOADS=off` environment hard lock (air-gap posture — **the same contract as
  the basemap downloader**, `apis/basemap.go.md`; this is myseliasan's second deliberate internet
  feature).
- **Import** (air-gapped installs): the operator points the server-side file picker
  (`services/filesystem_browse.go.md`) at an archive/model they carried in. Imports can be **any**
  build or model, so they are not checked against the pins — instead the computed SHA-256 is
  written to the install log and the audit trail, so what was imported is on the record even
  though it wasn't verified against anything.

Downloads land as `<dest>.part` and are renamed into place only after the checksum passes, so a
torn download can never be mistaken for a real artifact.

## `DownloadsAllowed()` / `DownloadSupported()`

`DownloadsAllowed()` checks `MYSELIASAN_AI_DOWNLOADS` (`off`/`0`/`false`/`no` hard-locks it off
regardless of config) then the live `agent.allowDownloads` config getter.
`DownloadSupported()` reports whether this OS/arch has a pinned release asset
(`llamaServerDownload`) — platforms with none (anything but windows/amd64, linux/amd64) get the
import path only, which works everywhere llama.cpp itself builds.

## Start*/Import* entry points

- `StartBinaryDownload(ctx)` — background download; errors that prevent **starting** are returned
  directly, progress/failure after that is polled via `Status()`.
- `StartModelDownload(ctx, tier string)` — `tier` is `""`/`"default"` (Qwen2.5-1.5B) or `"large"`
  (Qwen2.5-7B, `llm_catalog.go.md`); any other value errors immediately without starting anything.
  `apis/agent.go`'s `POST /api/agent/llm/install/model` passes the request body's `{tier}` through
  unchanged.
- `ImportBinaryArchive(ctx, path)` / `ImportModel(ctx, path)` — synchronous (local extraction/copy
  is fast); return the installed path.
- `Status() map[string]LLMInstallState` — both artifacts' `{Running, Status, Log, Path}`, polled
  by the settings UI roughly every 1.5s while an install runs.

## `publishModel` and the active-model marker

`publishModel(st, path)` — called after every successful download/import — now does two things,
not one: it calls `sidecar.SetPaths("", path)` as before, **and** it writes `path`'s base name
into `<llmDir>/models/active.txt` (`writeActiveModelMarker`, `services/llm_sidecar.go.md`). The
marker is what makes the operator's tier choice (default vs. large, or an imported model) survive
a restart — without it, `resolveSidecarModel` would fall back to the pinned default file on next
boot even though the operator just installed something else. A marker-write failure only logs a
warning to the install log; it never fails the install itself (the sidecar is already pointed at
the right file for the current process).

## Install mechanics

- `installBinaryArchive` extracts into `<llmDir>/bin`, locates `llama-server` (`findFileUnder`,
  since the Windows archive is flat but the Linux one nests under `llama-<tag>/`), chmods it
  executable on non-Windows, probes it (`externaltools.Probe`, `--version`), then calls
  `sidecar.SetPaths(exe, "")`. The sidecar is **paused around the whole extraction** (`Pause`/
  `defer Resume`) because a running `llama-server` holds file locks on Windows that would fail
  every overwrite.
- `downloadTo` streams to `<dest>.part` while hashing, verifies SHA-256, renames into place; a
  size or checksum mismatch removes the `.part` file and refuses the artifact.
- `extractLLMArchive` dispatches `.zip` (`extractLLMZip`) vs. `.tar.gz`/`.tgz`
  (`extractLLMTarGz`, which also recreates the ~10 versioned `.so` symlinks the Linux release
  ships). Both guard against a zip-slip/tar-slip path escaping `destDir`.
- Size caps: `maxBinaryArchiveBytes` 512MiB (b10289 archives are ~18MB), `maxModelBytes` 8GiB (q4
  models importable up to ~8GB).

## Notes

- `SetOnResult(fn func(artifact, method string, ok bool))` wires `MetricAgentInstallTotal
  {artifact, method, outcome}` (`app.go`).
- `firstLine`/`fileSHA256`/`copyFileAtomic`/`samePath` are small shared helpers; `copyFileAtomic`
  is used for `ImportModel` so a crash mid-copy never leaves a half-written model file at the
  final path.
- Constructed in `app.go`: `services.NewLLMInstaller(llmDir, llmSidecar, func() bool {
  return agentCfg.AllowDownloads == nil || *agentCfg.AllowDownloads }, logf)` — `llmDir` is
  `<dataDir>/llm`, the same directory the factory reset's `CollectDataPaths` erases (see
  `app/app.go.md`).
