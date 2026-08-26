# Module: apps/myidsan/apis/mfa_audit_test.go

## Purpose

Locks in the fix found by a live bench (`tools/fleetbench/bench_idsan_mfa.py`): five audit
actions were declared in `services/audit.go` (`mfa.enroll`, `mfa.disable`,
`mfa.recovery_used`, `mfa.recovery_regenerate`, `login.mfa_challenge`) and nothing ever
wrote them — the whole second-factor lifecycle was invisible on the trail. `mfa.disable` is
the worst of them: `Disable` deletes the factor row and every recovery-code hash, so the act
erases its own evidence, and without an audit entry an operator cannot even establish that a
factor existed, let alone who removed it.

## Coverage

- `stubMfa` scripts `IMfaService` (embeds the interface so only the methods these handlers
  reach need implementing): `verify services.MfaVerifyResult` controls `VerifyCode`'s
  `{Ok, UsedRecovery}` outcome, `confirmCodes`/`confirmErr` control `ConfirmEnroll`.
- `stubUsers` accepts any password via `AuthenticateDefault`, so a `disable` test measures
  the audit write rather than the credential check that has its own coverage elsewhere.
- `mfaApiForTest(mfa)` / `mfaRequest(method, body)` build a `*mfaApi` wired to a
  `recordingAudit` and an authenticated request carrying claims for `userId 1`.
- `actionsIn(audit)` counts recorded entries by `Action`.
- `TestDisablingTheSecondFactorIsAudited` — `disable` now records exactly one
  `services.ActionMfaDisable` entry.
- `TestConfirmingEnrollmentIsAudited` — `confirmEnroll` records `services.ActionMfaEnroll`,
  recorded at **confirmation** rather than at `beginEnroll` since staging a secret changes
  nothing about how the account authenticates.
- `TestARefusedEnrollmentIsNotAuditedAsAnEnrollment` — a bad confirmation code records
  **zero** `ActionMfaEnroll` entries; a refusal must not be recorded as though it happened.
- `TestRegeneratingRecoveryCodesIsAudited` — `regenerateRecovery` records
  `services.ActionMfaRegenerate`.
- `TestARecoveryCodeSpentAtTeardownIsAuditedSeparately` — when the code that authorized
  `disable` was itself a recovery code (`MfaVerifyResult.UsedRecovery`), both
  `services.ActionMfaRecovery` **and** `services.ActionMfaDisable` are recorded — the burn
  must not replace the removal.
- `TestATotpTeardownIsNotReportedAsARecoveryBurn` — the mirror image: a TOTP-authorized
  `disable` records zero `ActionMfaRecovery` entries, or the recovery-burn signal fires on
  the routine case and stops meaning anything.
