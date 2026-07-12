# Module: apps/myseliasan/services/fleet_ca.go

## Purpose

Control-plane wrapper around `infra/fleetca`. It persists the CA key material and revocation denylist in the `ControlSetting` KV store, mints and caches the control plane's own client certificate, and exposes the subset of CA operations that the `nodeRegistry` needs.

## Constructor

`newFleetCA(settings, parentID, certTTL, secretCipher *atrest.Cipher)` — `secretCipher` (from `NodeRegistryConfig.SecretCipher`, resolved in `app.go`'s `openFleetSecretCipher`) is nil when fleet-secret encryption at rest is disabled.

## Responsibilities

- `ensure(ctx)` — load the CA from the `pairing.caCert` / `pairing.caKey` settings rows (or create and persist a fresh one with a 10-year validity when none exists). The loaded `*fleetca.CA` is cached in memory for the process lifetime. The CA private key is read/written via `readSecret`/`writeSecret` (see "Encryption at rest" below); the cert stays plaintext.
- `CARootPEM(ctx)` — return the CA public certificate so it can be distributed to nodes during enrollment.
- `SignNodeCSR(ctx, nodeID, csrPEM)` — issue a node leaf certificate (CN = `nodeID`, TTL = `certTTL`) from the provided CSR. Returns `ErrNodeRevoked` if the node is on the denylist. Also returns the CA root PEM so the node can pin it.
- `ParentClientTLS(ctx, expectNodeID)` — mint (or load from cache, renewing when within 7 days of expiry) the control plane's own 90-day client certificate under `pairing.parentCert` / `pairing.parentKey`, then return the cert PEM, key PEM, and CA root PEM. The caller uses these to build a `fleetca.ClientTLSConfig` dialing a specific node. The parent leaf private key is read/written via `readSecret`/`writeSecret`; the cert stays plaintext.
- `Revoke(ctx, nodeID)` — add a node ID to the `pairing.revoked` JSON denylist (a serialized `[]string`) so it can no longer obtain new certificates.
- `Unrevoke(ctx, nodeID)` — remove a node ID from the denylist (e.g. when re-adopting a previously released node).
- `IsRevoked(ctx, nodeID)` — check whether a node ID is in the denylist.

## Storage Keys

| Setting key | Contents | At rest |
|---|---|---|
| `pairing.caCert` | CA certificate PEM | plaintext |
| `pairing.caKey` | CA private key PEM | encrypted (see below) |
| `pairing.parentCert` | Control-plane client certificate PEM | plaintext |
| `pairing.parentKey` | Control-plane client private key PEM | encrypted (see below) |
| `pairing.revoked` | JSON array of revoked node ID strings | plaintext |

## Encryption at rest

`pairing.caKey` and `pairing.parentKey` — the two PRIVATE keys that, if read from the DB, would let an attacker mint or trust arbitrary node certificates for the fleet — are encrypted at rest when a cipher is configured, via the shared helpers in `secret_store.go.md` (`readSecret`/`writeSecret` wrap `decodeSecret`/`encodeSecret`). Public certs and the revocation list are not secrets and stay plaintext. `secretCipher` is nil (plaintext, feature disabled) unless `app.go`'s `openFleetSecretCipher` resolves a cipher from the shared `security` config block (default on). Reading a legacy plaintext key (written before encryption was enabled) transparently passes through — no migration is needed to turn the feature on for an existing installation.

## Notes

- `certNearExpiry(certPEM, within)` is an unexported helper that returns `true` when the leaf in `certPEM` expires within `within` of now, or is unparseable.
- The control-plane client cert has a fixed 90-day TTL and is renewed when within 7 days of expiry.
- Node cert TTL is configurable via `NodeRegistryConfig.CertTTL` (from `pairing.certTtlHours`); default is 7 days.
- The `fleetCA` struct is unexported; `nodeRegistry` holds a pointer and calls its methods directly.
