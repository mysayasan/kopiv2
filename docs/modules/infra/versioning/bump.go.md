# Module: infra/versioning/bump.go

## Purpose

Applies pending changelog entries to the version manifest.

## Responsibilities

- Reads JSON change files from `changes/pending`.
- Supports `level` values `major`, `minor`, and `patch`.
- Supports `scope` values `core`, `app`, and `both`.
- Bumps core and/or app SemVer values.
- Writes `infra/versioning/version.json`.
- Moves processed changelog folders to `changes/applied`.

## Notes

- App version entries are created from `0.0.0` when a new app appears in a pending app-scoped change.
- The workflow stores commit SHA and update time in the manifest.
