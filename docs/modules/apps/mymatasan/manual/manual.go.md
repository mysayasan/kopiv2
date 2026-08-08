# Module: apps/mymatasan/manual/manual.go

## Purpose

mymatasan's built-in user manual: the articles a reader sees under Help, compiled into the
binary so the documentation always matches the running software and always works with no
network access. Content-only package — everything about indexing, language fallback,
search, printing, and serving is shared (`domain/shared/manual`, `manual/manual.go.md`);
this package is the `//go:embed` plus the shipped markdown.

## Responsibilities

- `//go:embed en ms zh ar assets` into `var files embed.FS`, then `var Library =
  sharedmanual.New(files, ".")`. Loading is lazy (`sharedmanual.New` reads nothing until
  first query), so this costs nothing at package init.
- Ships six articles × four languages under `en/`/`ms/`/`zh/`/`ar/`: `10-welcome.md`,
  `20-first-sign-in.md`, `30-setup-wizard.md`, `40-restore-from-backup.md`,
  `50-workspace-tour.md`, `60-using-this-manual.md` (numeric prefixes are on-disk ordering
  only — stripped from the slug by `sharedmanual.Slug`). `assets/README.md` documents the
  (currently empty) figures folder.
- Registered as `GET /manual`, `/manual/bundle`, `/manual/{slug}`, `/manual/assets/{name}`
  by `apps/mymatasan/apis/manual.go` (`apps/mymatasan/apis/manual.go.md`) on the **public**
  router.

## Notes

- Adding a language means adding its folder here **and** to the `//go:embed` pattern.
  Adding an article means adding the file to **every** language folder —
  `apps/mymatasan/manual/manual_test.go` (`manual_test.go.md`) fails otherwise via
  `manualcheck.Library`'s `LanguageParity` check, which is the only reliable way a
  four-language manual stays four languages.
- First app to adopt `domain/shared/manual` — phase 1 of a suite-wide built-in manual, one
  app at a time.
