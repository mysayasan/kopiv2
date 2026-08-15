# Module: apps/myseliasan/manual/manual.go

## Purpose

myseliasan's built-in user manual: the articles a reader sees under **Help**, compiled
into the binary so the documentation always matches the running control plane and works
even though myseliasan (and its myidsan hop) has no egress by design. Content-only
package — everything about indexing, language fallback, search, printing, and serving is
shared (`domain/shared/manual`, `domain/shared/manual/manual.go.md`); this package is the
`//go:embed` plus the shipped markdown. Second app to adopt the shared library, after
`apps/mymatasan/manual` (`apps/mymatasan/manual/manual.go.md`).

## Responsibilities

- `//go:embed en ms zh ar assets` into `var files embed.FS`, then `var Library =
  sharedmanual.New(files, ".")`. Loading is lazy (`sharedmanual.New` reads nothing until
  first query), so this costs nothing at package init.
- Ships **6 articles × four languages** (24 files) across two categories: **getting
  started** (`welcome`, `first-sign-in`, `setup-wizard`, `workspace-tour`,
  `using-this-manual`) and **fleet** (`adopting-nodes`). A deliberately smaller,
  fleet-scoped book than mymatasan's 36-article manual — myseliasan's operator surface is
  narrower (sign-in, the setup wizard, the workspace, and adopting nodes), not a gap.
  `assets/README.md` documents the (currently empty) figures folder.
- Registered as `GET /api/manual`, `/api/manual/bundle`, `/api/manual/{slug}`,
  `/api/manual/assets/{name}` by `apps/myseliasan/apis/manual.go`
  (`apps/myseliasan/apis/manual.go.md`) on the **bare** router, before any auth
  middleware.

## Notes

- Adding a language means adding its folder here **and** to the `//go:embed` pattern.
  Adding an article means adding the file to **every** language folder —
  `apps/myseliasan/manual/manual_test.go` (`manual_test.go.md`) fails otherwise via
  `manualcheck.Library`'s `LanguageParity` check, which is the only reliable way a
  four-language manual stays four languages.
- Every contextual "?" button in the SPA (`views/react-webpack/src/views/components/*`,
  and the pre-session screens in `components/auth_screens.js`) targets one of these
  slugs, optionally with a `{#anchor}` heading id. `manualcheck.UIReferences`
  (`domain/shared/manual/manualcheck/uirefs.go.md`), driven by `TestManualUIReferences`
  in `manual_test.go.md`, scans the frontend source and fails if a button points at an
  article or anchor that does not exist here — currently checking 19 targets. That is
  what keeps this folder and the UI's help wiring from drifting apart silently.
