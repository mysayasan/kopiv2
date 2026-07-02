# Module: infra/versioning/bump.go

## Purpose

Applies pending changelog entries to the version manifest.

## Responsibilities

- Reads JSON change files from `changes/pending`.
- Supports `level` values `major`, `minor`, and `patch`.
- Supports `scope` values `core`, `app`, and `both`.
- Bumps core and/or app SemVer values.
- Writes `infra/versioning/version.json`.
- When `ApplyOptions.ChangelogPath` is set, renders and prepends a dated `CHANGELOG.md` entry (`RenderChangelogEntry` + `PrependChangelogEntry`, `changelog.go`) for the changes applied this run, before moving them to `applied`.
- Moves processed changelog folders to `changes/applied`.

## Notes

- App version entries are created from `0.0.0` when a new app appears in a pending app-scoped change.
- The workflow stores commit SHA and update time in the manifest.
- `ApplyChange` now returns the resolved `changeTargetSet` (which core/apps it bumped) alongside the error, and `ApplyResult.AppliedChanges` carries every consumed `Change` in file order — both feed the changelog heading (bumped component list) and grouped summaries.
- An empty or unset `ChangelogPath` skips changelog writing entirely (only the manifest and `changes/applied` move happen) — the default CLI flag is `CHANGELOG.md`, but callers can pass `-changelog ""` to opt out.
