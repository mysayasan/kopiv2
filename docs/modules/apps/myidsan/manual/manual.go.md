# Module: apps/myidsan/manual/manual.go

## Purpose

myidsan's built-in user manual: the articles a reader sees under Help, compiled into the
binary so the documentation always matches the running software and always works with no
network access. Content-only package — everything about indexing, language fallback,
search, printing, and serving is shared (`domain/shared/manual`, `manual/manual.go.md`);
this package is the `//go:embed` plus the shipped markdown. Before this, myidsan shipped
no manual at all — it is the third app in the suite (after mymatasan and myseliasan) to
adopt `domain/shared/manual`.

## Responsibilities

- `//go:embed en ms zh ar assets` into `var files embed.FS`, then `var Library =
  sharedmanual.New(files, ".")`. Loading is lazy (`sharedmanual.New` reads nothing until
  first query), so this costs nothing at package init.
- Ships 9 articles × four languages under `en/`/`ms/`/`zh/`/`ar/`, grouped into four
  categories (numeric prefixes are on-disk ordering only — stripped from the slug by
  `sharedmanual.Slug`):
  - **getting-started**: `welcome` (what myidsan is), `first-sign-in` (the generated
    bootstrap password and the screens that can hold a fresh install up).
  - **federation**: `connecting-an-app` (registering a relying app, the four things it
    needs, and the authorize/code/token hop it makes).
  - **people**: `users-roles-groups` (a new account holds no role until cleared),
    `directory` (LDAP/Active Directory login — what myidsan takes from the directory and
    what it does not).
  - **security**: `second-factor` (TOTP, security keys, recovery codes, account
    recovery), `sessions-and-step-up` (the cache is the session; the table is only an
    index), `audit-log` (the append-only trail, superadmin-only).
  - **operations**: `backup-restore` (the `.idbackup` file and what a restore replaces).
  - `assets/README.md` documents the (currently empty) figures folder — the same
    two rules as the sibling apps: figures earn their place, and nothing loads from the
    network.
- Four hand-drawn figures, one apiece: a `` ```seq `` sequence diagram in
  `connecting-an-app` (the authorize/code/token hop), a `` ```flow `` diagram in
  `first-sign-in` (the screens a fresh sign-in can land on), a `` ```flow `` diagram in
  `second-factor` (enrol/challenge/recover), and an `` ```arch `` diagram in
  `sessions-and-step-up` (cache-is-authority, table-is-index) — plus three `` ```spec ``
  reference tables (`first-sign-in`'s sign-in policy, `connecting-an-app`'s three token
  lifetimes, `audit-log`'s action vocabulary), all asserted by
  `manual_test.go`'s `TestManualSpecValues` (`manual_test.go.md`) rather than typed once
  and left to drift.
- Registered as `GET /manual`, `/manual/bundle`, `/manual/search`, `/manual/{slug}`,
  `/manual/assets/{name}` by `apps/myidsan/apis/manual.go`
  (`apps/myidsan/apis/manual.go.md`) on the **public** router.

## Notes

- Adding a language means adding its folder here **and** to the `//go:embed` pattern.
  Adding an article means adding the file to **every** language folder —
  `apps/myidsan/manual/manual_test.go` (`manual_test.go.md`) fails otherwise via
  `manualcheck.Library`'s `LanguageParity` check, which is the only reliable way a
  four-language manual stays four languages.
- Third app to adopt `domain/shared/manual`, after `apps/mymatasan/manual`
  (`apps/mymatasan/manual/manual.go.md`) and `apps/myseliasan/manual`
  (`apps/myseliasan/manual/manual.go.md`).
- Every contextual "?" button in the SPA (`views/react-webpack/src/views/App.js`'s
  `TAB_HELP` map and `LoginHelpLink`/`HelpButton` call sites, and
  `views/components/setup.js`'s `STEP_HELP` array) targets one of these slugs, optionally
  with a `{#anchor}` heading id. `manualcheck.UIReferences`
  (`domain/shared/manual/manualcheck/uirefs.go.md`), driven by `TestManualUIReferences`
  in `manual_test.go.md`, scans the frontend source and fails if a button points at an
  article or anchor that does not exist here — currently checking 27 targets. myidsan is
  the one app in the suite whose four pre-session screens (sign-in, forced password
  change, MFA enrolment, pending-clearance) each carry their own `LoginHelpLink`, on top
  of the workspace header's per-tab "?" (`TAB_HELP`) and the setup wizard's per-step one
  (`STEP_HELP`).
