// Package retrieval makes the suite's built-in manuals searchable by a program rather than by a
// reader — specifically, so the myseliasan fleet agent can ground an answer in the documentation
// instead of only in the control plane's own tables.
//
// # Why lexical retrieval and not embeddings
//
// The obvious build for this is a vector store. It is the wrong one here, for reasons that are
// all consequences of where this runs:
//
//   - The appliance is air-gapped. Embeddings mean shipping a second model (100–400 MB), plus the
//     download / import / checksum / factory-reset paths for it, plus a store, plus a re-index on
//     every upgrade. The manual is ~800 KB of text.
//   - The corpus is hand-written and already structured. Chunk boundaries and citation targets
//     exist as `## Heading {#anchor}`; there is nothing for a model to discover.
//   - Determinism is worth more than recall here. A golden test can assert "this question finds
//     this section" and keep asserting it after the manual is edited. Nothing equivalent is
//     available for a nearest-neighbour search whose model may be swapped.
//   - The language model is OPTIONAL and off by default. Retrieval that needs no model still
//     answers "which page covers this" when the agent cannot compose prose.
//
// If that trade ever stops holding, Corpus is the seam: it exposes one Search method, and a
// hybrid retriever can be dropped behind it without the chat layer noticing.
package retrieval

import (
	"sort"
	"sync"

	"github.com/mysayasan/kopiv2/domain/shared/manual"
)

// Source is one app's manual joined to the corpus.
//
// App is the stable id used in chunk identity and in citations; Title is the product name a
// reader sees. Both are needed because a fleet control plane answers questions about software
// running on OTHER machines — "add a camera" is a MyMataSan answer even when it is asked here,
// and an answer that does not say so sends the operator to the wrong screen.
type Source struct {
	App     string
	Title   string
	Library *manual.Library
}

// Corpus is the searchable union of several apps' manuals, indexed per language on first use.
type Corpus struct {
	sources []Source

	mu     sync.Mutex
	byLang map[string]*index
}

// New builds a corpus. Nothing is read or indexed until the first search, so constructing one
// during wiring costs nothing on a control plane whose operator never opens the chat.
func New(sources ...Source) *Corpus {
	return &Corpus{sources: sources, byLang: map[string]*index{}}
}

// Apps lists the app ids in the corpus, in registration order.
func (c *Corpus) Apps() []string {
	out := make([]string, 0, len(c.sources))
	for _, s := range c.sources {
		out = append(out, s.App)
	}
	return out
}

// minCoverage drops a chunk that only matched a sliver of the question.
//
// It is a floor against noise, NOT an intent detector, and the distinction was measured rather
// than assumed. Calibrating against the real 328-chunk manual (see the calibration test in
// apps/myseliasan/services) showed documentation questions and fleet-status questions landing in
// the same 0.34–1.00 band, because they are asked with the same nouns: "how do I add a node" and
// "how many nodes are offline" differ in intent, not vocabulary. A second lexical gate was tried
// and removed — it saturated at 1.0 for every question on a corpus this size, which is the shape
// of a knob that looks principled and decides nothing.
//
// So the caller is built to absorb an unhelpful retrieval cheaply — few excerpts, small budget,
// and citations that reflect what the model actually used — instead of trusting a threshold to
// prevent one.
const minCoverage = 0.34

// A chunk is relevant if it matched enough of the question (minCoverage) OR the question named
// what the section is ABOUT (Result.TopicHit).
//
// The disjunction is load-bearing, and both halves were forced by measurement. Coverage alone
// rejected a valid Arabic question whose one matched word was the article's own title, because
// only English is folded morphologically and the other word was therefore unknown. Lowering the
// floor to admit it also admitted "what is the exchange rate for the yen", which matched "rate"
// in a paragraph about frame rates. Neither case is about *how much* was matched; both are about
// *where*.
func relevant(r Result) bool { return r.Coverage >= minCoverage || r.TopicHit }

// Search returns the best passages for a question, best first.
//
// A question asked in ms/zh/ar is searched in that language first and then, if the corpus has
// little to say, in English as well. That fallback is not a nicety: operators type English
// product nouns ("ONVIF", "RTSP", "PMTiles") inside a Malay sentence all the time, and the
// English article is a better answer than nothing. Each result carries the language it came
// from, so a caller can tell the reader when it had to fall back.
func (c *Corpus) Search(lang, query string, limit int) []Result {
	if limit <= 0 {
		limit = 5
	}
	results := filterRelevant(c.index(lang).search(query, limit*2))

	if lang != manual.DefaultLanguage && len(results) < limit {
		fallback := filterRelevant(c.index(manual.DefaultLanguage).search(query, limit*2))
		results = mergeByID(results, fallback)
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func filterRelevant(in []Result) []Result {
	out := in[:0]
	for _, r := range in {
		if relevant(r) {
			out = append(out, r)
		}
	}
	return out
}

// mergeByID appends fallback results the primary language did not already cover. Identity is the
// app+slug+anchor, NOT the language, so an English fallback never duplicates a section the
// reader's own language already answered.
func mergeByID(primary, fallback []Result) []Result {
	seen := make(map[string]bool, len(primary))
	for _, r := range primary {
		seen[r.Chunk.App+":"+r.Chunk.Slug+"#"+r.Chunk.Anchor] = true
	}
	for _, r := range fallback {
		key := r.Chunk.App + ":" + r.Chunk.Slug + "#" + r.Chunk.Anchor
		if seen[key] {
			continue
		}
		seen[key] = true
		primary = append(primary, r)
	}
	return primary
}

// index returns the language's index, building it on first use.
func (c *Corpus) index(lang string) *index {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ix, ok := c.byLang[lang]; ok {
		return ix
	}
	var chunks []Chunk
	for _, s := range c.sources {
		if s.Library == nil {
			continue
		}
		// Bundle already fills an untranslated article from English and reports where the text
		// really came from, so the index inherits that behaviour for free.
		for _, a := range s.Library.Bundle(lang) {
			chunks = append(chunks, ChunkArticle(s.App, s.Title, a)...)
		}
	}
	ix := buildIndex(chunks)
	c.byLang[lang] = ix
	return ix
}

// Size reports how many chunks a language's index holds, building it if needed. Diagnostics and
// tests use it; nothing in the answer path does.
func (c *Corpus) Size(lang string) int { return len(c.index(lang).chunks) }
