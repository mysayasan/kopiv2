# Module: apps/myseliasan/manual/manual_test.go

## Purpose

Runs the suite-wide manual conformance suite against myseliasan's shipped articles —
the same test shape `apps/mymatasan/manual/manual_test.go`
(`apps/mymatasan/manual/manual_test.go.md`) runs for its own, larger book.

## Behavior

`TestManual(t *testing.T)` calls `manualcheck.Library(t, manual.Library)`
(`domain/shared/manual/manualcheck/check.go.md`), which asserts: every article has the
frontmatter the index and print table of contents need, every language folder holds the
same set of articles as English, every internal cross-link resolves to a real article,
and every hand-written `{#anchor}` heading id is unique and identical across all four
translations.

`TestManualUIReferences(t *testing.T)` calls
`manualcheck.UIReferences(t, manual.Library, "../views/react-webpack/src/views")`
(`domain/shared/manual/manualcheck/uirefs.go.md`) — the opposite direction from
`TestManual`. It walks every `.js` file under the SPA's `views` tree and confirms each
contextual `HelpButton`/`help:`/`TAB_HELP`/`SETUP_STEP_HELP` target actually resolves
against the manual shipped above, currently checking 19 targets. A renamed article or a
dropped `{#anchor}` breaks nothing at build or run time — the "?" button just opens the
wrong page — so this is the only thing that catches it.

## Notes

- Mirrors `apps/mymatasan/manual/manual_test.go` (`apps/mymatasan/manual/manual_test.go.md`),
  the four-language predecessor this suite's tests generalize; both run the identical
  `manualcheck` package against different-sized libraries.
- This is the test that actually enforces "every article exists in every language" for
  `apps/myseliasan/manual` — see the caution in `manual.go.md`.
- `TestManualUIReferences` is the only automated guard on myseliasan's contextual-help
  wiring; every new `HelpButton` call, `TAB_HELP` entry, or `SETUP_STEP_HELP` anchor list
  update is checked here, not by hand.
