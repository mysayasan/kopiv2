# Module: domain/report/doc.go

## Purpose

Shared, dependency-light PDF builder for the printable reports the fleet apps generate on
demand (myseliasan first). Wraps `github.com/go-pdf/fpdf` so report code reads as data
(headings, key/value blocks, stat tiles, tables, embedded images) instead of a stream of
low-level cell calls, and centralises the shared look — the branded header band, zebra
tables, "page X of Y" footer — so every report across the suite comes out consistent.

Pure Go, no external binary or headless browser: the apps that use it run air-gapped, so
report generation can never reach for anything outside the process. Text renders through
fpdf's built-in Helvetica core font via the Windows-1252 translator, which covers Latin
scripts (English, Malay, accented European). CJK and Arabic in user-entered names render
blank until a Unicode TTF is bundled — a deliberate v1 limitation, not a silent failure.

## Key Types

- `Palette` — the small colour set (`Primary`/`Ink`/`Muted`/`Zebra`/`Border`/`TileBg`) the
  shared look is drawn from. `DefaultPalette()` is myseliasan's steel-blue scheme
  (`#2B6CB0` primary); a caller may set `Options.Palette` to tint a report for a different
  app.
- `Options` — cover/header metadata: `Title`, `Subtitle`, `Period` (blank to omit),
  `GeneratedAt` (passed in by the caller, never read from the clock, so output is
  deterministic and unit-testable), `AppName` (footer attribution, defaults `"myseliasan"`),
  `Palette` (zero value → `DefaultPalette()`).
- `Document` — a report under construction over an `*fpdf.Fpdf`. `New(opt)` starts it (A4
  portrait, mm units, auto page-break, `AliasNbPages` for a `{nb}` total-page footer token)
  and adds the first page ready for content; `drawHeader`/`drawFooter` are wired as fpdf's
  per-page callbacks so every page — including ones added mid-report by a table/image
  overflowing — gets the same branded band and footer with no per-call bookkeeping by
  the caller.
- `Tile` — one stat-tile value (`Label`, `Value`, `Accent` tints the value in the primary
  colour, `Danger` tints it red).
- `Column` — one table column (`Header`, `Width` mm — `0` shares the remaining width evenly
  among all auto columns, `Align` `"L"`/`"C"`/`"R"`).

## Content helpers (on `*Document`)

| Method | Renders |
|---|---|
| `H1(text)` | Primary section heading with an accent rule underneath. |
| `H2(text)` | Secondary heading. |
| `Para(text)` | Wrapped body paragraph. |
| `Note(text)` | Muted, smaller italic caption (footnotes, caveats). |
| `Empty(text)` | Muted italic "nothing to report" line — used so an empty section shows an explicit empty state rather than vanishing. |
| `Spacer(mm)` | Vertical whitespace. |
| `AddPage()` | Starts a fresh page (e.g. one page per floor/site in an inventory report). |
| `KeyValues(rows [][2]string)` | Two-column label:value definition list, values wrap. |
| `StatTiles(tiles []Tile)` | A row of up to 4 rounded summary tiles per row (wraps to further rows beyond 4). |
| `Table(cols []Column, rows [][]string)` | Bordered, zebra-striped table with a coloured header that repeats after a page break; each row's height grows to its tallest wrapped cell. |
| `Image(name string, img image.Image, maxWidthMM float64) error` | Embeds an image (PNG-encoded on the fly) scaled to `maxWidthMM`, capping height to the remaining page if it would otherwise overflow; page-breaks first if the image wouldn't fit at all. Returns an error (never swallowed) on a zero-size image or an fpdf registration failure, so a caller can surface *why* a plan couldn't be shown. |
| `Output() ([]byte, error)` | Finalises and returns the PDF bytes. |

`ensureSpace(mm)` (internal) forces a page break before a heading/tile row that would
otherwise be orphaned at the very bottom of a page — called at the top of `H1`/`H2`/`Table`.

## Notes

- `t(s)` sanitises every string written through the content helpers into cp1252 via the
  translator captured at `New`, dropping glyphs the core font cannot represent rather than
  panicking on them.
- New dependency: `github.com/go-pdf/fpdf` (`go.mod`/`go.sum`), a pure-Go PDF library with
  no cgo/system-font requirement, chosen specifically because myseliasan (and the suite in
  general) must keep working on an air-gapped install.
- First consumer: `apps/myseliasan/services/reports.go` (`services/reports.go.md`), which
  builds the Fleet Health / Inventory / Security / Incident reports on top of this package.
