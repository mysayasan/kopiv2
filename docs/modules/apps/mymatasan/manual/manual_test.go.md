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

## Notes

- Mirrors `apps/myiotsan/kb/kb_test.go` (`apps/myiotsan/kb/kb_test.go.md`), the
  single-language predecessor this suite's tests generalize.
- This is the test that actually enforces "every article exists in every language" for
  `apps/mymatasan/manual` — see the caution in `manual.go.md`.
- `TestManualUIReferences` is the only automated guard on mymatasan's contextual-help
  wiring; every new `HelpButton` call, `help:` tab entry, or `TAB_HELP`/`STEP_HELP` map
  update is checked here, not by hand.
