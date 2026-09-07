# Module: apps/myidsan/manual/manual_test.go

## Purpose

Runs the suite-wide manual conformance suite against myidsan's shipped articles.

## Behavior

`TestManual(t *testing.T)` calls `manualcheck.Library(t, manual.Library)`
(`domain/shared/manual/manualcheck/check.go.md`), which asserts: every article has the
frontmatter the index and print table of contents need, every language folder holds the
same set of articles as English, every internal cross-link resolves to a real article, and
every hand-written `{#anchor}` heading id is unique and identical across all four
translations. Its `Diagrams` subtest also runs here, over this app's four figures
(one `` ```seq ``, two `` ```flow ``, one `` ```arch ``) and three `` ```spec `` tables —
currently reporting "checked 7 figures across 4 languages".

`TestManualUIReferences(t *testing.T)` calls
`manualcheck.UIReferences(t, manual.Library, "../views/react-webpack/src/views")`
(`domain/shared/manual/manualcheck/uirefs.go.md`) — the opposite direction from
`TestManual`. It walks every `.js` file under the SPA's `views` tree and confirms each
contextual help target — `views/App.js`'s `TAB_HELP` map and `LoginHelpLink`/`HelpButton`
call sites, and `views/components/setup.js`'s `STEP_HELP` array — actually resolves
against the manual shipped above, currently checking 27 targets. A renamed article or a
dropped `{#anchor}` breaks nothing at build or run time — the "?" button just opens the
wrong page, or the wrong step of it — so this is the only thing that catches it.

`TestManualSpecValues(t *testing.T)` calls `manualcheck.SpecValues`
(`domain/shared/manual/manualcheck/diagrams.go.md`) with a map naming three articles'
`` ```spec `` rows against the values the software actually uses:

- `first-sign-in/*` — the shipped sign-in policy, read via
  `config.LoginSecurityConfigModel{}.Effective()` and
  `config.PasswordPolicyConfigModel{}.Effective()` (the zero-value `Effective()` calls are
  deliberate: they resolve exactly the way an install with no such block in `config.json`
  resolves, which is what the article describes).
- `connecting-an-app/{code,token,session}` — the three lifetimes a relying app is
  configured against, against `apis.DefaultAuthCodeTTLSeconds`,
  `apis.DefaultAccessTokenTTLSeconds`, `apis.DefaultFederatedSessionTTLSeconds`
  (`apis/federated_auth.go.md`) — exported from that file specifically so this table could
  assert against them rather than repeat them as prose.
- `audit-log/*` — the action vocabulary an investigator filters by, against the
  `services.Action*` constants (`services/audit.go.md`).

An integrator, an operator, and an investigator each act on one of these tables without
checking it first; the rule for this manual is that a value goes in a `spec` row only if
it can be asserted here.

## Notes

- Mirrors `apps/mymatasan/manual/manual_test.go` (`apps/mymatasan/manual/manual_test.go.md`)
  and `apps/myseliasan/manual/manual_test.go`
  (`apps/myseliasan/manual/manual_test.go.md`), the two predecessors this app's manual
  followed.
- This is the test that actually enforces "every article exists in every language" for
  `apps/myidsan/manual` — see the caution in `manual.go.md`.
- `TestManualUIReferences` is the only automated guard on myidsan's contextual-help
  wiring; every new `HelpButton` call, `LoginHelpLink`, or `TAB_HELP`/`STEP_HELP` entry is
  checked here, not by hand. The comment above `STEP_HELP` in `setup.js` warns that the
  scanner reads raw source text, so even a *written example* of the pattern inside a code
  comment is picked up as a real reference and fails the test.
