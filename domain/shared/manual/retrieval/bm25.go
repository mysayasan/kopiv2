package retrieval

import (
	"math"
	"sort"
)

// BM25 over the manual, with the article's own structure as field weights.
//
// The corpus is a few hundred hand-written sections, so the classic ranking function is not a
// compromise here — it is the right tool. It needs no model, no download and no store, it answers
// in microseconds, and above all it is DETERMINISTIC: the golden tests below can assert that a
// given question retrieves a given section, which is not something an embedding model lets you
// promise across a version bump.
//
// The one departure from textbook BM25 is field weighting. A section titled "Discovery" is about
// discovery in a way that a paragraph mentioning the word once is not, so heading, article title
// and slug terms are repeated into the term stream. Repetition rather than a separate per-field
// score keeps the length normalisation honest — the weighted count is also the document length.

const (
	bm25K1 = 1.2
	bm25B  = 0.75

	// topicBoost rewards a chunk whose HEADING, ARTICLE TITLE, SLUG or CATEGORY contains a query
	// term — "this section is about what you asked", as opposed to "this section mentions it".
	//
	// It replaced an earlier scheme that repeated title terms 2–3× into the term stream, which a
	// live bench showed does not work: BM25's term-frequency saturation (k1=1.2) turns a 3×
	// repeat into roughly a 1.7× score, so the title signal drowned in body text, and
	// `how do I add a camera` ranked a disk-sizing FAQ answer above the article actually titled
	// "Adding cameras". Scoring the topic fields as a separate additive term keeps them out of
	// the saturation curve and out of the length normalisation.
	topicBoost = 1.5
)

type posting struct {
	doc int
	tf  float64
}

// index is one language's searchable corpus. Built once, then read-only.
type index struct {
	chunks   []Chunk
	postings map[string][]posting
	idf      map[string]float64
	docLen   []float64
	avgLen   float64
	// topics[d] is the set of terms in chunk d's heading, article title, slug and category —
	// what the chunk is ABOUT, as opposed to what it happens to mention.
	topics []map[string]bool
	// maxIDF is what a term found in exactly one chunk would score, and therefore the price this
	// index puts on a query term it has NEVER seen. Charging unknown words the maximum is what
	// stops "what is the exchange rate for the yen" from looking well covered because the manual
	// happens to know the word "rate".
	maxIDF float64
}

func buildIndex(chunks []Chunk) *index {
	ix := &index{
		chunks:   chunks,
		postings: make(map[string][]posting, len(chunks)*32),
		idf:      make(map[string]float64, len(chunks)*32),
		docLen:   make([]float64, len(chunks)),
		topics:   make([]map[string]bool, len(chunks)),
	}
	var total float64
	for d, c := range chunks {
		terms := map[string]float64{}
		add := func(text string) {
			for _, tok := range Tokenize(text) {
				terms[tok]++
			}
		}
		add(c.Text)

		// The topic fields are indexed too — otherwise a term appearing ONLY in a heading would
		// never make its chunk a search candidate — but at plain weight; their extra pull comes
		// from topicBoost at query time instead.
		topic := map[string]bool{}
		for _, field := range []string{c.Heading, c.ArticleTitle, c.Slug, c.Category} {
			for _, tok := range Tokenize(field) {
				if !topic[tok] {
					topic[tok] = true
					terms[tok]++
				}
			}
		}
		ix.topics[d] = topic

		var length float64
		for tok, tf := range terms {
			ix.postings[tok] = append(ix.postings[tok], posting{doc: d, tf: tf})
			length += tf
		}
		ix.docLen[d] = length
		total += length
	}
	if len(chunks) > 0 {
		ix.avgLen = total / float64(len(chunks))
	}
	n := float64(len(chunks))
	for tok, ps := range ix.postings {
		df := float64(len(ps))
		// Probabilistic IDF, floored at zero so a term in nearly every document cannot subtract
		// score from a document that legitimately contains it.
		ix.idf[tok] = math.Max(0, math.Log(1+(n-df+0.5)/(df+0.5)))
	}
	ix.maxIDF = math.Log(1 + (n-0.5)/1.5)
	return ix
}

// Result is one ranked chunk.
type Result struct {
	Chunk Chunk   `json:"chunk"`
	Score float64 `json:"score"`
	// Coverage is the share of the question's total IDF this chunk actually matched, 0..1.
	//
	// It exists because a raw BM25 score is meaningless on its own — its scale moves with corpus
	// size and query length, so a threshold tuned on today's corpus silently mis-fires when
	// twenty articles are added. Coverage is scale-free, so it is a usable floor.
	//
	// What it CANNOT do — measured, not assumed — is tell a how-to question from a
	// what-is-happening-now question. "How do I add a node" and "how many nodes are offline"
	// share their whole vocabulary, so no lexical score separates them; the caller absorbs a
	// wrong retrieval cheaply instead of pretending to prevent it. See the calibration test in
	// apps/myseliasan/services/agent_docs_test.go.
	Coverage float64 `json:"coverage"`
	// TopicHit reports that a query term appears in this chunk's HEADING, article title, slug or
	// category — the section is *about* one of the asked-about things, not merely mentioning it.
	//
	// This is the evidence Coverage cannot supply. Coverage measures how much of the question was
	// matched, which collapses on a short question containing one word the index only knows in
	// another form — "كيف أضيف كاميرا" matched كامير in the article's own title and still scored
	// 0.31, while "what is the exchange rate for the yen" scored 0.48 off the word "rate" buried
	// in a paragraph about frame rates. Being in the title is the difference between those two,
	// and no threshold on Coverage expresses it.
	TopicHit bool `json:"topicHit,omitempty"`
}

func (ix *index) search(query string, limit int) []Result {
	if ix == nil || len(ix.chunks) == 0 || limit <= 0 {
		return nil
	}
	qterms := map[string]float64{}
	for _, tok := range Tokenize(query) {
		qterms[tok]++
	}
	if len(qterms) == 0 {
		return nil
	}

	// The denominator for Coverage. A term the corpus has never seen counts at maxIDF rather than
	// at zero: it is the most informative word in the question precisely BECAUSE nothing here
	// discusses it, and scoring it free is how a question about foreign exchange ends up "fully
	// covered" by an article that shares the word "rate".
	//
	// Charging full price is only affordable because Result.TopicHit exists. A question in a
	// language this tokenizer does not fold well (Arabic, Malay) legitimately carries unknown
	// variants and scores poorly here; it is rescued by having matched the section's TITLE, not
	// by discounting the denominator. Discounting was tried and it let the foreign-exchange
	// question back in — the two cases differ in where the match landed, not in how much matched.
	var totalIDF float64
	for tok := range qterms {
		if idf, ok := ix.idf[tok]; ok {
			totalIDF += idf
			continue
		}
		totalIDF += ix.maxIDF
	}

	scores := map[int]float64{}
	matched := map[int]float64{}
	topicHit := map[int]bool{}
	for tok := range qterms {
		idf := ix.idf[tok]
		if idf <= 0 {
			continue
		}
		for _, p := range ix.postings[tok] {
			norm := 1 - bm25B + bm25B*ix.docLen[p.doc]/ix.avgLen
			score := idf * (p.tf * (bm25K1 + 1)) / (p.tf + bm25K1*norm)
			if ix.topics[p.doc][tok] {
				score += topicBoost * idf
				topicHit[p.doc] = true
			}
			scores[p.doc] += score
			matched[p.doc] += idf
		}
	}
	if len(scores) == 0 {
		return nil
	}

	out := make([]Result, 0, len(scores))
	for doc, score := range scores {
		cov := 0.0
		if totalIDF > 0 {
			cov = matched[doc] / totalIDF
		}
		out = append(out, Result{
			Chunk: ix.chunks[doc], Score: score, Coverage: cov, TopicHit: topicHit[doc],
		})
	}
	// Ties break on the chunk id so the same question always produces the same citation order —
	// a chat answer that reorders its sources between identical runs reads as unreliable.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Chunk.ID() < out[j].Chunk.ID()
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
