package manual_test

import (
	"fmt"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/manual"
	"github.com/mysayasan/kopiv2/domain/shared/manual/manualcheck"
	"github.com/mysayasan/kopiv2/infra/pairing"
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

// TestManualSpecValues asserts that the ports the manual states are the ports the fleet uses.
//
// The same four numbers appear in mymatasan's manual, described from the node's side; this one
// describes them from the control plane's. Both are asserted against the SAME constants, so the
// two manuals cannot drift apart from each other either — which a reader comparing the two pages
// would notice long before anyone else did.
func TestManualSpecValues(t *testing.T) {
	manualcheck.SpecValues(t, manual.Library, map[string]string{
		"adopting-nodes/discovery": fmt.Sprintf("%d/udp", pairing.DefaultDiscoveryPort),
		"adopting-nodes/mtls":      fmt.Sprintf("%d/tcp", pairing.DefaultMTLSPort),
		"adopting-nodes/control":   fmt.Sprintf("%d/tcp", pairing.DefaultControlPort),
		"adopting-nodes/media":     fmt.Sprintf("%d/tcp", pairing.DefaultMediaPort),
	})
}
