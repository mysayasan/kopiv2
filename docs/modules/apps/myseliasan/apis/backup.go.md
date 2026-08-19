# Module: apps/myseliasan/apis/backup.go

## Purpose

HTTP surface for the control plane's disaster-recovery bundle — export, inspect and restore a passphrase-encrypted `.selbackup`. Thin handlers over `services.IBackupService` (`services/backup.go.md`), plus the audit records that make "who took a copy of the fleet CA" answerable afterwards.

## Endpoints

Mounted under `/api/backup`, behind `auth.Middleware` + `session.Middleware`, every route wrapped in `requireSuper`:

- `GET /api/backup/sections` — section ids with current row counts, so the export UI can show what will be included and disable the empty ones.
- `POST /api/backup/export` — `{sections[], passphrase}` → `{filename, dataBase64}`. Passphrase must be at least `backupMinPassphraseLen` (8); the file carries the fleet CA private key in the clear inside the envelope, so there is deliberately no unprotected export.
- `POST /api/backup/preview` — `{dataBase64, passphrase}` → the manifest only, so an operator sees what a file holds before committing to a restore.
- `POST /api/backup/restore` — `{dataBase64, passphrase, sections[], mode}` → `RestoreResult`, including `restartRequired` when the fleet CA was applied.

## Notes

- **Superadmin, not merely admin.** An export is a complete copy of the fleet's trust root in one file; a restore can replace the node registry and every role in the system. Neither is an operator action. This mirrors `apis/system.go`'s gating of restart and factory reset, and uses the same `requireSuper` shape.
- **Upload cap is 64 MiB** (`backupMaxUploadBytes`), well above mymatasan's 16 MiB, because floor plan images ride inside the bundle and a site with a dozen scanned plans is normal.
- Export and restore are audited (`backup.export` / `backup.restore`, target type `backup`, section list in `Metadata`), on both success and failure. Restore is recorded even when it replaced the roles the actor was authorised under — the action must stay attributable to them. **The passphrase is never in the audit entry**: the metadata blob is readable by every superadmin, and the passphrase is the only protection on the exported CA key.
- `preview` is deliberately NOT audited: nothing changed and nothing left the building.
- Actor and client IP come from the shared `auditActor` / `clientIP` helpers in `apis/audit.go`, so attribution and proxy resolution match every other audited route.
