# Module: apps/myseliasan/services/llm_catalog.go

## Purpose

The pinned LLM sidecar artifacts. **Pinned, not "latest"**: `llm_install.go`'s downloader
verifies SHA-256 against these constants, so a compromised CDN or a repointed release cannot slip
a different binary onto an operator's control plane. Bumping the sidecar version is a deliberate
code change that re-pins all three hashes (download each asset once, `sha256sum` it, update here
— the current values were computed from the artifacts on 2026-08-06).

## Pinned coordinates

- `llamaCppReleaseTag = "b10289"` — the llama.cpp release the sidecar downloads
  (`llamaReleaseBaseURL` = the GitHub releases download URL for that tag).
- `llamaAssetWinAmd64`/`llamaSHAWinAmd64` — the Windows CPU release zip.
- `llamaAssetLinuxAmd64`/`llamaSHALinuxAmd64` — the Linux release `tar.gz` (**not** a zip;
  everything is nested under a `llama-<tag>/` directory with ~10 symlinks — `llm_install.go`'s
  extractor handles both).
- `defaultModelFile`/`defaultModelURL`/`defaultModelSHA` — the **`"default"` tier**,
  **Qwen2.5-1.5B-Instruct Q4_K_M** (~1.1GB), chosen because it's small enough for usable CPU-only
  inference and genuinely multilingual across the suite's four UI languages (en/ms/zh/ar).
  Operators can import any other GGUF instead via the file picker.
- `largeModelFile`/`largeModelURL`/`largeModelSHA` — the **`"large"` tier**,
  **Qwen2.5-7B-Instruct Q4_K_M** (~4.7GB, needs ~6GB free RAM). Noticeably better prose and
  instruction-following than the 1.5B — including replies in the requested non-English language,
  which the 1.5B routinely ignores and answers in English anyway. Sourced from **bartowski's**
  single-file repack rather than the official Qwen 7B GGUF repo, because the official repo ships
  the file **split in two parts**, which the single-file installer/resolver deliberately does not
  handle.
- Size caps for downloads/imports: `maxBinaryArchiveBytes` (512MiB), `maxModelBytes` (8GiB) —
  `io.LimitReader` guards, not exact expected sizes (the 7B tier fits comfortably under the 8GiB
  model cap).

## Functions

- `llamaServerDownload(goos, goarch) (url, sha256, assetName string, err error)` — returns the
  pinned coordinates for `windows/amd64` or `linux/amd64`; any other platform errors, and the UI
  offers the import path only (which works everywhere llama.cpp itself builds — darwin/arm64
  included).
- `llamaServerExeName()` — `llama-server.exe` on Windows, `llama-server` elsewhere.

## Notes

- No macOS/arm64 (Apple Silicon) pinned download exists yet — that platform is import-only.
- See `services/llm_install.go.md` for how these coordinates are turned into a verified on-disk
  artifact, and `services/llm_sidecar.go.md` for how the resulting files are located and run.
