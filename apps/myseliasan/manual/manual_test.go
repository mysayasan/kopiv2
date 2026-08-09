package manual_test

import (
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/manual"
	"github.com/mysayasan/kopiv2/domain/shared/manual/manualcheck"
)

// TestManual runs the suite-wide conformance checks against myseliasan's shipped articles: every
// page has the frontmatter the index and print TOC need, every language folder holds the same
// articles, every cross-link resolves, and every heading anchor a contextual "?" button can point
// at exists identically in all four languages.
func TestManual(t *testing.T) {
	manualcheck.Library(t, manual.Library)
}

// TestManualUIReferences checks the other direction: every article slug and heading anchor that a
// contextual "?" button in the SPA points at must actually exist. Renaming an article or dropping
// a `{#anchor}` breaks nothing at build or run time — the button just opens the wrong page — so
// this is the only thing that catches it.
func TestManualUIReferences(t *testing.T) {
	manualcheck.UIReferences(t, manual.Library, "../views/react-webpack/src/views")
}
