# Module: domain/shared/manual/retrieval/chunk.go

## Purpose

Splits one manual article into retrievable passages. `ChunkArticle(app, appTitle string, a
manual.Article) []Chunk`.

## A chunk is a SECTION, not a window of characters

Retrieval systems usually have to invent chunk boundaries, and inventing them badly is how an
answer ends up quoting half a sentence. This manual does not need them invented: it is already
written as `## Heading {#anchor}` sections, each a self-contained answer to one question, and those
anchors are **already** the deep-link targets the contextual `?` buttons use.

Chunking on them means a retrieved passage is exactly the passage a reader can be sent to — the
citation and the evidence are the same object. It also means the manual's authors control chunking
by writing headings, with no tuning parameter in between.

## Key Type: Chunk

```go
type Chunk struct {
    App, AppTitle string   // stable owning-app id + display name
    Lang, Slug    string
    Anchor        string   // the section's {#id}; empty for an article's opening passage
    ArticleTitle  string
    Heading       string
    Category      string
    Part          int      // >0 when an oversized section had to be split
    Text          string
}
```

`App` is **not decoration**: both shipped manuals contain articles called `welcome`,
`first-sign-in`, `setup-wizard`, `workspace-tour` and `using-this-manual`, so a slug alone
identifies the wrong page half the time. `ID()` is `app:slug#anchor` (plus `~part`), and `Path()`
is the reader-facing trail `"MyMataSan › Adding cameras › Discovery"`.

## Splitting rules

1. The **H1 is skipped** — it repeats `Article.Title`, which is already an indexed weighted field.
2. Everything before the first H2 becomes the anchorless **opening chunk**. It is often the best
   answer to a broad question ("what is this screen for?").
3. Each `## ` starts a new section; `splitAnchor` peels `{#id}` off the heading text.
4. H3 and deeper stay *inside* their parent section — unless the section exceeds
   `maxChunkRunes`=1200, in which case they become the split points (`subHeadingBlocks`), falling
   back to packing whole paragraphs (`paragraphs`).
5. Fenced code blocks are tracked throughout, so a `##` inside a fence never starts a section and a
   split never lands inside one. A single block over the cap is emitted whole rather than hacked
   apart — half a table is worse than no table; `Snippet` trims it at read time instead.

**Runes, not bytes**, for the size cap: a byte budget would silently make Chinese chunks a third
the size of English ones, since zh is three bytes per character in UTF-8.

## Snippet

`Snippet(maxRunes)` trims to a paragraph break in the final quarter, else a sentence end past the
halfway point, else hard — and always marks the cut with `…`. A model told the text was truncated
knows it is looking at part of a section; silently truncated prose reads as a complete thought that
happens to end mid-argument.
