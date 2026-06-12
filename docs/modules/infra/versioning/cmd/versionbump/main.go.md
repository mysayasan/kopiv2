# Module: infra/versioning/cmd/versionbump/main.go

## Purpose

Command-line entrypoint used by GitHub Actions to update runtime versions.

## Behavior

- Runs `infra/versioning.ApplyPendingChanges`.
- Defaults to:
  - manifest: `infra/versioning/version.json`
  - pending changes: `changes/pending`
  - applied changes: `changes/applied`
- Stores `GITHUB_SHA` as the manifest commit when no explicit `-commit` value is provided.

## Notes

- Intended to run on pushes to `main`.
- Prints a no-op message when there are no pending changelog entries.
