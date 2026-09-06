# Module: domain/shared/manual/manualcheck/check.go

## Purpose

Exported conformance suite each app runs against its own manual (`manual.Library`) so a
four-language, cross-linked, deep-linkable manual doesn't silently rot. Lives in its own
package — rather than a `_test.go` file inside `domain/shared/manual` — so an app can call
it from its own test without pulling `testing` into the shipped `manual` package.

## Key Function: Library

```go
func Library(t *testing.T, lib *manual.Library)
```

Called from an app's own test, e.g. `apps/mymatasan/manual/manual_test.go`:

```go
func TestManual(t *testing.T) { manualcheck.Library(t, manual.Library) }
```

Fails immediately if the library ships no language folders, or if `DefaultLanguage`
(`"en"`) is not among them — every other language falls back to it, so its absence would
make every other check meaningless. Otherwise runs five subtests:

- **Metadata** — every article that actually belongs to a language (not an English
  fallback) must have a non-empty `Title`, `Category`, `CategoryLabel`, `Summary`, `Body`,
  and a non-zero `Order`; a slug's `Category` must agree across every language it's
  translated into, since grouping is keyed on that id.
- **LanguageParity** — every non-English language folder must hold exactly the same slugs
  as English (no more, no fewer). This is the check for the way a manual actually rots:
  someone adds an English article and stops: the loader's fallback means nothing breaks at
  runtime, so nobody notices for months without this test.
- **Links** — every internal markdown link (`[text](slug)` or `[text](slug#anchor)`,
  excluding `://`/`mailto:` externals and same-page `#anchor`-only links) must target a
  slug that exists in the English article set. A renamed file otherwise leaves a link that
  silently does nothing when clicked.
- **Anchors** — every hand-written `{#anchor}` heading id (`## Credentials {#credentials}`)
  must be unique within its article, and the set of anchors in a non-English article must
  be byte-identical to the same slug's English anchors. A contextual "?" button deep-links
  to one of these ids; a translator dropping or renaming one would make the deep link land
  at the top of the page in that language only — the check exists because that bug is
  otherwise the hardest one on this list to notice by hand.
- **Diagrams** — every ```` ```flow ````/```` ```arch ````/```` ```seq ````/```` ```spec ````
  fence (`domain/shared/manual/diagram`, `diagram/diagram.go.md`) must parse, every node's
  `=> slug#anchor` link must resolve the same way a contextual "?" button's does, and every
  non-English copy of a figure must fingerprint identically to the English original —
  same nodes, same edges, same links, same spec values, in any order. See
  `diagrams.go.md`. A separate opt-in helper, `SpecValues`, lets an app assert a
  ```` ```spec ```` row's literal (a port, a path, a default) against the real constant its
  own source uses.

## Notes

- `internalLink`/`headingAnchor` are the two regexes driving `Links`/`Anchors`; both
  operate on raw markdown, not rendered HTML, matching what the frontend's own renderer
  (`frontend/shared/src/Manual.js`) parses.
- `translatedSlugs(lib, lang)` deliberately excludes English-fallback entries (`Language !=
  lang`) — the whole point when checking for a *missing* translation.
- First consumer: `apps/mymatasan/manual/manual_test.go`
  (`apps/mymatasan/manual/manual_test.go.md`).
- `uirefs.go` (`uirefs.go.md`), same package, is the complementary check that runs in the
  other direction: instead of the manual checking itself, it checks that the app's frontend
  contextual-help wiring still points at real articles/anchors in this manual. It reuses
  `articleAnchors` defined here.
- `diagrams.go` (`diagrams.go.md`), same package, is the Diagrams subtest wired in above,
  plus the opt-in `SpecValues` helper. It reuses `anchorsIn`/`headingAnchor` from this file
  to resolve a figure's `=> slug#anchor` links, so a figure link and a markdown link can
  never disagree about what counts as a valid target.
