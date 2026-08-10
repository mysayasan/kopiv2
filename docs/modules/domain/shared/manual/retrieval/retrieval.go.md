# Module: domain/shared/manual/retrieval/retrieval.go

## Purpose

Makes the suite's built-in manuals (`domain/shared/manual/manual.go.md`) searchable by a
**program** rather than by a reader, so the myseliasan fleet agent can ground an answer in
documentation instead of only in the control plane's own tables.

`Corpus` is the union of several apps' manuals, indexed per language on first use.

## Why lexical retrieval and not embeddings

This is the load-bearing decision of the package, and every consequence below follows from it:

- **The appliance is air-gapped.** Embeddings mean shipping a second model (100–400 MB), plus its
  download / import / checksum / factory-reset paths, plus a vector store, plus a re-index on every
  upgrade. The whole corpus is ~800 KB of text.
- **The corpus is hand-written and already structured.** Chunk boundaries and citation targets
  exist as `## Heading {#anchor}` — the same anchors the contextual `?` buttons deep-link to. There
  is nothing for a model to discover.
- **Determinism is worth more than recall here.** A golden test can assert "this question finds
  this section" and keep asserting it after the manual is edited. Nothing equivalent is available
  for a nearest-neighbour search whose model may be swapped underneath it.
- **The language model is optional and off by default.** Retrieval that needs no model still
  answers "which page covers this" when the agent cannot compose prose — which is what
  `GET /api/agent/docs` serves.

`Corpus.Search` is the seam: a hybrid or vector retriever can be dropped behind it without the chat
layer noticing.

## Key Type: Source

```go
type Source struct {
    App     string          // stable id, e.g. "mymatasan" — used in chunk identity and citations
    Title   string          // display name, e.g. "MyMataSan"
    Library *manual.Library
}
```

Both names are needed because a fleet control plane answers questions about software running on
**other** machines: "how do I add a camera" is a MyMataSan answer even when it is asked in
myseliasan, and an answer that does not say so sends the operator to the wrong screen.

## API

| Func | Behaviour |
|---|---|
| `New(sources ...Source) *Corpus` | Builds the corpus. Nothing is read or indexed until the first search. |
| `(*Corpus) Search(lang, query string, limit int) []Result` | Ranked passages, best first. |
| `(*Corpus) Apps() []string` | App ids in registration order. |
| `(*Corpus) Size(lang string) int` | Chunk count for a language (diagnostics/tests only). |

## Lazy, per-language indexing

`index(lang)` builds under a mutex on first use and caches per language. Articles come from
`Library.Bundle(lang)`, which already fills an untranslated article from English and reports the
language the text really came from — so the index inherits that fallback for free, and
`Chunk.Lang` tells a caller when it had to fall back.

## English fallback

A question asked in ms/zh/ar is searched in that language and then, if the corpus had little to
say, in English as well. This is not a nicety: operators type English product nouns ("ONVIF",
"RTSP", "PMTiles") inside a Malay sentence constantly, and the English article beats nothing.
`mergeByID` de-duplicates on `app:slug#anchor` (**not** language), so an English fallback never
duplicates a section the reader's own language already answered.

## The relevance rule

A chunk is relevant if `Coverage >= minCoverage`(0.34) **OR** `TopicHit` — it matched enough of
the question, or the question named what the section is *about*.

The disjunction is load-bearing and both halves were forced by measurement. Coverage alone
rejected a valid Arabic question (`كيف أضيف كاميرا`, "how do I add a camera") whose one matched
word was the article's own title: only English is folded morphologically, so the other word was
unknown and the coverage denominator buried it at 0.31. Lowering the floor to admit it also
admitted "what is the exchange rate for the yen", which matched "rate" in a paragraph about frame
rates and scored 0.48. Neither case is about *how much* matched; both are about *where* — which is
what `TopicHit` reports and no threshold on `Coverage` can express.

## What the relevance rule cannot do

It is a floor against noise, **not an intent detector**, and that was measured rather than assumed.
Calibrated against the real 328-chunk corpus (see
`apps/myseliasan/services/agent_docs_test.go`), documentation questions and fleet-status questions
land in the same 0.34–1.00 coverage band, because they are asked with the same nouns — "how do I
add a node" and "how many nodes are offline" differ in intent, not vocabulary. A second lexical
gate (matched IDF against the corpus maximum) was implemented, measured, and **deleted**: it
saturated at 1.0 for every question on a corpus this size, which is the shape of a knob that looks
principled and decides nothing.

The consequence is deliberate and lives in the caller: myseliasan absorbs an unhelpful retrieval
cheaply — few excerpts, a small byte budget, and citations that reflect what the model actually
used — instead of trusting a threshold to prevent one. See
`apps/myseliasan/services/agent_docs.go.md`.

## Related

- `domain/shared/manual/retrieval/chunk.go.md` — section splitting
- `domain/shared/manual/retrieval/tokenize.go.md` — the four-language tokenizer
- `domain/shared/manual/retrieval/bm25.go.md` — ranking
- `apps/myseliasan/services/agent_docs.go.md` — the only consumer today
