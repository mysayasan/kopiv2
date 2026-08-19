# Module: apps/myidsan/services/audit.go

## Purpose

myidsan's slice of the audit trail. The trail itself — entity, record/list/purge, retention
— now lives in `domain/shared/audit` (`docs/modules/domain/shared/audit/service.go.md`):
myidsan and myseliasan had each grown an independent copy that had already begun to drift
(only myidsan truncated hostile input, only myidsan had retention, only myidsan recorded the
user agent), and mymatasan — the app holding the actual video evidence — had none at all.
What stays in this file is the part that is genuinely myidsan's: the identity-server action
vocabulary and sign-in-method constants, plus the two constructors that wire the shared
service to myidsan's own database and metric names.

## Responsibilities

- `Action*` constants (`ActionLoginSuccess`, `ActionLoginFailure`, `ActionLoginLockout`,
  `ActionLogout`, `ActionMfaChallenge`, `ActionPasswordReset`, `ActionMfaEnroll`,
  `ActionMfaDisable`, `ActionMfaAdminReset`, `ActionMfaRecovery`, `ActionMfaRegenerate`,
  `ActionWebAuthnEnroll`, `ActionWebAuthnRemove`, `ActionWebAuthnRename`,
  `ActionWebAuthnAdminReset`, `ActionWebAuthnClone`,
  `ActionUserCreate`, `ActionUserUpdate`, `ActionUserDelete`, `ActionUserRoleChange`,
  `ActionPasswordChange`, `ActionRoleCreate/Update/Delete`, `ActionPermissionSet`,
  `ActionAppCreate/Update/Delete`, `ActionAppSecretRotate`, `ActionAppRedirectSet`,
  `ActionDirectoryUpdate`, `ActionGroupMapChange`, `ActionStepUpSuccess/Failure`,
  `ActionSessionRevoke`/`ActionSessionRevokeAll`, `ActionBackupExport`/`ActionBackupRestore`)
  are kept as named constants, convention `<subject>.<verb>`, so the set stays greppable and
  a UI filter can offer a closed list. `Method*` sign-in-method constants (`local`/`ldap`/
  `kerberos`/`oidc`/`social`/`recovery_code`) live alongside them.
- Type aliases onto `domain/shared/audit`: `AuditEntry = sharedaudit.Entry`,
  `AuditFilter = sharedaudit.Filter`, `IAuditService = sharedaudit.IService`,
  `PurgeResult = sharedaudit.PurgeResult` — so every existing call site (handlers, tests,
  `app/app.go`) keeps compiling unchanged. `Outcome*` (`success`/`denied`/`error`) are
  re-exported the same way, and `ActionAuditPurge` (the action the retention purge records
  itself under) is re-exported from `sharedaudit.ActionAuditPurge`.
- `NewAuditService(repo dbsql.IGenericRepo[sharedaudit.AuditLog], logf) IAuditService` —
  thin wrapper over `sharedaudit.NewService`. The table is unchanged: the schema
  bootstrapper derives the table name from the struct name alone, and the shared entity
  keeps the name `AuditLog` for exactly that reason.
- `WithAuditMetrics(svc, m telemetry.Metrics) IAuditService` — attaches the recorder under
  myidsan's own series names (`MetricAuditWriteFailuresTotal`/`MetricAuditRetentionPurgedTotal`,
  `services/metrics.go.md`) via `sharedaudit.WithMetrics(svc, m, sharedaudit.MetricNames{...})`.
  Called directly from `app.go` now, not through an optional type assertion — see
  `app/app.go.md` for why the assertion form was a silent-failure trap once the shared
  package's setter gained a second parameter.

## Notes

- `PurgeOlderThan` and its archive-then-delete mechanics, previously implemented in this
  app's own `services/audit_retention.go` (now deleted), live in
  `domain/shared/audit/retention.go`; `apps/myidsan/app/audit_retention.go` (`app/audit_retention.go.md`)
  still wires it into the scheduler exactly as before.
- The five `ActionWebAuthn*` constants are separate from the `ActionMfa*` (TOTP) ones above
  them because they answer different investigative questions: "which key was added, and
  from where" is per-**credential**, not per-account. `ActionWebAuthnAdminReset` is an
  administrator clearing SOMEONE ELSE's keys (see `apis/webauthn.go.md`'s
  `adminResetAll`). `ActionWebAuthnClone` is written when an assertion's signature counter
  fails to advance — the sign-in is still allowed (see `services/webauthn.go.md` for why
  that ambiguity is not treated as proof), so this entry is the only durable trace that it
  happened.
- Action vocabularies stay per-app by design (see `domain/shared/audit/service.go.md`): the
  verbs are what each app does, and one shared list of every app's actions would be a list
  nobody can read.
- Constructed once in `app/app.go.md` and passed by value into every handler that performs
  a sensitive action, rather than reached through a global.
