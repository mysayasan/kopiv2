# Module: infra/procutil

## Purpose

Suppress stray console windows from child processes on Windows. A console
application spawned by a parent that has **no console of its own** — which is the
case after a detached self-restart, when the app runs as a Windows service, or from
certain launchers — gets a brand-new console window allocated by Windows on every
spawn. A per-camera stream/recording pipeline that retries a failing ffmpeg then turns
that into an endless storm of "DOS" windows. This was the root trigger behind "nonstop
command windows after restoring a backup" (restored camera URLs that don't resolve on
the new host retry-loop their ffmpeg).

## Responsibilities

- `HideWindow(cmd *exec.Cmd)` — sets `SysProcAttr.HideWindow = true` and OR-s
  `CREATE_NO_WINDOW` into `CreationFlags` on **Windows** (`hidewindow_windows.go`),
  preserving any flags already set; a **no-op** on every other OS
  (`hidewindow_other.go`), where child stdout/stderr are captured via pipes rather than
  a terminal so there is no window to hide.

## Wiring

Call it on every `exec.Cmd` before `Start`/`Run`/`Output`/`CombinedOutput`. Applied to
all ffmpeg/ffprobe/python/nvidia-smi/tar/powershell spawns across `infra/recording`,
`infra/stream`, `infra/rtsp`, `infra/vision`, `infra/externaltools`, and the
`apps/mymatasan/services` installers/training/GPU-detect helpers.

## Notes

- Distinct from the app's own self-relaunch, which uses `CREATE_NO_WINDOW` directly in
  `infra/apphost/relaunch_windows.go` (see `apphost/run.go.md`).
- `CREATE_NO_WINDOW` does not suppress captured output — the app reads child
  stdout/stderr through pipes, not a console.
