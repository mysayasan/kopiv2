# Module: apps/mymatasan/apis/system.go

## Purpose

HTTP surface for system-level operations: the **Secure Wipe & Reset** factory reset, a manual process restart, host clock/timezone reporting, the in-app **self-update** check/apply flow, and the admin-side **encryption-at-rest recovery escrow** (export/verify a passphrase-protected copy of the master key).

## Responsibilities

- `NewSystemApi(router, reset, restart, update, keystore)` — registers under `/system`:
  - `POST /system/reset` — start a factory reset in the background, returning the initial progress.
  - `GET /system/reset/state` — `{ "allowed": bool }`, so the UI hides the button unless reset is permitted.
  - `GET /system/reset/progress` — the current in-memory `ResetProgress`.
  - `POST /system/restart` — relaunch the process (via `restarter.Restart`) so startup-only config changes (a new ffmpeg path, switched Python, freshly installed GPU deps) take effect; responds first, then restarts after a short delay so the client can start polling for the server to come back.
  - `GET /system/time` — read-only host timezone/clock (`timezone`, `abbrev`, `offsetSec`, `now`, `unix`) so the setup wizard can let the user confirm timestamps will be correct.
  - `GET /system/update` — cached self-update status (current/latest version, `updateAvailable`, `canSelfUpdate`, `managed`, and any in-flight apply state) from `services.UpdateService.Status()`.
  - `POST /system/update/check` — force an immediate GitHub Releases check (`services.UpdateService.CheckNow`).
  - `POST /system/update/apply` — start the background download + verify + swap + restart. Admin-gated. An optional `{"version": "1.128.0"}` body (4 KiB cap, `DisallowUnknownFields`, a missing/empty body treated as the un-pinned form rather than a bad request) pins the release via `services.UpdateService.StartUpdateTo`, which REFUSES to downgrade; an empty body keeps the operator's own "update now" behaviour (`StartUpdate`, whatever is newest). This is how a `myseliasan` fleet rollout drives one node's self-update — see `apps/myseliasan/services/fleet_rollout.go.md` (W2-5, F-07).
  - `GET /system/recovery/state` — `{ "enabled": bool, "protector": string, "hostBound": bool }`: whether encryption-at-rest is on, which `KeyProtector` currently wraps the key, and whether that protector is host-bound (DPAPI/systemd-creds) — so the UI can nudge the operator to export a recovery escrow before a hardware failure makes that impossible.
  - `POST /system/recovery/export` — body `{ passphrase }` (min 8 chars); wraps the current master key with it (`KeyStore.ExportEscrow`) and returns `{ filename, keyBase64 }` for the browser to save as a `.atrestkey` file.
  - `POST /system/recovery/verify` — body `{ passphrase, keyBase64 }`; unwraps a previously exported escrow and reports `{ valid, matchesCurrent }` (or an `error` string) without mutating anything, so an operator can confirm a saved recovery file actually still works.
- `escrowKeyStore` is a narrow interface (`Enabled`, `Protector`, `ExportEscrow`, `VerifyEscrow`) so this package doesn't import `infra/atrest` directly and stays testable; `keystore` may be `nil` when encryption-at-rest is disabled, in which case the recovery endpoints respond with a bad-request "encryption-at-rest is not enabled" rather than erroring.
- `NewResetGate(isResetting func() bool) func(http.Handler) http.Handler` — a middleware that returns `503 {"status":"failed","message":"factory reset in progress","code":"reset_in_progress"}` (with `Retry-After: 5`) for any request while `isResetting()` is true, except requests under `/system/reset` (so the reset's own progress/state endpoints and the SPA's reset overlay keep working). Wired **before** the auth middleware in `app.go` so a request never touches the DB pool the reset has already closed; `isResetting` is a closure reading `SystemResetService.InProgress()` (the service is constructed after this middleware, so it's read live per request rather than captured at wiring time).

## Notes

- Registered on the protected (admin-for-writes) router, so the reset/restart/update-apply/recovery-export/recovery-verify `POST`s require an admin; `SystemResetService` additionally refuses reset unless `bootstrap.allowReset` is true, and `UpdateService` additionally refuses apply unless `canSelfUpdate()` (portable/installer install with a writable home dir, no newer version already applying), and — for `StartUpdate` — a newer version is cached, or — for a pinned `StartUpdateTo` — the requested version is a real, published, newer-than-current release (a downgrade is refused with an explanation rather than attempted).
- The handlers are thin: all wiping/shredding/restart logic lives in `services.SystemResetService`, all release-check/download/verify/swap logic lives in `services.UpdateService`, and all key-wrap/unwrap logic lives in `infra/atrest.KeyStore`. `startReset` and `updateApply` both return immediately (the work proceeds asynchronously); the client polls `/reset/progress` or `/system/update`, then `/health` once the server restarts.
- `update` is `nil`-tolerant: each update handler returns `ErrInternalServerError` ("updater unavailable") rather than panicking if constructed without one.
- These are the **admin, post-login** recovery endpoints (export/verify while the app is running normally). The separate PUBLIC pre-login recovery flow — used when the key is missing entirely — lives in `apis/recovery_gate.go` (`GET/POST /system/recovery/gate`, `/system/recovery/unlock`), mounted only while the app is in recovery mode.
