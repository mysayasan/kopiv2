package manual_test

import (
	"fmt"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/manual"
	"github.com/mysayasan/kopiv2/domain/shared/manual/manualcheck"
	"github.com/mysayasan/kopiv2/infra/pairing"
)

// TestManual runs the suite-wide conformance checks against mymatasan's shipped articles: every
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

// TestManualSpecValues asserts that the literal values the manual states are the ones the software
// actually uses.
//
// This is what separates a reference table from a plausible-looking one. A ```spec row is the part
// of a page an integrator copies into a firewall rule without checking, so a wrong number here
// costs them an afternoon, where a missing one costs a support call — which makes a stale
// reference section worse than none at all. The manual is only allowed to make these claims
// because this test fails when they stop being true.
//
// A renamed or deleted row also fails, rather than silently stopping being checked.
func TestManualSpecValues(t *testing.T) {
	manualcheck.SpecValues(t, manual.Library, map[string]string{
		"control-plane/discovery": fmt.Sprintf("%d/udp", pairing.DefaultDiscoveryPort),
		"control-plane/mtls":      fmt.Sprintf("%d/tcp", pairing.DefaultMTLSPort),
		"control-plane/control":   fmt.Sprintf("%d/tcp", pairing.DefaultControlPort),
		"control-plane/media":     fmt.Sprintf("%d/tcp", pairing.DefaultMediaPort),
	})
}
