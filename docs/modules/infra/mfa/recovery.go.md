# Module: infra/mfa/recovery.go

## Purpose

Generates and normalizes the single-use "break-glass" recovery codes that
substitute for a TOTP code when a user has lost their authenticator device.

## Responsibilities

- `GenerateRecoveryCodes()` — returns `RecoveryCodeCount` (10) fresh, high-entropy
  plaintext codes, each shaped as 4 dash-joined groups of 5 characters
  (`xxxxx-xxxxx-xxxxx-xxxxx`, 20 chars = 100 bits of entropy) drawn from
  `recoveryAlphabet` — a base32-like set that excludes the visually ambiguous
  `0`/`O`/`1`/`I`/`L` so a code copied by hand does not fail on a mistaken character.
  The caller shows the codes to the user exactly once and persists only their bcrypt
  hashes (see `apps/myidsan/services/mfa.go.md`'s `replaceRecoveryCodes`).
- `NormalizeRecoveryCode(in)` — canonicalises user input (case, spacing, dashes) so
  a code typed as `"abcde fghij ..."` still matches the stored hash of
  `"ABCDE-FGHIJ-..."`. Returns the compacted string unchanged (un-grouped) if it
  isn't exactly `recoveryGroups * recoveryGroupLen` characters after stripping
  non-alphabet runes, letting the hash comparison fail rather than mangling it
  further.

## Notes

- Codes are single-use: `apps/myidsan/services/mfa.go`'s `consumeRecoveryCode`
  matches a normalized code against the user's unused codes and marks the first
  match used (`UsedAt`).
- Regenerating a set (self-service `POST /api/mfa/recovery`, or on enrollment
  confirm) invalidates and deletes the previous set entirely.
