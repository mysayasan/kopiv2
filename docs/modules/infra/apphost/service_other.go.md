# Module: infra/apphost/service_other.go

## Purpose

Non-Windows (`//go:build !windows`) counterpart to `service_windows.go`, so `run.go` can call the platform-dispatch functions unconditionally on every OS.

## Responsibilities

- `runWithPlatform(app)`: calls `runApp(app)` directly — there is no service-manager integration to route through on Linux/macOS (those platforms use systemd/launchd/Docker restart policies instead; see `deploy/README.md`).
- `platformShutdownChan()`: returns a `nil` channel, so the `case <-svcStop` in `run.go`'s shutdown `select` never fires on these platforms.

## Notes

- Keeps `run.go` platform-agnostic: it always calls `runWithPlatform`/`platformShutdownChan`, and the build-tagged file pair supplies the right implementation.
