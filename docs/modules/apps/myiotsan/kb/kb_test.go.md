# Module: apps/myiotsan/kb/kb_test.go

## Purpose

Proves the embedded knowledge base actually loads and parses with no filesystem or network
involved — the property the whole in-app KB depends on, since `kb.go`'s `//go:embed` content is
compiled into the binary and has no runtime fallback if it silently failed to parse.

## Responsibilities

- `TestArticlesLoadAndParse` — `Articles()` returns at least five entries (the shipped count is
  eight; five is a loose floor so a future article count doesn't need updating), no entry's `Body`
  is populated in the list payload, every entry has a non-empty `Title` (proving the frontmatter
  parsed rather than the whole file becoming an untitled body), and the list is sorted by `Order`
  ascending.
- `TestGetArticleReturnsBodyWithoutFrontmatter` — `Get("index")` succeeds, its `Title` is set, its
  `Body` is non-empty and does not start with the `---` frontmatter delimiter (proving `parse`
  actually stripped it rather than leaking it into the rendered markdown), and `Get` on an unknown
  slug returns `ok == false`.

## Notes

- Pure unit tests against the real embedded `solar/*.md` files — no mocking of `embed.FS`, so a
  genuinely malformed shipped article (bad frontmatter, missing file) would fail these tests at
  build/test time rather than only being discovered when the Help page renders it wrong.
