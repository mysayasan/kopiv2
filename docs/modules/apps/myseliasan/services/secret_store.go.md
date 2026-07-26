# Module: apps/myseliasan/services/secret_store.go

## Purpose

Encryption-at-rest helpers for the handful of SECRET control-plane settings — the fleet CA private key, the parent leaf private key, and the fleet PSK — that would otherwise sit in plaintext in the control-plane database (`ControlSetting` rows). Reuses the shared `infra/atrest` AES-256-GCM module rather than inventing a new primitive; see `docs/modules/infra/atrest/cipher.go.md`. Consumed by `fleet_ca.go` (`readSecret`/`writeSecret`), `node_registry.go` (`FleetKey`/`upsertFleetKey`), and `settings.go` (`ensureDefaults`/`loadDefaults`, encrypting the in-app settings editor's first-run defaults snapshot — see `services/settings.go.md`).

## Responsibilities

- `encodeSecret(cipher *atrest.Cipher, plaintext string) (string, error)` — returns the storage form of a secret value: `base64(atrest ciphertext)` when `cipher` is non-nil, otherwise the plaintext unchanged (encryption disabled, or empty value). `ControlSetting.Value` is a shared TEXT column across sqlite/postgres/mariadb, so the binary AEAD blob is base64-wrapped for safe storage.
- `decodeSecret(cipher *atrest.Cipher, stored string) string` — reverses `encodeSecret`. If the stored value does not base64-decode to atrest-magic-prefixed ciphertext (`atrest.IsEncrypted`), it is returned unchanged — this is how a legacy plaintext value (PEM key, raw PSK) written before encryption was enabled is read correctly with no migration step. If the value **is** encrypted but no cipher is configured (key unavailable), the encrypted blob is returned as-is rather than a decryption error — the caller then fails to use it as a valid key/PSK (e.g. the CA fails to load and is regenerated) instead of silently getting garbage.

## Notes

- The three PRIVATE-key/secret pairing settings route through these helpers (`pairing.caKey`, `pairing.parentKey`, `pairing.fleetKey`). Public certs (`pairing.caCert`, `pairing.parentCert`) and the revocation list (`pairing.revoked`) are not secrets and are read/written as plain `ControlSetting` values, unaffected by this module. `settings.go`'s `settings.defaults` row (the whole first-run config snapshot, which itself contains secret leaves like `localAuth.password`) is also encrypted through the same two helpers, even though it is not a `pairing.*` key.
- The cipher is resolved once at startup by `apps/myseliasan/app/app.go`'s `openFleetSecretCipher` (see `app.go.md`) from the shared `security` config block (`infra/config.SecurityConfigModel` — the same block mymatasan documents for media encryption at rest) and threaded in via `NodeRegistryConfig.SecretCipher` → `newFleetCA`'s `secretCipher` parameter. A nil cipher throughout means encryption at rest is off and behavior is byte-for-byte the same as before this feature.
- Tests in `secret_store_test.go` cover the encode/decode roundtrip, legacy-plaintext passthrough, and (via `fleet_ca.go`/`node_registry.go`) that the CA key and fleet PSK are actually stored encrypted when a cipher is configured, and that a legacy plaintext PSK remains readable.
