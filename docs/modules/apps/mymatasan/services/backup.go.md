# Module: apps/mymatasan/services/backup.go

## Purpose

Produces and restores a portable, passphrase-encrypted snapshot of the app's **configuration** so a fresh install can be brought up without reconfiguring cameras, AI rules, notification destinations, or settings. It deliberately captures the `json:"-"` secrets the normal API strips (camera ONVIF password, TalkPassword) — safe only because the whole file is encrypted with a user passphrase (see `infra/atrest.EncryptWithPassphrase`, which is portable, not machine-bound).

Machine identity (the at-rest key, node pairing, certificates, `config.json`, the setup-complete flag) is intentionally **never** included; cloning it onto a second host would duplicate credentials. A restore lets those regenerate.

## Responsibilities

- `IBackupService` / `NewBackupService(...)` — spans the camera, camera-ONVIF, recording-config, detection-class, detection-rule, and runtime-setting repositories, plus the app version (stamped into the manifest).
- Sections (independently selectable): `cameras` (camera + camera_onvif incl. creds + recording_config), `ai` (detection_class registry + detection_rule), `notifications` (`runtime_setting["notification"]`), `settings` (`runtime_setting` keys `runtime`, `health`, `machineHealth`).
- `AvailableSections(ctx)` — per-section row counts for the export UI.
- `Export(ctx, {sections, passphrase})` — collects the selected sections into a `backupFile`, marshals to JSON, and encrypts with the passphrase. File framing: magic `MMBK` + 1-byte format version + passphrase-encrypted payload. `backupCamera`/`backupCameraOnvif` embed the entity and add a shadow field to re-expose the `json:"-"` secret so it lands in the (encrypted) file.
- `Preview(ctx, data, passphrase)` — decrypts only the manifest (app version, sections, counts) so the UI/wizard can show what a file contains before applying.
- `Restore(ctx, data, {sections, passphrase, mode})` — decrypts, intersects requested sections with those present, and applies them. Returns a per-section `restored`/`skipped` report plus any schema-version warning.

## Restore semantics

- **FK remap is mandatory**: `Create` ignores the supplied `Id` (`skipWhenInsert`), so cameras are inserted first to build an old→new id map; camera_onvif, recording_config and detection_rule are re-pointed to the new camera id. A detection rule whose camera was not restored in the same operation is skipped and counted (rather than left dangling).
- **Detection classes** are upsert-by-name (the registry is a global, name-keyed lookup shared with the built-ins this host seeds on boot), never wiped.
- **Runtime-setting sections** are upsert-by-key.
- **Modes**: `replace` wipes the selected sections' tables first, then inserts; `merge` appends. `replace` is the default.

## Notes

- No database schema changes — everything already exists as rows/key-values.
- Reuses the existing package-level `containsString` (in `notification.go`) and `isNoResultFoundErr` (in `camera.go`).
- Tests in `backup_test.go` cover a full round-trip (secrets preserved + IDs remapped), replace-mode wiping, and rejection of a wrong passphrase / non-backup bytes / missing passphrase / empty selection, using in-memory fake repositories.
