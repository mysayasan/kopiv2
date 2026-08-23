# Module: apps/mymatasan/app/wire_storage.go

## Purpose

`repos` gathers every repository the app persists through into one struct, and `newRepos`
builds them all in one call. Moved out of `app.go` (Tier 2 phase D2), where the 16
repositories were previously constructed in three scattered clumps through the function.

## Responsibilities

- `type repos struct` — one `dbsql.IGenericRepo[T]` field per app entity (`Camera`,
  `CameraOnvif`, `DetectionRule`, `DetectionClass`, `AlertEvent`, `ObjectObservation`,
  `ObjectAppearance`, `RecordingConfig`, `RecordingSegment`, `TrainingDataset`, `TrainingImage`,
  `TrainingModel`, `TeachSkill`, `FacePerson`, `FaceEmbedding`, `RuntimeSetting`,
  `LocalUser`) plus the two shared-domain tables this app also owns rows in
  (`Notification`, `NotificationRollup`). `FacePerson`/`FaceEmbedding` back the global
  face-recognition gallery (see `entities/face_person.go.md`/`entities/face_embedding.go.md`
  and `services/face_gallery.go.md`) — added alongside `TeachSkill`, bringing the total to
  18 repositories. `ObjectAppearance` (W3-2 — one appearance descriptor per sighting, see
  `entities/object_appearance.go.md`) brings it to 19, and backs `services.AppearanceService`
  (`services/appearance_search.go.md`).
- `newRepos(db dbsql.IDbCrud) repos` — constructs every field via
  `dbsql.NewGenericRepo[T](db)` against the bootstrapped database.

## Notes

- Gathering these in one place means the app's whole persistence surface is one screen and
  lines up 1:1 with `module.Entities()` — the list bootstrap derives the schema from.
- Pure move from `app.go`; no behavior change, no new repositories. Every call site that
  previously held a local `xxxRepo` variable now reads `repo.Xxx` off the returned struct
  — see `docs/modules/apps/mymatasan/app/app.go.md`.
