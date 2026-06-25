# Module: apps/mymatasan/apis/pairing.go

## Purpose

Exposes the node-side pairing HTTP endpoints. Public (unauthenticated) routes are called by the control plane and carry their own cryptographic authentication; protected routes are behind the local session + admin middleware and are used by the node operator.

## Endpoints

### Public routes (mounted before the session catch-all)

| Method | Path | Notes |
|---|---|---|
| POST | `/api/pairing/adopt` | Fleet-key assertion + claim code bind. Returns `AdoptResult` (nodeId, name, token) on success; 409 if already paired; 401 if assertion fails; 400 if claim code is wrong/expired. |
| POST | `/api/pairing/release` | Pairing-token release. Body: `{"token": "..."}`. Returns `{"paired":false}`; 401 on bad token. |

### Protected routes (local session + admin required)

| Method | Path | Notes |
|---|---|---|
| GET | `/api/pairing/status` | Returns `PairingStatus` (nodeId, name, paired, parentId/Name/BaseUrl, pairedAt, fleetKeySet, discoverable, claimCodeActive). |
| POST | `/api/pairing/claim-code` | Generate a new 8-char base32 claim code (10-min TTL, single-use). Returns `{"code":"...","expiresAt":<unix>}`. |
| POST | `/api/pairing/unpair` | Admin self-drop: clear pairing state and fire a best-effort signed notice to the ex-parent (`/api/nodes/self-dropped`). Returns `{"paired":false}`. |
| PUT | `/api/pairing/fleet-key` | Set the fleet key. Body: `{"key":"..."}`. Minimum 16 characters. Returns `{"fleetKeySet":true}`. |

## Self-drop notice

When an admin calls `POST /api/pairing/unpair`, the handler fires `notifyParentSelfDrop` in a background goroutine. It POSTs a fleet-key-signed payload (`nodeId`, `nonce`, `ts`, `assertion`) to the ex-parent's `/api/nodes/self-dropped`. The call uses `InsecureSkipVerify` (PSK bootstrap) and a 5-second timeout. The node is unpaired regardless of delivery — the notice is courtesy only.

## `onAdopted` Callback

`NewPairingPublicApi` accepts an optional `onAdopted func()` parameter. When provided, the adopt handler calls it (synchronously, after the state is saved) to signal downstream components. In `app.go` this is wired to `enrollmentManager.Kick()` so certificate enrollment begins immediately after a successful adoption, without waiting for the 5-minute reconcile timer.

## Notes

- `NewPairingPublicApi` must be registered before the local-session subrouter so the public paths match first.
- Request bodies are capped at 64 KiB and parsed with `DisallowUnknownFields`.
- The JSON body size limit and field strictness are shared with `decodeJSON` used elsewhere in the `apis` package.
