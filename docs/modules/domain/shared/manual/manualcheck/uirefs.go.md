# Module: domain/shared/manual/manualcheck/uirefs.go

## Purpose

The other half of the conformance suite in this package: `check.go`'s `Library` verifies a
manual is internally consistent; `UIReferences` verifies an app's frontend agrees with it.
A contextual "?" button names an article slug and a heading anchor as bare strings in JSX —
renaming the article file or dropping the `{#anchor}` from a heading breaks nothing at
compile or run time, so the deep link just opens the wrong page (or the right page scrolled
to the top) until somebody notices by hand. This exists so that fails a test instead.

## Key Function: UIReferences

```go
func UIReferences(t *testing.T, lib *manual.Library, root string)
```

Called from an app's own test alongside `Library`, e.g.
`apps/mymatasan/manual/manual_test.go` (`apps/mymatasan/manual/manual_test.go.md`):

```go
func TestManualUIReferences(t *testing.T) {
    manualcheck.UIReferences(t, manual.Library, "../views/react-webpack/src/views")
}
```

`root` is the frontend source directory to walk (relative to the calling test). It recurses
into every `.js` file and, per file, regex-matches four declared shapes a contextual help
target can take:

- **JSX**: `<HelpButton slug="camera-properties" anchor="stream" />`.
- **Tab-table field**: `help: ['settings-reference', 'runtime']` inside an array-of-objects
  tab definition (e.g. `SETTINGS_TABS`).
- **`const TAB_HELP = { … }` map**: `dashboard: ['dashboard', ''],` entries keyed by tab id
  (e.g. `components/layout.js`).
- **`const STEP_HELP = { slug, anchors: [...] }` block**: a stepped surface (the setup
  wizard) that declares one article slug and an index-aligned anchor list. It must be
  declared as data, not computed (`ANCHORS[step]`), specifically so this scan can see it —
  see the comment on `STEP_HELP` in `components/setup.js`.

Every match becomes a `(slug, anchor, file)` reference. Each reference's `slug` must name an
article in `lib.Bundle(manual.DefaultLanguage)`; if `anchor` is non-empty it must also be one
of that article's `{#anchor}` heading ids (computed via the same `articleAnchors` helper
`check.go`'s `anchors` check uses, so the two stay consistent with each other).

If the walk finds **zero** references at all, the test fails outright rather than passing
vacuously — a change to how `HelpButton`/`help:`/`TAB_HELP`/`STEP_HELP` is written (e.g. a
refactor to a different prop name) would otherwise silently turn this into a no-op guard
that never catches anything again.

## Notes

- The three "shapes" are recognized narrowly on purpose (see the doc comment on the regex
  vars): a shape this scanner doesn't know about must fail loudly, not be skipped, because a
  silently-skipped shape is exactly how a dead deep link ships.
- Reuses `articleAnchors` from `check.go` (same package) rather than re-deriving anchors, so
  `Library`'s `Anchors` check and this check can never disagree about what counts as a valid
  anchor.
- First consumer: `apps/mymatasan/manual/manual_test.go`
  (`apps/mymatasan/manual/manual_test.go.md`), currently checking 42 contextual-help
  targets across the SPA's `views/components/*`.
