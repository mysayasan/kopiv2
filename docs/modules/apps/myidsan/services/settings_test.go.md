# Module: apps/myidsan/services/settings_test.go

## Purpose

Tests `ISettingsService` (`services/settings.go.md`) against a minimal but representative
myidsan `config.json` (`idsanRawConfig`) that carries the editable blocks, an `audit` block
the editor must never touch, and `db`/`server` blocks so tests can prove untouched blocks
survive byte-for-byte. `newIdsanSettings(t)` builds the service with a nil database/cipher —
the defaults snapshot is best-effort and only `Reset` depends on it, which these tests do not
exercise.

## Coverage

- **`TestAuditRetentionIsNotReachableFromTheSettingsEditor`** — THE ONE THAT MATTERS MOST.
  `audit.retention` is the security trail's only removal path and is config-file-only ON
  PURPOSE: trimming the trail must require filesystem access to the server rather than a
  session on it, so the superadmin whose own actions the trail records cannot reach for it
  from inside the product. Asserts `audit` is not in `Sections()`, `Get("audit")` errors,
  `GetAll()` carries no `"audit"` key, and the marshaled `GetAll()` payload contains neither
  the literal `"audit"` nor `"retention"` needle — matched on the audit-specific shape (not a
  bare `"maxRetentionDays"`, since `logging`/`apiLog` legitimately have their own unrelated
  `cleanup.maxRetentionDays` fields).
- **`TestSavingAuditRetentionIsRefused`** — a save cannot reach it either: `Save(ctx, "audit",
  ...)` is refused outright (unknown section), and smuggling the same payload through a
  section that *is* editable (`Save(ctx, "logging", {"audit":{"retention":...}})`) succeeds
  for the logging save but is dropped by `projectOntoShape`'s shape projection rather than
  written through — verified both in the in-memory config and by re-reading `config.json` off
  disk.
- **`TestSecretsAreMasked`** — the SSO `internalToken` and the local-auth password never
  appear in `Get`'s response, marshaled and searched as a substring.
- **`TestBlankSecretKeepsTheStoredValue`** — saving a section with a blank secret field (the
  form never received the real value) does not wipe the stored token; the accompanying
  non-secret field in the same save still applies.
- **`TestSecurityShowsResolvedPolicyDefaultsNotZeroValues`** — `Get("security")` must report
  the lockout as `enabled: true` with a real `maxAttempts`, and a real `passwordPolicy.minLength`
  and non-empty `mfa.policy`, even though none of `loginSecurity`/`passwordPolicy`/`mfa` are
  present in `idsanRawConfig` — proving the section reads through the `Effective*()` resolvers
  rather than the zero-valued raw struct.
- **`TestSecurityValidationRefusesUselessPolicies`** — a table of three "policy that isn't":
  a lockout with `maxAttempts: 1` (locks out before the first attempt), a password
  `minLength: 4`, and an unknown `mfa.policy` value — each must be refused by `Save`.
- **`TestLongAuthCodeTTLIsRefused`** — an `authCodeTtlSeconds` of 86400 (24 hours) is refused;
  an authorization code lives in a redirect URL and therefore in browser history, proxy logs,
  and Referer headers.
- **`TestUntouchedBlocksSurviveASave`** — after saving `localAuth`, the untouched `db` and
  `server` blocks are present in the rewritten `config.json` byte-for-byte identical to the
  original, proving `settings_materialize.go`'s surgical rewrite never reformats what it
  doesn't touch.
- **`TestSaveReportsNeedsRestart`** — every successful `Save` reports `SaveResult{NeedsRestart:
  true}`, since the host only reads these blocks at boot.
