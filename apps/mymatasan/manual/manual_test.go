package manual_test

import (
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/manual"
	"github.com/mysayasan/kopiv2/domain/shared/manual/manualcheck"
)

// TestManual runs the suite-wide conformance checks against mymatasan's shipped articles: every
// page has the frontmatter the index and print TOC need, every language folder holds the same
// articles, every cross-link resolves, and every heading anchor a contextual "?" button can point
// at exists identically in all four languages.
func TestManual(t *testing.T) {
	manualcheck.Library(t, manual.Library)
}
