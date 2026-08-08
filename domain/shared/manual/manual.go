// Package manual is the suite's built-in user manual: markdown articles compiled into each app's
// binary so an appliance can show its own documentation with no network access — the same
// air-gapped posture the rest of the product keeps.
//
// It generalizes what apps/myiotsan/kb proved for a single app's setup guides: one language, no
// search, no print, no deep links. A manual needs all four, and it needs to be written ONCE and
// reused, so the loader here is app-agnostic. An app supplies an fs.FS laid out as
//
//	<root>/en/*.md   <root>/ms/*.md   <root>/zh/*.md   <root>/ar/*.md   <root>/assets/*
//
// and gets an index, per-article bodies, a whole-book bundle (for client-side search and print),
// and figure assets.
//
// # Slugs are the filename, not the title
//
// The FILENAME is the article's identity and is identical in every language folder. That is what
// makes a cross-link (`[see this](adding-cameras)`) and a contextual deep link from the UI keep
// working when the reader switches to Chinese — a title-derived slug would not. The numeric
// prefix used for on-disk ordering (`10-welcome.md`) is stripped, so renumbering a folder does
// not break links.
package manual

import (
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// DefaultLanguage is the language every other one falls back to, article by article. A partial
// translation therefore degrades to English prose rather than a 404 or an empty page.
const DefaultLanguage = "en"

// assetsDir is the reserved folder name for figures; it is not a language.
const assetsDir = "assets"

// Article is one manual page.
//
// Category is a STABLE id used for grouping (never translated); CategoryLabel is what a reader
// sees. Keeping them apart is what lets the four language folders group into the same sections.
// Body is raw markdown — the client renders it — and is omitted from index payloads.
type Article struct {
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	Category      string `json:"category"`
	CategoryLabel string `json:"categoryLabel,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Order         int    `json:"order"`
	// Language is the folder the body actually came from. It differs from the requested
	// language when this article has not been translated yet, which is exactly when the UI
	// wants to say so.
	Language string `json:"language,omitempty"`
	Body     string `json:"body,omitempty"`
}

// Library is one app's manual: every language, loaded once from the embedded filesystem.
type Library struct {
	fsys fs.FS
	root string

	once sync.Once
	// byLang holds each language's articles in display order.
	byLang map[string][]Article
	langs  []string
}

// New builds a library over an embedded filesystem. root is the directory holding the language
// folders ("." when the app embeds `en ms zh ar assets` from the package directory).
//
// Nothing is read until the first query, so constructing one at package scope is free.
func New(fsys fs.FS, root string) *Library {
	if root == "" {
		root = "."
	}
	return &Library{fsys: fsys, root: root}
}

// Languages returns the language folders this library actually ships, English first.
func (l *Library) Languages() []string {
	l.load()
	return append([]string(nil), l.langs...)
}

// Language reports which language a request actually resolves to. Callers echo THIS rather than
// what was asked for: the reader's UI compares it against each article's own Language to decide
// whether to say "not translated yet", and echoing an unshipped locale back would make every
// article on the page claim to be untranslated.
func (l *Library) Language(lang string) string {
	l.load()
	return l.normalize(lang)
}

// Articles returns the index for a language — metadata only, no bodies — sorted by (Order, Title).
// Every article the library knows about is present: one missing from this language is filled in
// from DefaultLanguage, with Language reporting where the text really came from.
func (l *Library) Articles(lang string) []Article {
	out := l.resolve(lang)
	for i := range out {
		out[i].Body = ""
	}
	return out
}

// Bundle returns every article for a language WITH bodies. The client fetches this once to power
// full-text search and whole-manual printing without a round trip per article — which is what
// keeps both working on an air-gapped appliance.
func (l *Library) Bundle(lang string) []Article {
	return l.resolve(lang)
}

// Get returns one article, body included.
func (l *Library) Get(lang, slug string) (Article, bool) {
	for _, a := range l.resolve(lang) {
		if a.Slug == slug {
			return a, true
		}
	}
	return Article{}, false
}

// Asset reads a figure from <root>/assets. name is a bare file name; any path separator or
// traversal is rejected rather than cleaned, because a manual figure never legitimately needs
// one and a silent clean is how a traversal bug gets written.
func (l *Library) Asset(name string) ([]byte, bool) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return nil, false
	}
	data, err := fs.ReadFile(l.fsys, path.Join(l.root, assetsDir, name))
	if err != nil {
		return nil, false
	}
	return data, true
}

// resolve returns a language's articles with English filling any gap, freshly copied so callers
// (which blank out bodies) cannot mutate the cache.
func (l *Library) resolve(lang string) []Article {
	l.load()
	base := l.byLang[DefaultLanguage]
	want := l.byLang[l.normalize(lang)]

	// Index the requested language by slug, then walk the English list so ordering and
	// completeness come from one place regardless of what the translation folder holds.
	index := make(map[string]Article, len(want))
	for _, a := range want {
		index[a.Slug] = a
	}
	source := base
	if len(source) == 0 {
		source = want
	}
	out := make([]Article, 0, len(source))
	for _, en := range source {
		if a, ok := index[en.Slug]; ok {
			out = append(out, a)
			continue
		}
		out = append(out, en)
	}
	sortArticles(out)
	return out
}

// normalize maps a requested language onto a folder this library actually ships.
func (l *Library) normalize(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if _, ok := l.byLang[lang]; ok {
		return lang
	}
	return DefaultLanguage
}

func (l *Library) load() {
	l.once.Do(func() {
		l.byLang = map[string][]Article{}
		entries, err := fs.ReadDir(l.fsys, l.root)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == assetsDir {
				continue
			}
			lang := e.Name()
			articles := l.loadLang(lang)
			if len(articles) == 0 {
				continue
			}
			l.byLang[lang] = articles
			l.langs = append(l.langs, lang)
		}
		sort.SliceStable(l.langs, func(i, j int) bool {
			if (l.langs[i] == DefaultLanguage) != (l.langs[j] == DefaultLanguage) {
				return l.langs[i] == DefaultLanguage
			}
			return l.langs[i] < l.langs[j]
		})
	})
}

func (l *Library) loadLang(lang string) []Article {
	entries, err := fs.ReadDir(l.fsys, path.Join(l.root, lang))
	if err != nil {
		return nil
	}
	var out []Article
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := fs.ReadFile(l.fsys, path.Join(l.root, lang, e.Name()))
		if err != nil {
			continue
		}
		a := Parse(string(raw))
		a.Slug = Slug(e.Name())
		a.Language = lang
		if a.Title == "" {
			a.Title = a.Slug
		}
		out = append(out, a)
	}
	sortArticles(out)
	return out
}

func sortArticles(list []Article) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Order != list[j].Order {
			return list[i].Order < list[j].Order
		}
		return list[i].Title < list[j].Title
	})
}

// orderPrefix matches the numeric ordering prefix on a file name ("10-welcome.md").
var orderPrefix = regexp.MustCompile(`^\d+[-_]`)

// Slug turns a file name into the article's stable id: the numeric ordering prefix and the .md
// extension are dropped, so files can be renumbered without breaking a single link.
func Slug(fileName string) string {
	name := strings.TrimSuffix(fileName, ".md")
	return orderPrefix.ReplaceAllString(name, "")
}

// Parse splits an optional `--- key: value ... ---` frontmatter block off the top of an article
// and returns the metadata plus the remaining markdown body.
func Parse(content string) Article {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	a := Article{}
	if !strings.HasPrefix(content, "---\n") {
		a.Body = strings.TrimSpace(content)
		return a
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		a.Body = strings.TrimSpace(content)
		return a
	}
	front := rest[:end]
	body := rest[end+len("\n---"):]
	// Drop the remainder of the closing delimiter line.
	if nl := strings.Index(body, "\n"); nl >= 0 {
		body = body[nl+1:]
	} else {
		body = ""
	}
	for _, line := range strings.Split(front, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "title":
			a.Title = v
		case "category":
			a.Category = v
		case "categoryLabel":
			a.CategoryLabel = v
		case "summary":
			a.Summary = v
		case "order":
			a.Order, _ = strconv.Atoi(v)
		}
	}
	a.Body = strings.TrimSpace(body)
	return a
}
