# Module: apps/mymatasan/apis/system.go

## Purpose

HTTP surface for destructive system-level operations — currently the **Secure Wipe & Reset** factory reset.

## Responsibilities

- `NewSystemApi(router, reset)` — registers under `/system`:
  - `POST /system/reset` — start a factory reset in the background, returning the initial progress.
  - `GET /system/reset/state` — `{ "allowed": bool }`, so the UI hides the button unless reset is permitted.
  - `GET /system/reset/progress` — the current in-memory `ResetProgress`.

## Notes

- Registered on the protected (admin-for-writes) router, so the `POST` requires an admin; the `SystemResetService` additionally refuses unless `bootstrap.allowReset` is true.
- The handlers are thin: all wiping, shredding, and restart logic lives in `services.SystemResetService`. `startReset` returns immediately (the wipe + restart proceed asynchronously); the client polls `/reset/progress`, then `/health` once the server restarts.
