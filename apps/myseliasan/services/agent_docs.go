package services

import (
	"regexp"
	"strconv"
	"strings"

	mymatasanmanual "github.com/mysayasan/kopiv2/apps/mymatasan/manual"
	myseliasanmanual "github.com/mysayasan/kopiv2/apps/myseliasan/manual"
	"github.com/mysayasan/kopiv2/domain/shared/manual/retrieval"
)

// The documentation half of the agent's grounding.
//
// The chat could already answer "what is my fleet doing" and could not answer "how does this
// product work", because everything it was allowed to read came from the control plane's own
// tables. This file adds the second source: the built-in manuals, retrieved per question and
// handed to the model as a few named excerpts.
//
// TWO MANUALS, ON PURPOSE. A fleet control plane is where an operator asks questions about
// software running on other machines — "how do I add a camera" is a MyMataSan answer even though
// it is asked here. So myseliasan's binary carries mymatasan's manual as well as its own, and
// every citation names which product it came from; an answer that silently mixed the two would
// send the reader to a screen their control plane does not have.
//
// This is the suite's ONLY cross-app import, and it is deliberately confined to this file. The
// imported package is content plus an embed directive — no services, no config, no init work — so
// the coupling is a body of text, not behaviour. If it ever needs to become a shared `manuals/`
// namespace, this file is the whole blast radius.

// docCorpusSources binds each app id to the manual it owns and the name a reader knows it by.
var docCorpusSources = []retrieval.Source{
	{App: "myseliasan", Title: "MySeliaSan", Library: myseliasanmanual.Library},
	{App: "mymatasan", Title: "MyMataSan", Library: mymatasanmanual.Library},
}

// localApp is the manual this control plane serves itself, and therefore the only one the SPA can
// deep-link into with openHelp.
const localApp = "myseliasan"

// The budget is small ON PURPOSE, and that is a design conclusion rather than a default.
//
// The obvious plan was a relevance threshold that admits documentation questions and rejects
// fleet-status ones. Calibrated against the real manual, no such threshold exists: "how do I add
// a node" and "how many nodes are offline" are asked with identical vocabulary, so every lexical
// score puts them in the same band (the measurements are in agent_docs_test.go). Retrieval will
// therefore sometimes attach documentation to a question that did not want it.
//
// Given that, the useful lever is not precision but COST. Three short excerpts are enough to
// answer a how-to question and cheap enough that a wrong retrieval costs a state question a
// fraction of its context instead of a third of it — and the prompt, which can see intent in a
// way a term-frequency score cannot, decides what to actually use.
const (
	docsMaxExcerpts = 3
	// docsMaxBytes caps the rendered excerpt block. Bytes are the enforceable proxy for tokens.
	docsMaxBytes = 2500
	// docsPerExcerptRunes trims one section so a single long one cannot eat the whole budget.
	docsPerExcerptRunes = 700
)

// DocExcerpt is one cited manual section, in the shape the SSE `sources` event and the docs
// search endpoint both use.
type DocExcerpt struct {
	// Ref is the label the model cites — "M1". The server assigns it and keeps the mapping, so a
	// citation can be mis-chosen but never invented: the model is never asked to reproduce a slug.
	Ref      string `json:"ref"`
	App      string `json:"app"`
	AppTitle string `json:"appTitle,omitempty"`
	Slug     string `json:"slug"`
	Anchor   string `json:"anchor,omitempty"`
	Title    string `json:"title"`
	Heading  string `json:"heading,omitempty"`
	Path     string `json:"path"`
	// Lang is the language the text actually came from, which is not always the one asked for.
	Lang string `json:"lang"`
	// Local marks a section in THIS app's manual — the only ones the reader's UI can open.
	Local bool `json:"local"`
	// Cited reports whether the finished answer actually referenced this excerpt. The reader's
	// UI leads with the cited ones and offers the rest quietly as further reading, so a source
	// list never implies the answer came from a section the model ignored.
	Cited bool `json:"cited,omitempty"`
	// Snippet is populated for the docs search endpoint (the reader sees it); the chat path
	// leaves it empty because the same text is already in the prompt.
	Snippet string `json:"snippet,omitempty"`
}

// DocsService retrieves manual sections for a question.
type DocsService struct {
	corpus *retrieval.Corpus
}

// NewDocsService builds the retriever over every manual this binary ships. Indexing is lazy, so
// a control plane whose operator never asks a question never pays for it.
func NewDocsService() *DocsService {
	return &DocsService{corpus: retrieval.New(docCorpusSources...)}
}

// Search returns ranked manual sections with a display snippet. Used by the docs endpoint, which
// works whether or not a language model is configured.
func (d *DocsService) Search(lang, question string, limit int) []DocExcerpt {
	if d == nil || d.corpus == nil {
		return nil
	}
	results := d.corpus.Search(lang, question, limit)
	out := make([]DocExcerpt, 0, len(results))
	for i, r := range results {
		e := toExcerpt(i, r)
		e.Snippet = r.Chunk.Snippet(docsPerExcerptRunes)
		out = append(out, e)
	}
	return out
}

// Ground returns the excerpts for a question and the exact text block to append to the prompt.
// Both are empty when the manual has nothing relevant to say — which is the common case for a
// pure fleet-status question, and is why a documentation corpus does not cost every answer its
// context budget.
func (d *DocsService) Ground(lang, question string) ([]DocExcerpt, string) {
	if d == nil || d.corpus == nil {
		return nil, ""
	}
	results := d.corpus.Search(lang, question, docsMaxExcerpts)
	if len(results) == 0 {
		return nil, ""
	}

	var (
		excerpts []DocExcerpt
		b        strings.Builder
	)
	b.WriteString(docsHeader)
	for i, r := range results {
		snippet := r.Chunk.Snippet(docsPerExcerptRunes)
		e := toExcerpt(i, r)
		entry := "\n[" + e.Ref + "] " + e.Path + "\n" + snippet + "\n"
		// Stop at the budget rather than trimming the last entry to a stub: half a section is
		// the kind of evidence a model completes from memory.
		if b.Len()+len(entry) > docsMaxBytes && len(excerpts) > 0 {
			break
		}
		b.WriteString(entry)
		excerpts = append(excerpts, e)
	}
	return excerpts, b.String()
}

// docsHeader is the framing the excerpts arrive under. It says what the text IS, because the one
// failure mode that matters here is a model reading a manual sentence as an observation about
// this fleet — "cameras are added from the Cameras screen" becoming "your cameras were added".
const docsHeader = "MANUAL EXCERPTS — product documentation, not live fleet data. " +
	"Each excerpt is labelled with the product it documents.\n"

// citedRef matches the labels the model was asked to cite. Small models are inconsistent about
// the brackets — "[M2]", "(M2)", "as M2 says" all occur — so the label alone is the signal.
var citedRef = regexp.MustCompile(`(?i)\bM([0-9]{1,2})\b`)

// MarkCited flags the excerpts the finished answer referenced.
//
// It runs AFTER the completion rather than before, which is the whole point: what deserves to be
// shown as a source is what the answer used, not what a search ranked highly. A retriever that
// pulled the wrong section therefore costs a few hundred bytes of context and nothing else — the
// reader is never told the answer rests on a page it does not.
func MarkCited(excerpts []DocExcerpt, answer string) []DocExcerpt {
	if len(excerpts) == 0 || answer == "" {
		return excerpts
	}
	cited := map[string]bool{}
	for _, m := range citedRef.FindAllStringSubmatch(answer, -1) {
		cited["M"+strings.TrimLeft(m[1], "0")] = true
	}
	out := make([]DocExcerpt, len(excerpts))
	copy(out, excerpts)
	for i := range out {
		out[i].Cited = cited[out[i].Ref]
	}
	return out
}

func toExcerpt(i int, r retrieval.Result) DocExcerpt {
	c := r.Chunk
	return DocExcerpt{
		Ref:      "M" + strconv.Itoa(i+1),
		App:      c.App,
		AppTitle: c.AppTitle,
		Slug:     c.Slug,
		Anchor:   c.Anchor,
		Title:    c.ArticleTitle,
		Heading:  c.Heading,
		Path:     c.Path(),
		Lang:     c.Lang,
		Local:    c.App == localApp,
	}
}
