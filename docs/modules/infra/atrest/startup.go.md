# Module: infra/atrest/startup.go

## Purpose

Resolves the encryption-at-rest master key at boot with a strict precedence that never
silently mints a new key over already-encrypted data: it distinguishes "the key is missing
because this is a genuinely new install" from "the key is missing but data encrypted with it
still exists" (host-bound protector lost its host, key file deleted/corrupted, disk swapped),
and in the latter case requires recovery instead of guessing.

## Responsibilities

- `initMarker` — a small **non-secret** JSON file written beside the key (`<keyPath>.init`: `keyId`, `createdAt`, `protector`) the first time a key is established. It never holds key material — `keyId` is a random install identifier, not derived from the key — and exists purely so a later boot can tell "a key existed here" apart from "brand-new install". `ensureMarker`/`readMarker`/`writeMarker`/`removeMarker` manage it; `removeMarker` is called by `KeyStore.Destroy` so a post-wipe boot reads as a clean first run.
- `Outcome` / startup modes — `ModeLoaded` (existing key loaded normally), `ModeRestored` (key rebuilt from a config-configured recovery escrow, no prompt), `ModeCreated` (genuine first run, fresh key generated), `ModeRecoveryPending` (a key existed here before but is now missing — `KeyStore` is `nil`, caller must gate on recovery).
- `OpenForStartup(keyPath, recoveryPath, cfg)` — the boot-time entry point, in precedence order:
  1. Key file present → `LoadOrCreateWithConfig`, `ModeLoaded`.
  2. Key absent + `recoveryPath` configured, present, and a passphrase is resolvable → `LoadOrCreateWithRecovery`; success is `ModeRestored` with no user prompt. Failure (wrong passphrase/corrupt file) logs and falls through rather than hard-failing the boot.
  3. Key absent + marker present (a key existed here before) → `ModeRecoveryPending`, no key is created.
  4. Key absent + no marker → genuine first run, `LoadOrCreateWithConfig` generates a fresh key, `ModeCreated`.
  Every branch that establishes/loads a key also calls `ensureMarker`, so installs that predate this feature become recoverable on their next key-establishing boot.
- `RestoreFromEscrow(keyPath, escrow, passphrase, cfg)` — the interactive recovery-gate path: validates `escrow` is a passphrase-protector key file (`decodeKeyFile`), unwraps it with `passphrase`, then writes it at `keyPath` wrapped by the protector selected in `cfg` (`writeKeyFile`). Refuses anything that isn't a passphrase escrow or that the passphrase can't open; never leaves a partially-written key file behind.

## Notes

- `OpenForStartup` is called once, first, in `app.go`'s `RegisterAppRoutes` — before any other service is built — so a `ModeRecoveryPending` outcome can mount only the recovery gate (`apis.NewRecoveryGateApi`) and return early, guaranteeing nothing writes plaintext or mints a replacement key while recovery is pending.
- `passphraseAvailable(cfg)` is a cheap pre-check (`resolvePassphrase` succeeds) so `OpenForStartup` only *attempts* config-driven recovery when a passphrase is actually resolvable, avoiding a doomed unwrap on every boot when `recoveryPath` is configured but not yet backed by a passphrase.
- Tests: `startup_test.go` (all four modes, marker lifecycle, config-driven recovery success/failure fallthrough, escrow restore).
