# Module: infra/versioning/changelog.go

## Purpose

Renders a `CHANGELOG.md` section (Keep a Changelog format) for one version-bump run and prepends it to the changelog file.

## Responsibilities

- `RenderChangelogEntry(changes, manifest, bumpedCore, bumpedApps, commit, now)` — builds one dated `## ` section: a heading listing every component whose version changed (apps sorted alphabetically, `core` last) plus the short commit hash, followed by the change summaries grouped under `### Added`/`Changed`/`Deprecated`/`Removed`/`Fixed`/`Security` subheadings (render order fixed by `changelogGroups`), each rendered as `- **<app-or-scope>**: <summary>`. Returns `""` when `changes` is empty.
- `changelogGroup(change)` — maps a change's `type` to its Keep-a-Changelog section: `added`→Added, `deprecated`→Deprecated, `removed`→Removed, `fixed`/`fix`→Fixed, `security`→Security; everything else (including bare `major`/`minor`/`patch` levels, `docs`, `cleanup`, `refactor`) falls into `Changed` so nothing is silently dropped.
- `changeScopeLabel(change)` — the bold prefix before a change's summary: prefers `change.App`, then `change.Scope`, defaulting to `"core"`.
- `shortCommit(commit)` — truncates a commit SHA to 7 characters for the heading.
- `PrependChangelogEntry(path, entry)` — inserts `entry` at the top of the changelog file, right after the `# Changelog` title line, so the newest release always leads. Creates the file with a standard header (`changelogIntro`) when it doesn't exist yet. If the existing file doesn't start with `# Changelog` (e.g. empty/new), it writes the header, the new entry, then the prior contents (if any) beneath.

## Notes

- Heading format: `## YYYY-MM-DD — app1 1.2.3, app2 4.5.6, core 7.8.9 (abcdef1)` — the date/version/commit portions are all optional in isolation (an entry with no bumped components still renders a bare dated heading if `changes` is non-empty).
- `now` defaults to `time.Now().UTC()` when zero-valued (tests pass a fixed time for determinism).
- Called from `ApplyPendingChanges` (`bump.go`) only when `ApplyOptions.ChangelogPath` is non-empty, before pending change files are moved to `applied`.
