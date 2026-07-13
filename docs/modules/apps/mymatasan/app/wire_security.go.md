# Module: apps/mymatasan/app/wire_security.go

## Purpose

`openAtRest` resolves the encryption-at-rest master key and decides whether the app should
start normally or enter RECOVERY mode. Moved out of `app.go` (Tier 2 phase D2) so the
RECOVERY-mode decision is a typed flag the composition root acts on (`atrestBoot.RecoveryPending`),
instead of an early `return` buried in ~50 lines of key handling.

## Responsibilities

- `type atrestBoot struct { KeyStore *atrest.KeyStore; Cipher *atrest.Cipher; RecoveryPending bool }`
  — the outcome. `RecoveryPending` is the field that matters: when true, the caller must
  mount nothing else and return.
- `openAtRest(api *mux.Router, deps apphost.Dependencies) (atrestBoot, error)`:
  - No-ops (`atrestBoot{}, nil`) when `security.encryptAtRest` is off (default on via
    `boolValue(..., true)`).
  - Resolves `keyPath` (`security.keyPath`, else `apphost.ResolveWritablePath(deps.DataDir,
    "secret/atrest.key")` — which keeps an existing legacy pre-packaging key in place so
    upgrades don't orphan encrypted footage) and `recoveryPath` (`security.recoveryPath`,
    else `recovery.atrestkey` beside the key).
  - Builds `atrest.ProtectorConfig` from `security.keyProtector`/`passphrase`/
    `passphraseFile`/`passphraseEnv`, then calls
    `atrest.OpenForStartup(keyPath, recoveryPath, protectorCfg)`.
  - On `atrest.ModeRecoveryPending` (a key existed here before via the init marker but is
    now missing): mounts `apis.NewRecoveryGateApi(api, keyPath, protectorCfg,
    deps.Restarter, outcome.KeyId)` on the **public** router, logs, and returns
    `atrestBoot{RecoveryPending: true}` — no camera/vision/recording/API services are
    built by the caller, no DB writes happen, and nothing can mint a replacement key.
  - Otherwise (`ModeLoaded`/`ModeCreated`/`ModeRestored`) returns the resolved
    `KeyStore`/`Cipher`; a `ModeRestored` outcome (key rebuilt from a config-driven
    recovery escrow) is logged distinctly.

## Notes

- Runs FIRST in `RegisterAppRoutes`, before any service is built, precisely so a missing
  key cannot race a service into writing plaintext.
- The key deliberately lives OUTSIDE the media roots, so a factory reset can destroy it
  explicitly (`KeyStore.Destroy`) — that's what makes the wipe instant and
  device-independent rather than a scrub of every file.
- Pure move from `app.go`; no behavior change. See `docs/modules/apps/mymatasan/app/app.go.md`
  and `docs/modules/apps/mymatasan/apis/recovery_gate.go.md`.
