// Package kb is the shipped, in-app knowledge base: markdown articles compiled into the binary so
// the appliance can show setup guides with no network access (the same air-gapped posture the rest
// of the product keeps). The articles live under kb/solar as plain markdown — they are the repo's
// source of truth and are served verbatim; the frontend renders them.
package kb

import (
	"embed"
	"sort"
	"strconv"
	"strings"
)

//go:embed solar/*.md
var files embed.FS

// Article is one KB page. Body is the raw markdown (frontmatter stripped); the client renders it.
type Article struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Order    int    `json:"order"`
	Body     string `json:"body,omitempty"`
}

// Articles returns every shipped article WITHOUT its body, sorted by (Order, Title) — the payload
// the list view needs. Loading is cheap (files are embedded) but done once and cached.
func Articles() []Article {
	all := load()
	out := make([]Article, len(all))
	for i, a := range all {
		a.Body = "" // list payload carries metadata only
		out[i] = a
	}
	return out
}

// Get returns one article, body included, by slug.
func Get(slug string) (Article, bool) {
	for _, a := range load() {
		if a.Slug == slug {
			return a, true
		}
	}
	return Article{}, false
}

var cached []Article

func load() []Article {
	if cached != nil {
		return cached
	}
	entries, err := files.ReadDir("solar")
	if err != nil {
		return nil
	}
	var out []Article
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := files.ReadFile("solar/" + e.Name())
		if err != nil {
			continue
		}
		a := parse(string(raw))
		a.Slug = strings.TrimSuffix(e.Name(), ".md")
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Title < out[j].Title
	})
	cached = out
	return out
}

// parse splits an optional `--- key: value ... ---` frontmatter block off the top and returns the
// metadata plus the remaining markdown body.
func parse(content string) Article {
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
	// Drop the rest of the closing delimiter line.
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
		case "order":
			a.Order, _ = strconv.Atoi(v)
		}
	}
	a.Body = strings.TrimSpace(body)
	return a
}
