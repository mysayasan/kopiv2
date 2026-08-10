# Module: apps/myseliasan/services/agent_docs.go

## Purpose

The **documentation half** of the fleet agent's grounding (`DocsService`). The chat could already
answer "what is my fleet doing" and could not answer "how does this product work", because
everything it was allowed to read came from the control plane's own tables. This adds the second
source: the built-in manuals, retrieved per question and handed to the model as a few named
excerpts.

Built on `domain/shared/manual/retrieval/retrieval.go.md`.

## Two manuals, on purpose — and the suite's only cross-app import

```go
var docCorpusSources = []retrieval.Source{
    {App: "myseliasan", Title: "MySeliaSan", Library: myseliasanmanual.Library},
    {App: "mymatasan",  Title: "MyMataSan",  Library: mymatasanmanual.Library},
}
```

A fleet control plane is where an operator asks questions about software running on **other**
machines — "how do I add a camera" is a MyMataSan answer even though it is asked here. So
myseliasan's binary carries mymatasan's manual as well as its own, and every citation names which
product it came from; an answer that silently mixed the two would send the reader to a screen their
control plane does not have.

This is the **only** place in the repo where one app imports another, and it is deliberately
confined to this file. The imported package is content plus an `//go:embed` — no services, no
config, no init work — so the coupling is a body of text, not behaviour. If it ever needs to become
a shared `manuals/` namespace, this file is the whole blast radius. (Cost: ~700 KB of markdown in
the binary.)

`localApp = "myseliasan"` marks the manual this control plane serves itself, and therefore the only
one the SPA can deep-link into with `openHelp`.

## The budget is small because precision is impossible

The original design had a relevance threshold admit documentation questions and reject
fleet-status ones. **Calibrated against the real manual, no such threshold exists**: "how do I add
a node" and "how many nodes are offline" are asked with identical vocabulary, so every lexical
score puts them in the same band (measurements in `agent_docs_test.go`; a second gate was tried
and deleted for saturating at 1.0). Retrieval will therefore sometimes attach documentation to a
question that did not want it.

Given that, the useful lever is not precision but **cost**:

| Constant | Value | Why |
|---|---|---|
| `docsMaxExcerpts` | 3 | Enough to answer a how-to question; past a handful a small model averages documents together instead of answering from the best one. |
| `docsMaxBytes` | 2500 | Bytes are the enforceable proxy for tokens. |
| `docsPerExcerptRunes` | 700 | Stops one long section eating the whole budget. |

A wrong retrieval costs a state question a fraction of its context instead of a third of it, and
the prompt — which can see intent in a way a term-frequency score cannot — decides what to use.

## Key Type: DocExcerpt

```go
type DocExcerpt struct {
    Ref, App, AppTitle, Slug, Anchor string
    Title, Heading, Path, Lang       string
    Local, Cited                     bool
    Snippet                          string
}
```

`Ref` ("M1") is the label the model cites. **The server assigns it and keeps the mapping**, so a
citation can be mis-chosen but never invented — the model is never asked to reproduce a slug.

## API

| Func | Behaviour |
|---|---|
| `NewDocsService()` | Builds the retriever over every manual this binary ships. Indexing is lazy. |
| `Search(lang, question, limit) []DocExcerpt` | Ranked sections **with** display snippets — serves `GET /api/agent/docs`, works with no LLM. |
| `Ground(lang, question) ([]DocExcerpt, string)` | The excerpts plus the exact text block to append to the prompt. Both empty when the manual has nothing to say. |
| `MarkCited(excerpts, answer) []DocExcerpt` | Flags which excerpts the finished answer referenced. Copies rather than mutating its input. |

## Excerpts reach the model as pre-rendered text

```
MANUAL EXCERPTS — product documentation, not live fleet data. Each excerpt is labelled with the
product it documents.

[M1] MyMataSan › Adding cameras › Discovery
<section text>
```

Not JSON. Same law the timestamp-hallucination bench produced: anything a small model must
interpret, it will eventually interpret wrong. The header says what the text **is**, because the
one failure mode that matters is a model reading a manual sentence as an observation about this
fleet — "recordings are kept for 14 days" becoming "your recordings are kept for 14 days".

`Ground` stops at the byte budget rather than trimming the last entry to a stub: half a section is
the kind of evidence a model completes from memory.

## MarkCited runs after the completion

That is the whole point. What deserves to be shown as a source is what the answer **used**, not
what a search ranked highly, so a retriever that pulled the wrong section costs a few hundred bytes
of context and nothing else — the reader is never told the answer rests on a page it does not.
`citedRef` matches `M<n>` loosely (`[M2]`, `(M2)`, "as M2 says"), because small models are
inconsistent about brackets.

## Related

- `apps/myseliasan/services/agent_chat.go.md` — the consumer
- `apps/myseliasan/apis/agent.go.md` — `sources` SSE event and `GET /api/agent/docs`
