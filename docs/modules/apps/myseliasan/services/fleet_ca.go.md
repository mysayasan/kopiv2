# Module: apps/myseliasan/services/fleet_ca.go

## Purpose

Control-plane wrapper around `infra/fleetca`. It persists the CA key material and revocation denylist in the `ControlSetting` KV store, mints and caches the control plane's own client certificate, and exposes the subset of CA operations that the `nodeRegistry` needs.

## Responsibilities

- `ensure(ctx)` — load the CA from the `pairing.caCert` / `pairing.caKey` settings rows (or create and persist a fresh one with a 10-year validity when none exists). The loaded `*fleetca.CA` is cached in memory for the process lifetime.
- `CARootPEM(ctx)` — return the CA public certificate so it can be distributed to nodes during enrollment.
- `SignNodeCSR(ctx, nodeID, csrPEM)` — issue a node leaf certificate (CN = `nodeID`, TTL = `certTTL`) from the provided CSR. Returns `ErrNodeRevoked` if the node is on the denylist. Also returns the CA root PEM so the node can pin it.
- `ParentClientTLS(ctx, expectNodeID)` — mint (or load from cache, renewing when within 7 days of expiry) the control plane's own 90-day client certificate under `pairing.parentCert` / `pairing.parentKey`, then return the cert PEM, key PEM, and CA root PEM. The caller uses these to build a `fleetca.ClientTLSConfig` dialing a specific node.
- `Revoke(ctx, nodeID)` — add a node ID to the `pairing.revoked` JSON denylist (a serialized `[]string`) so it can no longer obtain new certificates.
- `Unrevoke(ctx, nodeID)` — remove a node ID from the denylist (e.g. when re-adopting a previously released node).
- `IsRevoked(ctx, nodeID)` — check whether a node ID is in the denylist.

## Storage Keys

| Setting key | Contents |
|---|---|
| `pairing.caCert` | CA certificate PEM |
| `pairing.caKey` | CA private key PEM |
| `pairing.parentCert` | Control-plane client certificate PEM |
| `pairing.parentKey` | Control-plane client private key PEM |
| `pairing.revoked` | JSON array of revoked node ID strings |

## Notes

- `certNearExpiry(certPEM, within)` is an unexported helper that returns `true` when the leaf in `certPEM` expires within `within` of now, or is unparseable.
- The control-plane client cert has a fixed 90-day TTL and is renewed when within 7 days of expiry.
- Node cert TTL is configurable via `NodeRegistryConfig.CertTTL` (from `pairing.certTtlHours`); default is 7 days.
- The `fleetCA` struct is unexported; `nodeRegistry` holds a pointer and calls its methods directly.
