package manual

import (
	"strconv"
	"testing"
	"testing/fstest"
)

// article builds a frontmatter'd markdown file the way a real manual page is written.
func article(title, category, label, summary string, order int, body string) *fstest.MapFile {
	front := "---\ntitle: " + title + "\ncategory: " + category + "\ncategoryLabel: " + label +
		"\nsummary: " + summary + "\norder: " + strconv.Itoa(order) + "\n---\n\n" + body + "\n"
	return &fstest.MapFile{Data: []byte(front)}
}

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"en/10-welcome.md":  article("Welcome", "start", "Getting started", "What this is", 10, "# Welcome\n\nHello."),
		"en/20-signing.md":  article("Signing in", "start", "Getting started", "First sign-in", 20, "# Signing in\n\nSee [Welcome](welcome)."),
		"ms/10-welcome.md":  article("Selamat datang", "start", "Permulaan", "Apa ini", 10, "# Selamat datang\n\nHai."),
		"assets/figure.svg": &fstest.MapFile{Data: []byte("<svg/>")},
	}
}

func TestArticlesAreOrderedAndBodyless(t *testing.T) {
	lib := New(testFS(), ".")
	list := lib.Articles("en")
	if len(list) != 2 {
		t.Fatalf("want 2 articles, got %d", len(list))
	}
	if list[0].Slug != "welcome" || list[1].Slug != "signing" {
		t.Fatalf("wrong order/slugs: %q, %q", list[0].Slug, list[1].Slug)
	}
	for _, a := range list {
		if a.Body != "" {
			t.Errorf("%s: index payload must not carry a body", a.Slug)
		}
	}
}

// The numeric prefix is an on-disk ordering device only. If it leaked into the slug, renumbering
// a folder would silently break every cross-link and every contextual help deep link.
func TestSlugDropsOrderingPrefix(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{"10-welcome.md", "welcome"},
		{"010_adding-cameras.md", "adding-cameras"},
		{"faq.md", "faq"},
		{"2026-in-review.md", "in-review"},
	} {
		if got := Slug(tc.file); got != tc.want {
			t.Errorf("Slug(%q) = %q, want %q", tc.file, got, tc.want)
		}
	}
}

// A partially translated language must still show the WHOLE manual, with the untranslated pages
// falling back to English — and must say which language each body really came from, so the UI
// can tell the reader.
func TestUntranslatedArticleFallsBackToEnglish(t *testing.T) {
	lib := New(testFS(), ".")
	list := lib.Bundle("ms")
	if len(list) != 2 {
		t.Fatalf("want the full manual in ms, got %d articles", len(list))
	}
	if list[0].Slug != "welcome" || list[0].Language != "ms" {
		t.Errorf("welcome should come from ms, got %q from %q", list[0].Slug, list[0].Language)
	}
	if list[1].Slug != "signing" || list[1].Language != "en" {
		t.Errorf("signing is untranslated and should fall back to en, got %q from %q", list[1].Slug, list[1].Language)
	}
}

func TestUnknownLanguageFallsBackToEnglish(t *testing.T) {
	lib := New(testFS(), ".")
	list := lib.Bundle("de")
	if len(list) != 2 || list[0].Language != "en" {
		t.Fatalf("unknown language should serve English, got %d articles from %q", len(list), list[0].Language)
	}
}

// An unshipped locale must resolve to the language actually SERVED, not be echoed back. The
// reader compares this against each article's Language to decide whether to show the "not
// translated yet" notice, so echoing "de" would flag every article on the page.
func TestLanguageResolvesToAServedFolder(t *testing.T) {
	lib := New(testFS(), ".")
	for _, tc := range []struct{ ask, want string }{
		{"ms", "ms"},
		{"de", "en"},
		{"", "en"},
		{" MS ", "ms"},
	} {
		if got := lib.Language(tc.ask); got != tc.want {
			t.Errorf("Language(%q) = %q, want %q", tc.ask, got, tc.want)
		}
	}
}

func TestLanguagesPutsEnglishFirst(t *testing.T) {
	lib := New(testFS(), ".")
	langs := lib.Languages()
	if len(langs) != 2 || langs[0] != "en" || langs[1] != "ms" {
		t.Fatalf("want [en ms], got %v", langs)
	}
}

// assets/ is a reserved folder, not a language. Treating it as one would put an empty locale in
// the language list and let a reader "switch to assets".
func TestAssetsDirIsNotALanguage(t *testing.T) {
	lib := New(testFS(), ".")
	for _, lang := range lib.Languages() {
		if lang == "assets" {
			t.Fatal("assets/ was loaded as a language folder")
		}
	}
}

func TestAssetRejectsTraversal(t *testing.T) {
	lib := New(testFS(), ".")
	if _, ok := lib.Asset("figure.svg"); !ok {
		t.Fatal("a real asset should be readable")
	}
	for _, name := range []string{"", "../en/10-welcome.md", "sub/figure.svg", `..\en\10-welcome.md`} {
		if _, ok := lib.Asset(name); ok {
			t.Errorf("Asset(%q) should be rejected", name)
		}
	}
}

// Callers blank out bodies to build index payloads; that must not empty the cache for the next
// caller who asks for the same language.
func TestArticlesDoesNotMutateTheCache(t *testing.T) {
	lib := New(testFS(), ".")
	_ = lib.Articles("en")
	for _, a := range lib.Bundle("en") {
		if a.Body == "" {
			t.Fatalf("%s: body was emptied by a previous Articles() call", a.Slug)
		}
	}
}

func TestParseReadsFrontmatter(t *testing.T) {
	a := Parse("---\ntitle: T\ncategory: c\ncategoryLabel: C\nsummary: S\norder: 7\n---\n\nbody text\n")
	if a.Title != "T" || a.Category != "c" || a.CategoryLabel != "C" || a.Summary != "S" || a.Order != 7 {
		t.Fatalf("bad frontmatter parse: %+v", a)
	}
	if a.Body != "body text" {
		t.Fatalf("bad body: %q", a.Body)
	}
}

func TestParseWithoutFrontmatterKeepsWholeBody(t *testing.T) {
	a := Parse("# Just markdown\n\ntext")
	if a.Body != "# Just markdown\n\ntext" {
		t.Fatalf("body was mangled: %q", a.Body)
	}
}
