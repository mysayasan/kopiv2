# Module: domain/shared/manual/diagram/diagram.go

## Purpose

The grammar and strict parser for the manual's drawn figures. A manual article draws a
picture by writing a fenced block — ```` ```flow ````, ```` ```arch ````, ```` ```seq ```` or
```` ```spec ```` — whose lines declare nodes and edges as text. This package is the single
authority for that grammar: `domain/shared/manual/manualcheck/diagrams.go`
(`manualcheck/diagrams.go.md`) uses it as the build guard, and
`frontend/shared/src/manual/diagram.js` renders the same shapes to inline SVG. A figure is
written as text, not shipped as an image, because only text can be translated by the same
person translating the prose around it, follow the app's `--ui-*` theme, mirror for Arabic,
and be diffed in a pull request.

## Key Rule

> Everything before the label is STRUCTURE and must be byte-identical in every language.
> Everything after it is PROSE and must be translated.

Structure is node ids, shapes, edges, link targets and spec values — none of which is
language. `Diagram.Fingerprint()` hashes exactly that half, which is what lets
`manualcheck` assert a translated copy of a figure drew the same picture as English.

## Key Types

```go
type Kind string // KindFlow | KindArch | KindSeq | KindSpec
type Node struct { ID, Shape, Link, Value, Label string }
type Edge struct { From, To string; Dashed bool; Label string }
type Diagram struct { Kind Kind; Title string; Nodes []Node; Edges []Edge }
type Raw struct { Kind Kind; Line int; Body string } // a located, unparsed fence
```

- Each `Kind` accepts its own set of node shapes (`shapes` map): `flow` → `step`/`ask`/
  `ok`/`end`/`alert`; `arch` → `box`/`store`/`ext`; `seq` → `actor`; `spec` → `row`. A shape
  is a role the renderer draws with meaning (a diamond for `ask`, the refusal palette for
  `end`), not a decoration the author picks for looks.
- `Node.Link` (`=> slug` or `=> slug#anchor`) is what turns a figure into navigation;
  resolved by `manualcheck` with the same rule a contextual `?` button uses.
- `Node.Value` is only legal on a `spec` row's literal (a port, a path, a default) and is
  asserted against real source constants by `manualcheck.SpecValues`.

## Key Functions

```go
func IsKind(s string) bool
func Fences(body string) []Raw
func Parse(kind Kind, body string) (Diagram, error)
func (d Diagram) Links() []string
func (d Diagram) Values() map[string]string
func (d Diagram) Fingerprint() string
```

- `Fences` walks an article body tracking fence open/close state (not a regexp), so a fence
  that legitimately contains a line looking like a fence opener — a manual about markdown
  eventually has one — is not mis-split.
- `Parse` is deliberately strict: it rejects anything it does not fully understand rather
  than skipping it, because the frontend renderer is deliberately the lenient half — by the
  time a fence reaches a reader, CI has already validated it, and the worst thing a
  renderer can do on an appliance is throw and take the article with it. Every returned
  error names the 1-based line inside the fence.
- Line grammar, read positionally: `title : <label>`, `<from> -> <to> [: label]` /
  `<from> --> <to> [: label]` (`-->` marks a reply/aside/optional path), and
  `<shape> <id> [=> link] [` `value` `] : <label>`. Parsing stops looking at syntax at the
  first standalone `:`, so a label may itself contain colons, arrows and backticks. `#` at
  the start of a trimmed line is a comment, not content.
- `checkID` restricts every id to `[A-Za-z0-9_-]` — characters a fingerprint, an SVG id and
  a URL fragment all handle unescaped, and that exist on every keyboard a translator might
  be using.
- `validate()` also catches an isolated node (declared but touched by no edge — almost
  always a typo'd id in an edge rather than a deliberately stranded box, for any kind but
  `spec`) and a `spec` block containing an edge (it is a table, not a drawing).
- `Fingerprint()` sorts nodes and edges before hashing so declaration order — the author's
  layout control — never affects equality. It cannot see which prose sits on which edge
  (only whether the edge is labelled), the same limit `manualcheck`'s Anchors check has
  always had for prose: structure is machine-checkable, meaning is what review is for.

## Notes

- Author-facing guide to this grammar: `docs/MANUAL_DIAGRAMS.md`.
- Consumers: `domain/shared/manual/manualcheck/diagrams.go` (`manualcheck/diagrams.go.md`,
  the build guard) and `frontend/shared/src/manual/diagram.js` (the renderer — deliberately
  the lenient, not the authoritative, half of this grammar).
- If the grammar changes, it changes here first; the renderer and the guard both follow.
