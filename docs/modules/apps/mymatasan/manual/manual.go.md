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
- Ships 36 articles × four languages under `en/`/`ms/`/`zh/`/`ar/`, grouped into eight
  categories (numeric prefixes are on-disk ordering only — stripped from the slug by
  `sharedmanual.Slug`): **getting-started** (`welcome`, `first-sign-in`, `setup-wizard`,
  `restore-from-backup`, `workspace-tour`, `using-this-manual` — phase 1), **daily-use**
  (`dashboard`, `live-views`, `notifications`, `recordings`, `object-search`, `people`),
  **cameras** (`adding-cameras`, `camera-properties`, `camera-health`,
  `onvif-management`), **detection** (`how-detection-works`, `detection-rules`,
  `object-classes`, `teach-mode`, `fire-smoke-and-plates`, `training-models`),
  **recording** (`recording-configuration`, `storage-and-capacity`), **notifications**
  (`notification-destinations`), **administration** (`users-and-roles`,
  `settings-reference`, `encryption-at-rest`, `backup-and-restore`,
  `updates-and-restart`, `secure-wipe-and-reset`, `control-plane`, `machine-health`), and
  **appendix** (`troubleshooting`, `faq`, `glossary`). `assets/README.md` documents the
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
- Every contextual "?" button in the SPA (`views/react-webpack/src/views/components/*`)
  targets one of these slugs, optionally with a `{#anchor}` heading id.
  `manualcheck.UIReferences` (`domain/shared/manual/manualcheck/uirefs.go.md`), driven by
  `TestManualUIReferences` in `manual_test.go.md`, scans the frontend source and fails if a
  button points at an article or anchor that does not exist here — that is what keeps this
  folder and the UI's help wiring from drifting apart silently.
