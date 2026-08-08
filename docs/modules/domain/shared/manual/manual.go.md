# Module: domain/shared/manual/manual.go

## Purpose

Shared, app-agnostic library for the suite's built-in user manual — markdown articles
compiled into an app's binary so an appliance can show its own documentation with no
network access, the same air-gapped posture the rest of the product keeps. Generalizes what
`apps/myiotsan/kb` (`apps/myiotsan/kb/kb.go.md`) proved for a single app, single language,
no search, no print, no deep links: this loader is reused by every app and supports all
four suite languages plus figures.

An app supplies an `fs.FS` (normally a `//go:embed`) laid out as:

```
<root>/en/*.md   <root>/ms/*.md   <root>/zh/*.md   <root>/ar/*.md   <root>/assets/*
```

## Key Type: Article

```go
type Article struct {
    Slug          string
    Title         string
    Category      string
    CategoryLabel string
    Summary       string
    Order         int
    Language      string
    Body          string `json:"body,omitempty"`
}
```

`Category` is a stable, never-translated grouping id; `CategoryLabel` is the translated
heading a reader sees — keeping them apart is what lets all four language folders group
into the same sections. `Language` reports the folder the body actually came from, which
differs from the requested language exactly when this article has not been translated yet.
`Body` is omitted (`json:",omitempty"`) from index payloads.

## Slugs are the filename, not the title

The **filename** is the article's stable identity and is identical in every language
folder — that is what keeps a cross-link (`[see this](adding-cameras)`) and a contextual
deep link from the UI working when the reader switches language, where a title-derived slug
would not. `Slug(fileName)` strips the `.md` extension and a numeric on-disk ordering prefix
(`10-welcome.md` → `welcome`), so renumbering a folder never breaks a link.

## Key Type: Library / New

```go
func New(fsys fs.FS, root string) *Library
```

`root` is the directory holding the language folders (`"."` when the app embeds `en ms zh
ar assets` from its own package directory). Nothing is read until the first query
(`sync.Once`-backed `load()`), so constructing a `Library` at package scope costs nothing.

## Key Functions

```go
func (l *Library) Languages() []string
func (l *Library) Language(lang string) string
func (l *Library) Articles(lang string) []Article
func (l *Library) Bundle(lang string) []Article
func (l *Library) Get(lang, slug string) (Article, bool)
func (l *Library) Asset(name string) ([]byte, bool)
```

- `Languages()` — the language folders this library actually ships, `DefaultLanguage`
  (`"en"`) first.
- `Language(lang)` — resolves a requested locale onto a shipped one (`normalize`); callers
  echo this back rather than the raw request, since the reader's UI compares it against each
  article's own `Language` to decide whether to say "not translated yet" — echoing an
  unshipped locale would make every article on the page claim to be untranslated.
- `Articles(lang)` — the index for a language: metadata only (`Body` blanked), sorted by
  `(Order, Title)`.
- `Bundle(lang)` — every article for a language, bodies included. The client fetches this
  once to power client-side full-text search and whole-manual printing without a
  round trip per article.
- `Get(lang, slug)` — one article, body included.
- `Asset(name)` — reads a figure from `<root>/assets`. `name` must be a bare file name; any
  path separator or `..` is rejected outright (not cleaned), because a manual figure never
  legitimately needs one and a silent clean is how a traversal bug gets written.

## resolve / fallback behavior

`resolve(lang)` (used by both `Articles` and `Bundle`) walks `DefaultLanguage`'s article list
as the ordering/completeness source of truth and overlays the requested language's articles
by slug — an article missing from a language falls back to English rather than being omitted,
and its `Language` field reports the real source so a partial translation degrades to English
prose instead of a 404 or an empty page. `resolve` returns a freshly copied slice each call,
so callers that blank out `Body` (`Articles`) cannot mutate the cached data other callers see.

## load / Parse

`load()` reads every language subdirectory once (`sync.Once`), skipping `assets/`; a missing
or unreadable `root` or language directory degrades to an empty result rather than panicking.
`Parse(content string) Article` splits an optional `--- key: value ... ---` frontmatter block
off the top of a markdown file (`title`/`category`/`categoryLabel`/`summary`/`order`) and
returns the remaining markdown as `Body`; content with no frontmatter fence, or a missing
closing delimiter, degrades to "the whole file is the body" rather than erroring.

## Notes

- Covered by `manual_test.go` (`Slug` prefix-stripping, language fallback/resolution,
  asset traversal rejection, cache-mutation safety, frontmatter parsing).
- `domain/shared/manual/manualcheck` (`manualcheck/check.go.md`) is the conformance suite
  each app runs against its own shipped `Library` — frontmatter completeness, language
  parity, cross-link resolution, and heading-anchor consistency are enforced there, not
  here, so this package stays free of `testing` as a shipped dependency.
- `domain/shared/apis/manual.go` (`apis/manual.go.md`) is the HTTP handler layer built on
  top of this package; `apps/mymatasan/manual/manual.go` (`apps/mymatasan/manual/manual.go.md`)
  is the first app to embed content and wire it up.
