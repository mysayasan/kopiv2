# Module: apps/mymatasan/apis/recovery_gate.go

## Purpose

Serves the PUBLIC endpoints used only while the app is in **recovery mode**:
encryption-at-rest is on, the master key is missing, but a key existed here before (so the
app must not silently mint a new one — see `infra/atrest/startup.go`). These are the only
routes mounted in that mode; normal services and login never start, so the browser can reach
nothing but the gate until the key is restored.

## Responsibilities

- `NewRecoveryGateApi(router, keyPath, cfg, restart, keyId)` — registers under `/system/recovery` (mounted directly, not behind the protected/admin router, since login itself is unavailable in this mode):
  - `GET /system/recovery/gate` — `{ "pending": true, "keyId": "..." }`. The SPA probes this before showing the login form; in normal mode the route doesn't exist (404) so the SPA falls through to login as usual. `keyId` is the non-secret install identifier (from the init marker) shown so an operator can confirm they're uploading the right recovery file.
  - `POST /system/recovery/unlock` — body `{ passphrase, keyBase64 }` (the exported `.atrestkey` recovery file, base64-encoded). Calls `atrest.RestoreFromEscrow(keyPath, data, passphrase, cfg)`; on success responds `{ "restarting": true }` and restarts the process (via `restarter.Restart`) after a short delay so the client can start polling for the server to come back up with the key in place.

## Notes

- `unlock` is rate-limited to one attempt per 2 seconds per process (in-memory `lastTry`, not per-IP) since recovery is a rare, deliberate action, not a hot path needing per-source throttling.
- A wrong passphrase or a file that isn't a passphrase recovery escrow is a client error (`ErrBadRequest`) surfaced from `RestoreFromEscrow`/`decodeKeyFile`, never a partial key write.
- `restarter` is the same seam `apis/system.go` uses for `/system/restart`; it may be `nil` in tests, in which case the endpoint responds but never actually restarts.
- Wired from `app.go`: `RegisterAppRoutes` resolves the key via `atrest.OpenForStartup` before building any other service; on `atrest.ModeRecoveryPending` it calls `NewRecoveryGateApi` and returns immediately, skipping the rest of route registration entirely for that boot.
