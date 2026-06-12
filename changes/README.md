# Version Changelog Entries

GitHub Actions consumes pending version changelog entries from this folder when changes land on `main`.

Create one folder per version change:

```text
changes/pending/YYYYMMDD-HHMMSS-short-title/change.json
```

Example:

```json
{
  "level": "minor",
  "scope": "both",
  "app": "mymatasan",
  "summary": "Add shared runtime version endpoint"
}
```

Fields:

- `level`: `major`, `minor`, or `patch`.
- `scope`: `core`, `app`, or `both`.
- `app`: required when `scope` is `app` or `both`.
- `summary`: short human-readable change note.
- `compatibility`: optional note for operator-facing compatibility or migration detail.

The version bump tool also supports the newer multi-target shape:

```json
{
  "type": "minor",
  "scope": "core,myidsan,myseliasan",
  "summary": "Add browser SSO flow"
}
```

`type` can be `major`, `minor`, `patch`, or a mapped change kind such as `fixed`, `docs`, `cleanup`, `refactor`, `added`, `changed`, `removed`, `deprecated`, or `security`.
Comma-separated `scope` values can include core aliases (`core`, `shared`, `apphost`, `infra`, `domain`, `bootstrap`, `config`) and app names from `infra/versioning/version.json`.
Documentation-only tokens such as `docs`, `readme`, `tests`, and `changelog` are accepted and ignored for version-target selection.

After the workflow bumps `infra/versioning/version.json`, processed folders are moved from `changes/pending/` to `changes/applied/`.
