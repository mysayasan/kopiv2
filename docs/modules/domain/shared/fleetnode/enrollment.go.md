# Module: domain/shared/fleetnode/enrollment.go

## Purpose

Manages the node side of the mTLS hardening lifecycle: after adoption it enrolls with the control-plane fleet CA (generating a key + CSR locally, sending only the CSR), stores the issued certificate bundle, runs the mutual-TLS management listener, and renews the certificate before it expires. It reconciles on a timer and on demand via `Kick`.

Moved here from `apps/mymatasan/services/node_enrollment.go` (Tier: fleet extraction), unchanged
in behavior. `mymatasan` keeps compiling unchanged via a same-named alias in
`apps/mymatasan/services/fleetnode.go`; `myiotsan` constructs it directly
(`apps/myiotsan/app/wire_fleet.go`).

## Type: `EnrollmentManager`

### Constructor

`NewEnrollmentManager(svc IPairingService, mtlsPort int, renewBefore time.Duration, logf func(string,...any))` — defaults: `mtlsPort` → `49532`, `renewBefore` → `48h`.

### Methods

| Method | Description |
|---|---|
| `Kick()` | Signal an immediate reconcile (non-blocking; buffered channel). Called by the adopt handler after a successful adoption. |
| `Run(ctx)` | Reconcile once on startup, then re-reconcile on every `Kick()` and every 5 minutes until `ctx` is cancelled. Tears down the listener on cancel. |

## Reconcile Loop

Each reconcile cycle:

1. Read pairing status via `IPairingService.Status`; if unpaired, stop the listener and return.
2. Read the stored `Enrollment`; if no cert is held or the cert is within `renewBefore` of expiry, call `enroll`.
3. If a cert is held, call `ensureListener` to start or restart the mTLS listener with the current cert.

## Enrollment (`enroll`)

- Reads `NodeID` and `ParentBaseURL` from the pairing service.
- Generates a fresh ECDSA P-256 key + CSR via `fleetca.GenerateKeyAndCSR(nodeID)`. The private key never leaves the node.
- POSTs `{"nodeId","token","csr"}` to `<parentBaseURL>/api/nodes/enroll` using an `InsecureSkipVerify` HTTP client (bootstrap leg; the parent's HTTPS cert may be self-signed).
- Parses the enrollment response for `nodeCert` + `caRoot` PEMs. The response envelope is dual-parsed: the standard `result.nodeCert` top-level path (produced by `controllers.SendResult`) is read first; the legacy `data.result.nodeCert` nesting is accepted as a fallback so any residual shape difference in the parent's response doesn't silently empty the certificate. Returns an error when both are empty. Stores the cert bundle together with the original key via `IPairingService.SaveCert`.

## mTLS Management Listener

`ensureListener(enr Enrollment)` starts an HTTPS server on `:<mtlsPort>` configured with `fleetca.ServerTLSConfig` (presents the node cert; requires a client cert signed by the fleet CA). The listener is restarted automatically when the cert changes after renewal.

Routes served:

| Method | Path | Behaviour |
|---|---|---|
| `GET` | `/heartbeat` | Returns `{"paired":true,"nodeId":"..."}`. The TLS layer has already verified the caller's client cert chains to the fleet CA. |
| `POST` | `/release` | Reads the stored enrollment token and calls `IPairingService.Release`, then kicks a reconcile (which tears down the listener since the node is now unpaired). |

## Notes

- The listener binds on all interfaces at the configured `mtlsPort` (default 49532) so the control plane can reach it regardless of which network interface the probe arrives on.
- `EnrollmentManager` is started as a goroutine inside the monitor lifecycle context (gated by `pairing.enabled`) — in `mymatasan`'s `apps/mymatasan/app/wire_fleet.go`, and identically in `myiotsan`'s `apps/myiotsan/app/wire_fleet.go` (see `docs/modules/apps/myiotsan/app/wire_fleet.go.md`).
- The adopt handler passes `enrollmentManager.Kick` as the `onAdopted` callback to `NewPairingPublicApi` so enrollment begins immediately after the adopt call returns.
- The enrollment HTTP client intentionally uses `InsecureSkipVerify` only for the bootstrap `/api/nodes/enroll` call; the management listener it starts is full mTLS.
