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

After the workflow bumps `infra/versioning/version.json`, processed folders are moved from `changes/pending/` to `changes/applied/`.
