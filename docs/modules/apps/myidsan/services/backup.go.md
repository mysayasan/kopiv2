# Module: apps/myidsan/services/backup.go

## Purpose

Implements `IBackupService` — a portable, passphrase-encrypted export/restore of
myidsan's identity store, so a dead host can be rebuilt without "copy the database and
`secret/atrest.key` yourself, as a pair" (the previous documented recovery procedure).
myidsan is the one app in the suite where losing the database locks every user out of
every other app at once: it holds every user, every role, every registered SSO client,
the SSO CA private key, all TOTP secrets, and the LDAP bind password.

## How the at-rest secrets travel

The load-bearing design decision. TOTP secrets and the LDAP bind password are sealed
with the host's at-rest key (`infra/atrest`). Copying the sealed bytes into the archive
would produce a backup that restores "successfully" onto a fresh host and then fails
every second-factor check, because the new host has a different key — the worst
possible outcome, since it is only discovered when someone tries to log in.

So the sealed columns are **unsealed on export** (into the archive, which is itself
encrypted with the operator's passphrase via `atrest.EncryptWithPassphrase`) and
**re-sealed with the destination host's own key on restore** (`encodeSecret`/
`decodeSecret`). The at-rest key itself is never included in the archive.

## File format

- Magic `IDBK` (`backupMagic`) + one format-version byte (`backupFormatVersion`) +
  `atrest.EncryptWithPassphrase`-sealed JSON (`backupFile`). A non-backup upload, or one
  with a passphrase that doesn't decrypt it, is rejected before any of the contents are
  trusted.
- `BackupManifest` (app name, `appVersion` from the version manifest, `schemaVersion`,
  `createdAt`, the sections present, and a row count per section) sits in the clear once
  decrypted, so `Preview` can show what a file holds without applying anything.
- Six logical, user-selectable sections, in a fixed dependency order (later sections'
  foreign keys are remapped using ids produced by earlier ones): `access` (roles +
  permissions), `identity` (users, groups, avatars), `mfa` (TOTP factors + recovery
  codes), `apps` (app registry + auth config + redirect URIs), `federation` (directory
  config + group mappings), `ssoca` (the SSO CA cert and private key).
- Deliberately excluded (host-local state): `config.json`, TLS certificates, the at-rest
  key, `api_log`/`runtime_log`, `password_reset_request` (a transient operator queue —
  restoring stale requests would hand out temporary passwords nobody asked for), and
  `user_session` (cache-backed, explicitly invalidated by `Restore` instead).

## Responsibilities

- `AvailableSections(ctx)` — row counts per section, for the export screen to show what
  is about to be included and grey out empty sections.
- `Export(ctx, BackupRequest{Sections, Passphrase})` — refuses an empty passphrase or an
  empty section list, collects each requested section (`collect`), marshals the
  manifest + rows to JSON, and seals the whole thing with the passphrase.
- `Preview(ctx, data, passphrase)` — decodes and returns only the `BackupManifest`, so an
  operator can confirm app version/sections/counts before committing to a restore that
  replaces live accounts.
- `Restore(ctx, data, RestoreRequest{Sections, Passphrase, Mode})` — decodes the archive,
  intersects the requested sections with what the file actually contains
  (`restoreSections`), and applies each present section in the fixed dependency order via
  `restoreAccess` → `restoreIdentity` → `restoreMfa` → `restoreApps` →
  `restoreFederation` → `restoreSsoCa`. `Mode` is `"replace"` (wipes each selected
  section's own tables first — the default and the one to use when rebuilding a host) or
  `"merge"` (appends without clearing).
  - Every restored row gets a fresh id (`row.Id = 0`, `Create`); foreign keys are
    remapped through per-run `map[oldID]newID` tables built as each parent section is
    restored (`roleIDs`, `userIDs`, `appIDs`, `authIDs`).
  - A child row whose parent id is **not present** in the remap table is **skipped**
    (counted in `RestoreResult.Skipped`), never restored with a dangling or
    reinterpreted foreign key — e.g. a permission whose role wasn't in the backup, a
    user whose role wasn't restored (lands role-less rather than silently inheriting
    whatever role now occupies that id on the new host), an MFA factor whose user
    wasn't restored, a redirect URI whose auth config wasn't restored, a group mapping
    whose role wasn't restored.
  - After every section is applied, every live session is dropped
    (`cache.Store.DeleteByPrefix("sso:session:")`) — a session token issued a moment
    earlier would still resolve to ids/roles that may no longer mean what they did,
    so `RestoreResult.SessionsInvalidated` is set and the caller is expected to sign the
    operator out immediately.
  - First-run setup is marked complete (`ISetupStateService.Complete`) best-effort, so a
    restored instance never re-shows the first-run wizard.
  - A `SchemaVersion` mismatch produces a non-fatal `RestoreResult.SchemaWarning` — the
    restore still proceeds best-effort.

## Notes

- `sessionCachePrefix = "sso:session:"` must stay in sync with the key
  `middlewares/auth.go` writes sessions under.
- `backupPageLimit = 100000` bounds each section's `repo.Get` page size — generous
  enough that a real deployment's row counts fit in one page, avoiding paging logic in
  export/wipe.
- `NewBackupService` takes every repo the six sections touch, the `*atrest.Cipher` (nil
  when at-rest encryption is disabled — sealed columns are then already plaintext and
  pass through unchanged), `cache.Store` (for session invalidation), `ISetupStateService`,
  and the app version string (see `apps/myidsan/app/app.go.md`'s `moduleAppVersion`).
- Wired in `apps/myidsan/app/app.go`'s `RegisterAppRoutes` and mounted via
  `apis.NewBackupApi` (`apis/backup.go.md`).
