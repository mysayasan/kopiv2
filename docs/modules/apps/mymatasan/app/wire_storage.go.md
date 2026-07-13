# Module: apps/mymatasan/app/wire_storage.go

## Purpose

`repos` gathers every repository the app persists through into one struct, and `newRepos`
builds them all in one call. Moved out of `app.go` (Tier 2 phase D2), where the 16
repositories were previously constructed in three scattered clumps through the function.

## Responsibilities

- `type repos struct` — one `dbsql.IGenericRepo[T]` field per app entity (`Camera`,
  `CameraOnvif`, `DetectionRule`, `DetectionClass`, `AlertEvent`, `ObjectObservation`,
  `RecordingConfig`, `RecordingSegment`, `TrainingDataset`, `TrainingImage`,
  `TrainingModel`, `TeachSkill`, `RuntimeSetting`, `LocalUser`) plus the two
  shared-domain tables this app also owns rows in (`Notification`, `NotificationRollup`).
- `newRepos(db dbsql.IDbCrud) repos` — constructs every field via
  `dbsql.NewGenericRepo[T](db)` against the bootstrapped database.

## Notes

- Gathering these in one place means the app's whole persistence surface is one screen and
  lines up 1:1 with `module.Entities()` — the list bootstrap derives the schema from.
- Pure move from `app.go`; no behavior change, no new repositories. Every call site that
  previously held a local `xxxRepo` variable now reads `repo.Xxx` off the returned struct
  — see `docs/modules/apps/mymatasan/app/app.go.md`.
