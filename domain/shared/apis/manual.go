package apis

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/mysayasan/kopiv2/domain/shared/manual"
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
}

// NewManualHandlers builds the handler set over one app's manual library.
func NewManualHandlers(lib *manual.Library) *ManualHandlers {
	return &ManualHandlers{lib: lib}
}

// List returns the article index for the requested language (`?lang=ms`), metadata only.
// It also reports which languages the manual actually ships, so the reader is never offered a
// language that would silently render as English.
func (h *ManualHandlers) List(w http.ResponseWriter, r *http.Request) {
	lang := h.lib.Language(language(r))
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
	controllers.SendResult(w, map[string]any{
		"language":  lang,
		"languages": h.lib.Languages(),
		"items":     h.lib.Bundle(lang),
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
