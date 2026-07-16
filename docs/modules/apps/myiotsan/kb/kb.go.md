# Module: apps/myiotsan/kb/kb.go

## Purpose

The shipped, in-app knowledge base: setup guides compiled into the binary so the appliance can
show them with no network access — the same air-gapped posture the rest of the product keeps
(myseliasan/myidsan intranet, mymatasan first-run wizard). Markdown is the source of truth (`kb/
solar/*.md`) and is served near-verbatim; the frontend (`components/kb.js`) renders it. Added
alongside the three new register-map Modbus profiles (`services/profile_catalog.go.md`) it
documents.

## Key Type: Article

```go
type Article struct {
    Slug     string
    Title    string
    Category string
    Order    int
    Body     string `json:"body,omitempty"`
}
```

`Slug` is the markdown filename without `.md` (the file IS the identity — no separate id to keep
in sync). `Title`/`Category`/`Order` come from a `--- key: value ... ---` YAML-ish frontmatter
block `parse` strips off the top of the file; a file with no frontmatter is accepted too (`Body`
becomes the whole trimmed file, everything else zero-value) rather than failing to load.

## Key Functions: Articles / Get

```go
func Articles() []Article // list, Body always ""
func Get(slug string) (Article, bool) // one article, Body included
```

`Articles()` is the list-view payload — metadata only, deliberately stripping `Body` even though
`load()`'s cache carries it, so a list request never ships every article's full markdown. `Get`
looks up one article by slug, body included, for the detail view. Both read through a single
process-lifetime `cached []Article` populated once by `load()` (embedded files are static content;
there is nothing to invalidate).

## Key Function: load / parse

```go
func load() []Article
func parse(content string) Article
```

`load()` reads every `*.md` under the `//go:embed solar/*.md` filesystem, `parse`s each, and sorts
the result by `(Order, Title)` — the order the list view and the index article's own "read this
first" narrative expect. A directory-read or per-file-read error is swallowed to an empty/skipped
result rather than panicking (embedded content read failing is not something the appliance should
crash over). `parse` splits the frontmatter delimiter (`---\n` ... `\n---`) off the top; a file
missing the closing delimiter, or with no frontmatter fence at all, degrades to "the whole file is
the body" rather than erroring, so a slightly malformed article still displays instead of 500ing.

## Notes

- Registered as `GET /api/kb` (list) and `GET /api/kb/{slug}` (one article) by
  `apps/myiotsan/apis/kb.go` (`apis/kb.go.md`), granted to viewer AND operator
  (`services/rbac.go.md`) — it is read-only reference content, so there is no reason to gate it any
  tighter than the device/profile catalog it documents.
- Ships eight articles under `kb/solar/`: `index` (the "start here" overview linking every other
  article and summarizing the five new solar flow templates), `sungrow-sh`, `deye-hybrid`,
  `eastron-sdm630` (one per new register-map profile in `services/profile_catalog.go.md`),
  `sunspec-inverters` (the SunSpec-native path — Fronius/SMA/SolarEdge), `modbus-tcp-vs-rtu` and
  `gateway-rs485-tcp` (the connection-reality guides most residential inverters need, since they
  expose Modbus over RS485 rather than native TCP), and `verify-control` (how to bench-verify a
  control register before enabling actuation on any profile that declares a command).
- Covered by `kb_test.go` (`kb_test.go.md`): the embed loads at least five articles, the list
  payload never carries a body, every article has a parsed title, the list is order-sorted, and
  `Get` both returns a frontmatter-free body for a real slug and `false` for a missing one.
