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

A WebAuthn credential's public key needs **none** of this — it is a public key, not a
symmetric secret, so nothing about it is bound to the exporting host's at-rest key. It
does need a wrapper for a different reason, though: `UserWebauthnCredential.PublicKey` is
`json:"-"` on the entity (so it never rides along in an ordinary REST projection), and
without re-exposing it explicitly for the backup, `json.Marshal` silently drops it —
the archive would then carry a credential id with no key, restore without error, and fail
every assertion signature check afterwards. See `backupWebauthnCredential` below;
`TestBackupCarriesSecurityKeyPublicKey` covers exactly this. A credential carries one
host-coupling of its own kind that the backup **cannot** paper over: it is bound to the
Relying Party ID it was created under, so restoring it onto a host answering on a
different hostname (with `webauthn.relyingPartyId` left to derive) makes the browser
refuse it; set `relyingPartyId` explicitly first if a DR host will not share the
original hostname.

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
  codes **+ WebAuthn/security-key credentials**), `apps` (app registry + auth config +
  redirect URIs), `federation` (directory config + group mappings), `ssoca` (the SSO CA
  cert and private key). Security keys ride inside the existing `mfa` section rather than
  getting one of their own, because an operator selecting "mfa" means "the second
  factors" — leaving keys out would silently restore an account whose only factor had
  vanished, a lockout under a `required` MFA policy.
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
  `"merge"` (appends without clearing — "Keep both" in the UI, `RestoreModeMerge`).
  - Every restored row gets a fresh id (`row.Id = 0`, `Create`); foreign keys are
    remapped through per-run `map[oldID]newID` tables built as each parent section is
    restored (`roleIDs`, `userIDs`, `appIDs`, `authIDs`).
  - A child row whose parent id is **not present** in the remap table is **skipped**
    (counted in `RestoreResult.Skipped`), never restored with a dangling or
    reinterpreted foreign key — e.g. a permission whose role wasn't in the backup, a
    user whose role wasn't restored (lands role-less rather than silently inheriting
    whatever role now occupies that id on the new host), an MFA factor (or a WebAuthn
    credential) whose user wasn't restored, a redirect URI whose auth config wasn't
    restored, a group mapping whose role wasn't restored. A row whose parent was itself
    **skipped as a merge collision** (below) takes the same path — it is absent from the
    same remap table either way, so nothing re-parents onto the record that was already
    there.
  - **Restoring a child section without its parent section resolves the parent against
    this host instead of leaving it unmapped (`matchExistingRoles`,
    `matchExistingUsers`).** The UI lets an operator tick only `mfa` (or only
    `federation`) — "restore the second factors onto the accounts already here" is a
    reasonable request on its own. But `roleIDs`/`userIDs` were filled **only** by
    restoring the parent section, so an unselected parent left every child row unmapped
    and therefore skipped. In `replace` mode that was silently destructive: selecting
    only `mfa` wiped `user_mfa_factor` and every recovery code first (the section's own
    wipe), found no user mapping to place the backup's factors on, skipped every one,
    and returned `err=nil` — every account on the server lost its second factor, with
    the response reporting success. `federation` alone had the identical shape one
    level along (group mappings, which decide what role a directory login lands on,
    wiped and none restored, because `roleIDs` is only filled by restoring `access`).
    Confirmed against a live server before the fix. Now, in `Restore` before any
    section is dispatched: if `access` was **not** selected, `matchExistingRoles` reads
    every role already on this host and fills `roleIDs` by `AccessRole.Name`; if
    `identity` was **not** selected, `matchExistingUsers` does the same for `userIDs` by
    `UserLogin.Email` — the same natural keys the collision guard below uses. A child
    whose parent is absent from **both** the backup's selected sections and this host
    still skips — that is the case the skip was written for, unchanged.
  - **Merge-mode collision guard (`keyIndex`, `newKeyIndex`, `claim`, `normKey`).** Every
    table a section writes to carries a UNIQUE constraint on a natural key, and every
    myidsan install seeds the same stock role names and the same bootstrap admin email —
    so an unguarded merge restore of `access` or `identity` used to hit that constraint on
    its very first row and abort, surfacing the driver's own text
    (`UNIQUE constraint failed: access_role.name (2067)`) to the operator mid-recovery,
    with everything before the collision already written (the restore has no enclosing
    transaction). Each `restore*` function now builds a `keyIndex` per table **after** any
    replace-mode wipe (so replace mode is unaffected — the index starts empty and nothing
    is skipped) and calls `claim(normKey(...))` before every insert; a key already claimed
    is **skipped and counted** (`RestoreResult.Skipped`) rather than aborting, and the
    target's own row is left untouched — a backup must not overwrite a live account's
    password/role or an app's live client secret and redirect allow-list. Keys are claimed
    as rows are inserted (not just read once up front), so an archive carrying two records
    with the same key no longer aborts even in replace mode. `normKey` lower-cases and
    trims each part before joining, so `"Admin"` and `"admin"` collide the way an operator
    means them to, regardless of the database's own collation. Natural keys, one `keyIndex`
    per table: `AccessRole.Name`; `AccessRolePermission(RoleId, Path)`; `UserLogin.Email`;
    `UserGroup(Title, ParentId)`; `AppRegistry.Code` **and** `AppRegistry.Audience` as two
    separate indexes (either collision skips the row — the table carries two independent
    UNIQUE constraints); `AppAuthConfig(AppRegistryId, ClientId)`;
    `AppRedirectUri(AppAuthConfigId, RedirectUri)`; `DirectoryConfig.Name`;
    `FederatedGroupMapping(Provider, GroupName)`; `SsoCa.Name`; and, in `restoreMfa`,
    `UserMfaFactor(UserLoginId, Kind)`, `UserMfaRecoveryCode(UserLoginId, CodeHash)`, and
    `UserWebauthnCredential.CredentialId`. Those three exist for the case the parent-match
    above creates: the target account may already have its own factor of that kind, and
    `loadFactor` takes the first row it finds for `(UserLoginId, Kind)`, so a second would
    make which factor gates a login depend on insert order; `CredentialId` is globally
    unique and is what the browser presents to say which physical security key it is
    using. On a collision the live factor stands — it is the one actually on the
    account holder's phone or in their key — and the skip is counted.
  - A mid-restore insert error (any table, either mode) is wrapped by `restoreFailure`,
    which names the section that failed, lists every section already applied with its
    restored count, states plainly that the server is now **partially restored**, and
    tells the operator to re-run in `"replace"` mode to reach a known state — the
    underlying driver error is still wrapped (`%w`) for a bug report.
  - After every section is applied, every live session is dropped
    (`cache.Store.DeleteByPrefix("sso:session:")`) — a session token issued a moment
    earlier would still resolve to ids/roles that may no longer mean what they did,
    so `RestoreResult.SessionsInvalidated` is set and the caller is expected to sign the
    operator out immediately.
  - First-run setup is marked complete (`sharedservices.ISetupStateService.Complete`,
    `domain/shared/services`) best-effort, so a restored instance never re-shows the
    first-run wizard.
  - A `SchemaVersion` mismatch produces a non-fatal `RestoreResult.SchemaWarning` — the
    restore still proceeds best-effort.

## Notes

- `sessionCachePrefix = "sso:session:"` must stay in sync with the key
  `middlewares/auth.go` writes sessions under.
- `backupPageLimit = 100000` bounds each section's `repo.Get` page size — generous
  enough that a real deployment's row counts fit in one page, avoiding paging logic in
  export/wipe.
- `NewBackupService` takes every repo the six sections touch — including
  `dbsql.IGenericRepo[myidsanentities.UserWebauthnCredential]`, added alongside the
  existing MFA-factor/recovery-code repos in the `mfa` section's dependency slot — the
  `*atrest.Cipher` (nil when at-rest encryption is disabled — sealed columns are then
  already plaintext and pass through unchanged; WebAuthn credentials pass through this
  parameter's absence entirely, since they were never sealed to begin with),
  `cache.Store` (for session invalidation), `sharedservices.ISetupStateService`
  (`domain/shared/services` — the suite-wide seam this app used to carry its own copy
  of, see `domain/shared/services/setup_state.go.md`), and the app version string (see
  `apps/myidsan/app/app.go.md`'s `moduleAppVersion`).
- Wired in `apps/myidsan/app/app.go`'s `RegisterAppRoutes` and mounted via
  `apis.NewBackupApi` (`apis/backup.go.md`).
