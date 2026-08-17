# Module: apps/myseliasan/services/fleet_ca.go

## Purpose

Control-plane wrapper around `infra/fleetca`. It persists the CA key material and revocation denylist in the `ControlSetting` KV store, mints and caches the control plane's own client certificate, and exposes the subset of CA operations that the `nodeRegistry` needs.

## Constructor

`newFleetCA(settings, parentID, certTTL, secretCipher *atrest.Cipher)` — `secretCipher` (from `NodeRegistryConfig.SecretCipher`, resolved in `app.go`'s `openFleetSecretCipher`) is nil when fleet-secret encryption at rest is disabled.

## Responsibilities

- `ensure(ctx)` — load the CA from the `pairing.caCert` / `pairing.caKey` settings rows (or create and persist a fresh one with a 10-year validity when none exists). The loaded `*fleetca.CA` is cached in memory for the process lifetime. The CA private key is read/written via `readSecret`/`writeSecret` (see "Encryption at rest" below); the cert stays plaintext.
- `CARootPEM(ctx)` — return the CA public certificate so it can be distributed to nodes during enrollment.
- `SignNodeCSR(ctx, nodeID, csrPEM)` — issue a node leaf certificate (CN = `nodeID`, TTL = `certTTL`) from the provided CSR. Returns `ErrNodeRevoked` if the node is on the denylist. Also returns the CA root PEM so the node can pin it.
- `ParentClientTLS(ctx, expectNodeID)` — mint (or load from cache, renewing when within 7 days of expiry) the control plane's own 90-day client certificate under `pairing.parentCert` / `pairing.parentKey`, then return the cert PEM, key PEM, and CA root PEM. The caller uses these to build a `fleetca.ClientTLSConfig` dialing a specific node. The parent leaf private key is read/written via `readSecret`/`writeSecret`; the cert stays plaintext. See "FIXED: the parent certificate could be permanently broken" below for `parentCert`'s multi-instance safety.
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

## FIXED (Phase 3 bench): the parent certificate could be permanently broken by two instances starting together

`parentCert` reads `pairing.parentCert`/`pairing.parentKey` from **two separate**
`ControlSetting` rows with no locking. Two instances starting at the same moment could both
find neither row set (or both find the cached pair near expiry), both call
`ca.IssueLeaf`, and both write — and since the two writes interleave across two rows, one
instance's certificate can end up stored beside the OTHER instance's key. Nothing detected
this at write time, and it **persisted**: on every later boot both rows were non-empty and
not near expiry, so the old code returned them unexamined, and the fleet's mTLS listener
failed to start with "private key does not match public key" — the control channel never
opened, EVERY ADOPTED NODE WENT DARK, permanently, until an operator manually cleared both
rows. This was hit **twice** during the live Phase 3 bench (the second time with the at-rest
key correctly shared between instances, which is what isolated it as a distinct bug from an
at-rest key mismatch).

Fixed two ways:

1. **Validate on read, self-heal on mismatch.** Before trusting a cached, non-expiring pair,
   `parentCert` now calls `tls.X509KeyPair(cert, key)`; a mismatch falls through to
   re-issuing rather than being returned. Both the old and the new leaf are signed by the
   same CA, so whichever pair a concurrent reader ends up with is equally valid to every
   node — there is no "wrong" winner, only a broken PAIRING of an otherwise-valid cert with
   an otherwise-valid key. This makes an install **already broken** by the bug self-heal on
   its next `ParentClientTLS` call, with no manual row-clearing needed.
2. **Write the key before the certificate.** A concurrent reader arriving mid-write now sees
   either the OLD pair (still matching, since the key hasn't changed yet) or a key with NO
   certificate yet (which fails the `cert != "" && key != ""` presence check and re-issues)
   — never a certificate that LOOKS current sitting beside a key that does not belong to it,
   which is the exact state that used to persist forever.

Covered by `fleet_ca_pair_test.go`: `TestParentCertReissuesOnMismatchedStoredPair` corrupts
the stored key underneath a valid certificate and asserts the next read returns (and
persists) a matching pair instead of the broken one; `TestParentCertReusesHealthyStoredPair`
asserts a healthy pair is reused, not re-issued on every call, so the fix does not churn the
fleet's parent certificate on every boot.

## Notes

- `certNearExpiry(certPEM, within)` is an unexported helper that returns `true` when the leaf in `certPEM` expires within `within` of now, or is unparseable.
- The control-plane client cert has a fixed 90-day TTL and is renewed when within 7 days of expiry.
- Node cert TTL is configurable via `NodeRegistryConfig.CertTTL` (from `pairing.certTtlHours`); default is 90 days (`defaultCertTTL`, raised from 7 days now that renewal is operator-gated per node — see `ManagedNode.AutoRenew` in `entities/managed_node.go.md` and `nodeRegistry.Enroll` in `services/node_registry.go.md` — so a node nobody blesses for auto-renew still stays in the fleet for a meaningful window rather than lapsing weekly).
- The `fleetCA` struct is unexported; `nodeRegistry` holds a pointer and calls its methods directly.
