# Module: apps/myidsan/apis/backup.go

## Purpose

Superadmin-only HTTP surface over `services.IBackupService` (`services/backup.go.md`):
export an encrypted `.idbackup` archive of the identity store, preview one before
committing, or restore it — disaster recovery for the one app whose loss locks the whole
suite out at once.

## Routes

`NewBackupApi` mounts `/backup`, gated `auth.Middleware` + `access.Middleware` +
`access.RequireSuperadmin` — **not** delegatable through the RBAC permission matrix even
though the menu row itself (`apps/myidsan/app/app.go`'s `Seeders`) has no `SeedRbac`.
An export hands the caller the entire identity store in one file and a restore rewrites
every account and role in it — strictly more power than any individual admin endpoint
grants, so it sits with the other privilege-affecting surfaces (`user-credential`,
`mfa-admin`, `password-reset`).

- `GET /api/backup/sections` (`sections`) — row counts per section
  (`IBackupService.AvailableSections`), for the export screen to show what is about to
  be included.
- `POST /api/backup/export` (`export`) — body `{sections, passphrase}`. Refuses a
  passphrase shorter than `backupMinPassphraseLen` (12) with `ErrBadRequest` before
  calling the service. Returns `{filename: "myidsan-backup-<timestamp>.idbackup",
  dataBase64}` — the caller decodes and downloads the bytes client-side.
- `POST /api/backup/preview` (`preview`) — body `{dataBase64, passphrase}`. Decrypts
  only the manifest (`IBackupService.Preview`) so the operator can confirm app version,
  sections, and row counts before committing to a restore that replaces live accounts.
  Writes nothing.
- `POST /api/backup/restore` (`restore`) — body `{dataBase64, passphrase, sections,
  mode}`. Calls `IBackupService.Restore` and returns the `RestoreResult` (restored/
  skipped counts per section, `sessionsInvalidated`, `setupCompleted`). The caller's own
  session is invalidated along with everyone else's, so the SPA must treat a successful
  restore response as an immediate sign-out rather than continuing to poll with the
  now-dead cookie.

## Limits

- `backupMinPassphraseLen = 12` — the passphrase is the only thing standing between the
  file and every password hash, TOTP secret, LDAP bind password, and the SSO CA private
  key, so a trivially short one is refused outright.
- `backupMaxUploadBytes = 64 * 1024 * 1024` caps a restore/preview upload
  (`http.MaxBytesReader`) — the archive is configuration, not media; a directory of
  100k users still lands far inside this. The `export` request body itself is capped
  much lower (64 KB) since it only carries section selection and a passphrase, not file
  data.

## Notes

- `dataBase64` round-trips the encrypted archive as base64 inside a JSON body rather
  than a raw multipart upload, matching the size caps above and keeping the handler
  symmetric with `export`'s response shape.
- Mounted from `apps/myidsan/app/app.go`'s `RegisterAppRoutes`, alongside the other
  privilege-affecting admin surfaces.
