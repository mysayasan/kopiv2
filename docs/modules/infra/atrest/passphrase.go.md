# Module: infra/atrest/passphrase.go

## Purpose

Portable, passphrase-based encryption of **arbitrary payloads** (not the master key). It is the crypto primitive behind the mymatasan configuration backup (`apps/mymatasan/services/backup.go`): because a settings backup must open on a *different* machine, it cannot be protected by the host-bound at-rest key (DPAPI/systemd-creds) — it needs a user passphrase instead.

## Responsibilities

- `EncryptWithPassphrase(passphrase, plaintext) ([]byte, error)` — derives a KEK from the passphrase with **Argon2id** (reusing the same in-package parameters and `newGCM` helper as the passphrase key-protector) and seals the plaintext with **AES-256-GCM**. The Argon2id parameters (time/memory/threads) and salt are written into the self-describing blob so they can evolve without breaking older files. Rejects an empty passphrase.
- `DecryptWithPassphrase(passphrase, blob) ([]byte, error)` — reverses the above. A wrong passphrase or any tampering fails GCM authentication and returns an error rather than garbage.

## Notes

- Distinct from `KeyStore.ExportEscrow`/`VerifyEscrow` (in `keystore.go`), which wrap the fixed-size 32-byte master key for disaster recovery. These functions wrap *arbitrary-length* data and are unrelated to the machine's key — nothing here is host-bound, so output is portable across hosts.
- Tests in `passphrase_test.go` cover round-trip, wrong passphrase, tamper detection, and empty-passphrase rejection.
