# Module: apps/mymatasan/apis/system.go

## Purpose

HTTP surface for system-level operations: the **Secure Wipe & Reset** factory reset, a manual process restart, host clock/timezone reporting, and (new) the in-app **self-update** check/apply flow.

## Responsibilities

- `NewSystemApi(router, reset, restart, update)` — registers under `/system`:
  - `POST /system/reset` — start a factory reset in the background, returning the initial progress.
  - `GET /system/reset/state` — `{ "allowed": bool }`, so the UI hides the button unless reset is permitted.
  - `GET /system/reset/progress` — the current in-memory `ResetProgress`.
  - `POST /system/restart` — relaunch the process (via `restarter.Restart`) so startup-only config changes (a new ffmpeg path, switched Python, freshly installed GPU deps) take effect; responds first, then restarts after a short delay so the client can start polling for the server to come back.
  - `GET /system/time` — read-only host timezone/clock (`timezone`, `abbrev`, `offsetSec`, `now`, `unix`) so the setup wizard can let the user confirm timestamps will be correct.
  - `GET /system/update` — cached self-update status (current/latest version, `updateAvailable`, `canSelfUpdate`, `managed`, and any in-flight apply state) from `services.UpdateService.Status()`.
  - `POST /system/update/check` — force an immediate GitHub Releases check (`services.UpdateService.CheckNow`).
  - `POST /system/update/apply` — start the background download + verify + swap + restart (`services.UpdateService.StartUpdate`). Admin-gated.

## Notes

- Registered on the protected (admin-for-writes) router, so the reset/restart/update-apply `POST`s require an admin; `SystemResetService` additionally refuses reset unless `bootstrap.allowReset` is true, and `UpdateService` additionally refuses apply unless `canSelfUpdate()` (portable/installer install with a writable home dir, no newer version already applying) and a newer version is cached.
- The handlers are thin: all wiping/shredding/restart logic lives in `services.SystemResetService`, and all release-check/download/verify/swap logic lives in `services.UpdateService`. `startReset` and `updateApply` both return immediately (the work proceeds asynchronously); the client polls `/reset/progress` or `/system/update`, then `/health` once the server restarts.
- `update` is `nil`-tolerant: each update handler returns `ErrInternalServerError` ("updater unavailable") rather than panicking if constructed without one.
