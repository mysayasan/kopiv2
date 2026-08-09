// Package manualcheck is the conformance suite every app's manual runs against itself.
//
// It lives in its own package (rather than a _test.go file in `manual`) so an app can call it
// from its own test without importing `testing` into the shipped package.
//
// What it guards is specific and was chosen because each failure mode is silent in production:
//
//   - A forgotten translation. With four language folders, the way a manual rots is that
//     someone adds an English article and stops. Nothing breaks at runtime — the loader falls
//     back to English — so nobody notices for months. LanguageParity turns that into a red test.
//   - A dead cross-link. Internal links are bare slugs, so a renamed file leaves a link that
//     silently does nothing when clicked.
//   - A drifted anchor. Contextual "?" buttons in the UI deep-link to `{#anchor}` ids. If a
//     translator drops or renames one, the deep link lands at the top of the page in that
//     language only — the single most annoying bug to notice by hand.
package manualcheck

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/domain/shared/manual"
)

// internalLink matches a markdown link whose target is not an absolute URL or a fragment-only
// anchor — i.e. a link to another manual article, optionally with an anchor.
var internalLink = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)

// headingAnchor matches the explicit stable anchor on a heading: `## Credentials {#credentials}`.
var headingAnchor = regexp.MustCompile(`(?m)^#{1,6}[^\n]*\{#([A-Za-z0-9._-]+)\}\s*$`)

// Library runs every check against one app's manual. Call it from the app's own test:
//
//	func TestManual(t *testing.T) { manualcheck.Library(t, manual.Library) }
func Library(t *testing.T, lib *manual.Library) {
	t.Helper()
	langs := lib.Languages()
	if len(langs) == 0 {
		t.Fatal("manual ships no language folders — is the //go:embed pattern still matching?")
	}
	if langs[0] != manual.DefaultLanguage {
		t.Fatalf("manual has no %q folder (found %v); every other language falls back to it", manual.DefaultLanguage, langs)
	}

	t.Run("Metadata", func(t *testing.T) { metadata(t, lib, langs) })
	t.Run("LanguageParity", func(t *testing.T) { languageParity(t, lib, langs) })
	t.Run("Links", func(t *testing.T) { links(t, lib, langs) })
	t.Run("Anchors", func(t *testing.T) { anchors(t, lib, langs) })
}

// metadata asserts every article carries the frontmatter the index and print TOC need.
func metadata(t *testing.T, lib *manual.Library, langs []string) {
	t.Helper()
	for _, lang := range langs {
		for _, a := range lib.Bundle(lang) {
			if a.Language != lang {
				continue // untranslated: reported by LanguageParity, not here
			}
			if strings.TrimSpace(a.Title) == "" {
				t.Errorf("%s/%s: empty title", lang, a.Slug)
			}
			if strings.TrimSpace(a.Category) == "" {
				t.Errorf("%s/%s: empty category (the stable grouping id)", lang, a.Slug)
			}
			if strings.TrimSpace(a.CategoryLabel) == "" {
				t.Errorf("%s/%s: empty categoryLabel (the translated section heading)", lang, a.Slug)
			}
			if strings.TrimSpace(a.Summary) == "" {
				t.Errorf("%s/%s: empty summary (shown in search results and the print TOC)", lang, a.Slug)
			}
			if a.Order == 0 {
				t.Errorf("%s/%s: no order — it will sort by title instead of reading order", lang, a.Slug)
			}
			if strings.TrimSpace(a.Body) == "" {
				t.Errorf("%s/%s: empty body", lang, a.Slug)
			}
		}
	}

	// The stable category id must map to exactly one order-of-appearance across an app, and an
	// article's category must not disagree between languages.
	perSlug := map[string]string{}
	for _, lang := range langs {
		for _, a := range lib.Bundle(lang) {
			if a.Language != lang {
				continue
			}
			if want, seen := perSlug[a.Slug]; seen && want != a.Category {
				t.Errorf("%s/%s: category %q disagrees with %q used by another language — grouping is keyed on this id", lang, a.Slug, a.Category, want)
				continue
			}
			perSlug[a.Slug] = a.Category
		}
	}
}

// languageParity asserts every language folder holds the same set of articles as English.
func languageParity(t *testing.T, lib *manual.Library, langs []string) {
	t.Helper()
	want := translatedSlugs(lib, manual.DefaultLanguage)
	for _, lang := range langs {
		if lang == manual.DefaultLanguage {
			continue
		}
		have := translatedSlugs(lib, lang)
		for _, slug := range sortedKeys(want) {
			if !have[slug] {
				t.Errorf("%s/%s.md is missing — every article must exist in every language", lang, slug)
			}
		}
		for _, slug := range sortedKeys(have) {
			if !want[slug] {
				t.Errorf("%s/%s.md has no English original — the English folder is the index of record", lang, slug)
			}
		}
	}
}

// links asserts every internal cross-link resolves to an article that exists.
func links(t *testing.T, lib *manual.Library, langs []string) {
	t.Helper()
	known := translatedSlugs(lib, manual.DefaultLanguage)
	for _, lang := range langs {
		for _, a := range lib.Bundle(lang) {
			if a.Language != lang {
				continue
			}
			for _, m := range internalLink.FindAllStringSubmatch(a.Body, -1) {
				target := m[1]
				if isExternal(target) {
					continue
				}
				slug, _, _ := strings.Cut(strings.TrimPrefix(target, "#"), "#")
				if strings.HasPrefix(target, "#") || slug == "" {
					continue // same-page anchor
				}
				if !known[slug] {
					t.Errorf("%s/%s: link to %q, but no such article exists", lang, a.Slug, slug)
				}
			}
		}
	}
}

// anchors asserts explicit heading anchors are unique inside an article and identical across
// that article's translations — the property every contextual deep link depends on.
func anchors(t *testing.T, lib *manual.Library, langs []string) {
	t.Helper()
	base := map[string][]string{}
	for _, a := range lib.Bundle(manual.DefaultLanguage) {
		base[a.Slug] = articleAnchors(t, manual.DefaultLanguage, a)
	}
	for _, lang := range langs {
		if lang == manual.DefaultLanguage {
			continue
		}
		for _, a := range lib.Bundle(lang) {
			if a.Language != lang {
				continue
			}
			got := articleAnchors(t, lang, a)
			want := base[a.Slug]
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("%s/%s: anchors %v differ from English %v — contextual help links would miss in this language", lang, a.Slug, got, want)
			}
		}
	}
}

func articleAnchors(t *testing.T, lang string, a manual.Article) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, m := range headingAnchor.FindAllStringSubmatch(a.Body, -1) {
		id := m[1]
		if seen[id] {
			t.Errorf("%s/%s: duplicate anchor {#%s}", lang, a.Slug, id)
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// translatedSlugs is the set of articles a language folder REALLY holds — English fallbacks are
// excluded, which is the whole point when checking for a missing translation.
func translatedSlugs(lib *manual.Library, lang string) map[string]bool {
	out := map[string]bool{}
	for _, a := range lib.Bundle(lang) {
		if a.Language == lang {
			out[a.Slug] = true
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isExternal(target string) bool {
	return strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:")
}
