package kb

import "testing"

// TestArticlesLoadAndParse proves the embedded markdown loads, the frontmatter is stripped, and the
// list is ordered — the whole in-app KB depends on this working with no filesystem or network.
func TestArticlesLoadAndParse(t *testing.T) {
	arts := Articles()
	if len(arts) < 5 {
		t.Fatalf("expected the shipped solar articles to be embedded, got %d", len(arts))
	}
	// The list payload must not carry bodies.
	for _, a := range arts {
		if a.Body != "" {
			t.Errorf("article %q carried a body in the list payload", a.Slug)
		}
		if a.Title == "" {
			t.Errorf("article %q has no title (frontmatter not parsed?)", a.Slug)
		}
	}
	// Ordered by Order ascending.
	for i := 1; i < len(arts); i++ {
		if arts[i-1].Order > arts[i].Order {
			t.Errorf("articles not sorted by order: %d before %d", arts[i-1].Order, arts[i].Order)
		}
	}
}

func TestGetArticleReturnsBodyWithoutFrontmatter(t *testing.T) {
	a, ok := Get("index")
	if !ok {
		t.Fatal("index article not found")
	}
	if a.Title == "" {
		t.Error("index has no title")
	}
	if a.Body == "" {
		t.Fatal("index has no body")
	}
	if len(a.Body) >= 3 && a.Body[:3] == "---" {
		t.Error("frontmatter delimiter leaked into the body")
	}
	if _, ok := Get("does-not-exist"); ok {
		t.Error("Get returned ok for a missing slug")
	}
}
