package apis

import (
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/mysayasan/kopiv2/domain/shared/manual"
	"github.com/mysayasan/kopiv2/domain/shared/manual/retrieval"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// ManualHandlers serves an app's built-in user manual: the article index, one article, the whole
// book in one payload, and figure assets.
//
// Route registration is deliberately left to each app, the same way SetupHandlers does it — the
// apps differ in where the manual sits in their middleware chain. mymatasan mounts it on the
// PUBLIC router so the help link works on the sign-in screen and inside the first-run wizard,
// which is exactly where a reader is most stuck and least able to authenticate.
//
// Everything served here is shipped, read-only reference content compiled into the binary. There
// is no runtime state and no per-user data, so there is nothing here to leak between roles.
type ManualHandlers struct {
	lib *manual.Library

	// corpus is the ranked search index over lib, built on the first search and never before.
	// A reader who only ever clicks through the contents pays nothing for it.
	corpusOnce sync.Once
	corpus     *retrieval.Corpus
}

// NewManualHandlers builds the handler set over one app's manual library.
func NewManualHandlers(lib *manual.Library) *ManualHandlers {
	return &ManualHandlers{lib: lib}
}

// index builds the search corpus lazily. It is a corpus of ONE source: unlike the fleet agent,
// which searches several apps' manuals and must say which product an answer came from, an
// appliance searching its own manual has only one possible answer to that question.
func (h *ManualHandlers) index() *retrieval.Corpus {
	h.corpusOnce.Do(func() {
		h.corpus = retrieval.New(retrieval.Source{App: "self", Library: h.lib})
	})
	return h.corpus
}

// List returns the article index for the requested language (`?lang=ms`), metadata only.
// It also reports which languages the manual actually ships, so the reader is never offered a
// language that would silently render as English.
func (h *ManualHandlers) List(w http.ResponseWriter, r *http.Request) {
	lang := h.lib.Language(language(r))
	cacheable(w)
	controllers.SendResult(w, map[string]any{
		"language":  lang,
		"languages": h.lib.Languages(),
		"items":     h.lib.Articles(lang),
	}, "succeed")
}

// Bundle returns every article WITH its body. The client fetches this once and then searches and
// prints entirely offline — no per-article round trip, and no server-side search index to keep
// in step with the content.
func (h *ManualHandlers) Bundle(w http.ResponseWriter, r *http.Request) {
	lang := h.lib.Language(language(r))
	cacheable(w)
	controllers.SendResult(w, map[string]any{
		"language":  lang,
		"languages": h.lib.Languages(),
		"items":     h.lib.Bundle(lang),
	}, "succeed")
}

// Search ranks the manual against a question (`?q=how+do+i+adopt+a+node&lang=ms`).
//
// The reader has always been able to search — by substring, client-side, over the bundle it had
// already downloaded. That finds a page only when the reader guessed a word the author used.
// This ranks by BM25 with the article's own structure as field weights, so "the door does not
// unlock" reaches the troubleshooting section without sharing a word with its title, and it
// returns the SECTION rather than the article, so a result opens where the answer is.
//
// The engine is the one the fleet agent already grounds its answers in; nothing new is indexed,
// and its golden tests keep asserting that a given question finds a given section.
//
// A question asked in ms/zh/ar falls back to English when its own language has little to say —
// operators type English product nouns (ONVIF, RTSP, Modbus) inside a Malay sentence constantly,
// and each result carries the language it came from so the reader can be told.
func (h *ManualHandlers) Search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	lang := h.lib.Language(language(r))

	limit := 8
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 25 {
		limit = n
	}

	// An empty or one-character query is a reader still typing, not a question. Answering it
	// would rank the whole manual by noise, so it returns nothing rather than something wrong.
	items := []map[string]any{}
	if len([]rune(query)) >= 2 {
		for _, hit := range h.index().Search(lang, query, limit) {
			items = append(items, map[string]any{
				"slug":     hit.Chunk.Slug,
				"anchor":   hit.Chunk.Anchor,
				"title":    hit.Chunk.ArticleTitle,
				"heading":  hit.Chunk.Heading,
				"language": hit.Chunk.Lang,
				"snippet":  hit.Chunk.Snippet(240),
			})
		}
	}

	cacheable(w)
	controllers.SendResult(w, map[string]any{
		"language": lang,
		"query":    query,
		"items":    items,
	}, "succeed")
}

// Get returns one article by slug. slug comes from the caller because the mux variable name is
// the app's choice, not this package's.
func (h *ManualHandlers) Get(w http.ResponseWriter, r *http.Request, slug string) {
	article, ok := h.lib.Get(language(r), slug)
	if !ok {
		controllers.SendError(w, controllers.ErrNotFound, "no such manual article")
		return
	}
	controllers.SendResult(w, map[string]any{"article": article}, "succeed")
}

// Asset serves a manual figure verbatim. Unknown extensions fall back to a generic binary type
// rather than being sniffed, and the response is marked nosniff, so a figure can never be
// coaxed into executing in the reader's browser.
func (h *ManualHandlers) Asset(w http.ResponseWriter, r *http.Request, name string) {
	data, ok := h.lib.Asset(name)
	if !ok {
		controllers.SendError(w, controllers.ErrNotFound, "no such manual asset")
		return
	}
	ctype := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Embedded content only changes when the binary does, so it is safe to cache for a while;
	// the SPA's own cache-busted bundle is what pulls in a new manual after an upgrade.
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// language reads the requested locale off the query string. An unknown or absent value resolves
// to English inside the library, so no validation is needed here.
func language(r *http.Request) string {
	return strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
}

// cacheable marks a manual response as safe to cache for a while.
//
// The content is compiled into the binary, so it cannot change until the binary does — and the
// SPA's own cache-busted bundle is what pulls in a new manual after an upgrade. This matters
// more than it looks: these routes are UNAUTHENTICATED, and the bundle is a few hundred
// kilobytes, so without it a client (or a bored network peer) re-fetches the whole book on every
// navigation. The shared rate limiter bounds abuse; this removes the ordinary case.
func cacheable(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
}
