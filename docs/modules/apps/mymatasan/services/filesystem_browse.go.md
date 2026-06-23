# Module: apps/mymatasan/services/filesystem_browse.go

## Purpose

Backs the **server-side file picker** used in Settings → Runtime → Decoder to choose the ffmpeg binary (and reusable for future path pickers). Lists one directory level at a time, confined to a whitelist of allowed roots, so the browser UI can navigate the host filesystem without exposing it wholesale.

## Responsibilities

- `BrowseDirectory(dir string, extraRoots []string) (FsBrowseResult, error)` — returns the immediate entries of `dir`: directories first, dotfiles hidden, **names only (never file contents)**. An empty `dir`, a non-existent/non-absolute seed (e.g. the default `"ffmpeg"` resolved from `PATH`), or a path outside the whitelist all fall back to listing the allowed roots rather than erroring. A file path is treated as its containing directory.
- `FsBrowseResult` (`Path`, `Parent`, `Separator`, `Entries[]`) and `FsEntry` (`Name`, `Path`, `Dir`) — the JSON shape returned by `GET /api/settings/fs/browse`.
- `allowedBrowseRoots(extra []string)` — builds the validated, de-duplicated whitelist: app working dir + `bin/` (where the in-app installer drops ffmpeg), the user home, OS-specific common install locations, then any site-specific `extra` roots from `decoder.browseRoots`. Only existing directories are included.
- `withinAllowed` / `allowedParent` — enforce the sandbox: a path is browsable only if it equals or is nested under a root, and navigating "up" stops at a root (returns `""` → roots listing) instead of escaping. Comparison is case-insensitive on Windows.

## OS-specific roots

- **Windows**: `C:\ffmpeg`, `C:\ffmpeg\bin`, `%ProgramFiles%`, `%ProgramFiles(x86)%`, `%ProgramData%`, `%LOCALAPPDATA%`.
- **macOS** (`darwin`): `/usr/bin`, `/usr/local/bin` (Intel Homebrew), `/opt/homebrew` (Apple Silicon Homebrew), `/opt/local` (MacPorts), `/Applications`.
- **Linux / other unix**: `/usr/bin`, `/usr/local/bin`, `/opt`, `/snap/bin`, `/bin`.

Cross-platform (always): app working directory, app `bin/`, user home, plus `decoder.browseRoots`.

## Notes

- Exposed by `apis/settings.go` (`GET /api/settings/fs/browse`); the handler is **admin-only** and read-only.
- The whitelist is a convenience/limiting surface, not the sole security boundary — the admin gate and "names only" listing are. A user can still type any absolute ffmpeg path directly into the field; it is validated by the decoder/installer at use time.
