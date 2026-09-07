# Module: apps/mymatasan/manual/manual_test.go

## Purpose

Runs the suite-wide manual conformance suite against mymatasan's shipped articles.

## Behavior

`TestManual(t *testing.T)` calls `manualcheck.Library(t, manual.Library)`
(`domain/shared/manual/manualcheck/check.go.md`), which asserts: every article has the
frontmatter the index and print table of contents need, every language folder holds the
same set of articles as English, every internal cross-link resolves to a real article, and
every hand-written `{#anchor}` heading id is unique and identical across all four
translations.

`TestManualUIReferences(t *testing.T)` calls
`manualcheck.UIReferences(t, manual.Library, "../views/react-webpack/src/views")`
(`domain/shared/manual/manualcheck/uirefs.go.md`) — the opposite direction from
`TestManual`. It walks every `.js` file under the SPA's `views/components` and confirms
each contextual `HelpButton`/`help:`/`TAB_HELP`/`STEP_HELP` target actually resolves
against the manual shipped above, currently checking 42 targets. A renamed article or a
dropped `{#anchor}` breaks nothing at build or run time — the "?" button just opens the
wrong page — so this is the only thing that catches it.

`TestManualSpecValues(t *testing.T)` calls `manualcheck.SpecValues` (`domain/shared/manual/
manualcheck/diagrams.go.md`) with a map naming the `control-plane` article's four `` ```spec ``
port rows (`control-plane/discovery`, `control-plane/mtls`, `control-plane/control`,
`control-plane/media`) against `infra/pairing.Default{Discovery,MTLS,Control,Media}Port`
(`infra/pairing/packet.go.md`). This is the first (only) app-level `SpecValues` caller in the
suite: it is what makes the manual's port table an assertion rather than a table someone typed
once and nobody re-checks.

## Notes

- Mirrors `apps/myiotsan/kb/kb_test.go` (`apps/myiotsan/kb/kb_test.go.md`), the
  single-language predecessor this suite's tests generalize.
- This is the test that actually enforces "every article exists in every language" for
  `apps/mymatasan/manual` — see the caution in `manual.go.md`.
- `TestManualUIReferences` is the only automated guard on mymatasan's contextual-help
  wiring; every new `HelpButton` call, `help:` tab entry, or `TAB_HELP`/`STEP_HELP` map
  update is checked here, not by hand.
- `TestManual`'s call into `manualcheck.Library` also runs the `Diagrams` subtest
  (`domain/shared/manual/manualcheck/diagrams.go.md`) over this app's shipped figures —
  two `` ```flow `` figures and one `` ```seq `` figure alongside the `` ```spec `` table
  `TestManualSpecValues` checks the values of — reporting "checked 4 figures across 4
  languages".
