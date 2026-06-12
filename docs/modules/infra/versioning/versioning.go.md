# Module: infra/versioning/versioning.go

## Purpose

Loads and validates the embedded runtime version manifest.

## Responsibilities

- Embeds `infra/versioning/version.json` into the Go binary.
- Validates strict `major.minor.patch` SemVer values.
- Stores separate core and app version entries.
- Returns a client-facing version payload for the selected running app only.

## Notes

- The public API does not expose the internal `apps` map.
- Missing app version entries are treated as configuration errors.
