# Module: apps/mymatasan/apis/backup.go

## Purpose

HTTP surface for the Settings → Backup & Recovery "Configuration backup" panel and the first-run wizard recovery step. Wraps `services.IBackupService`.

## Responsibilities

- `NewBackupApi(router, serv)` registers under `/settings/backup`:
  - `GET /sections` — available sections + current row counts (for the export UI).
  - `POST /export` — body `{sections[], passphrase}`; returns `{filename, dataBase64}` (the `.mmbackup` bytes, base64-encoded for the browser to download). Refuses a passphrase shorter than 8 characters.
  - `POST /preview` — body `{dataBase64, passphrase}`; returns the decrypted manifest without applying anything.
  - `POST /restore` — body `{dataBase64, passphrase, sections[], mode}`; applies the backup and returns the per-section restore report.

## Notes

- Uses the same base64-in-JSON envelope convention as the recovery-escrow endpoints (`apis/system.go`) rather than multipart uploads.
- Export/preview/restore are POSTs; the catalog in `apps/mymatasan/services/rbac.go` grants no viewer/operator permission under `/api/settings/backup`, so the router's `NewRequireRolePermission` middleware (deny-by-default) leaves them reachable by admins only — the file carries plaintext secrets and never leaves without a passphrase.
- Wired in `app.go` after `NewSystemApi`, reusing the repositories declared there and the resolved `currentVersion`.
