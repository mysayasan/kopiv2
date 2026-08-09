package manualcheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/domain/shared/manual"
)

// The four shapes a contextual help target takes in an app's frontend. Keeping the recognized
// forms narrow is deliberate: a new shape should fail this check loudly rather than be silently
// skipped, which is the failure mode that lets a dead deep link ship.
var (
	// <HelpButton slug="camera-properties" anchor="stream" />
	jsxRef = regexp.MustCompile(`slug="([A-Za-z0-9._-]+)"(?:\s+anchor="([A-Za-z0-9._-]*)")?`)
	// help: ['settings-reference', 'runtime']   (a tab-table column)
	helpFieldRef = regexp.MustCompile(`help:\s*\[\s*'([A-Za-z0-9._-]+)'\s*,\s*'([A-Za-z0-9._-]*)'\s*\]`)
	// The TAB_HELP map: dashboard: ['dashboard', ''],
	tabHelpBlock = regexp.MustCompile(`(?s)const TAB_HELP\s*=\s*\{(.*?)\n\};`)
	tabHelpEntry = regexp.MustCompile(`[A-Za-z0-9_]+:\s*\[\s*'([A-Za-z0-9._-]+)'\s*,\s*'([A-Za-z0-9._-]*)'\s*\]`)
	// A stepped surface (the setup wizard) declares one slug and an index-aligned anchor list:
	//   const STEP_HELP = { slug: 'setup-wizard', anchors: ['welcome', 'system', …] };
	// It is declared as data precisely so it can be checked; an inlined `ANCHORS[step]` could not.
	stepHelpBlock  = regexp.MustCompile(`(?s)const\s+[A-Za-z0-9_]*STEP_HELP\s*=\s*\{(.*?)\n\};`)
	stepHelpSlug   = regexp.MustCompile(`slug:\s*'([A-Za-z0-9._-]+)'`)
	stepHelpAnchor = regexp.MustCompile(`'([A-Za-z0-9._-]+)'`)
)

// UIReferences checks every contextual-help target written into an app's frontend against the
// manual it ships.
//
// This exists because the two halves drift apart silently. A "?" button names an article slug and
// a heading anchor as bare strings in JSX; rename the article file or drop the `{#anchor}` from a
// heading and nothing fails to compile, nothing fails at runtime, and the button simply opens the
// wrong page — or the right page scrolled to the top — for as long as it takes somebody to notice.
// Scanning the real source (rather than a hand-kept list that would drift in its own right) is
// what makes this a guard instead of a second thing to maintain.
//
// root is the frontend directory to walk, relative to the calling test.
func UIReferences(t *testing.T, lib *manual.Library, root string) {
	t.Helper()

	known := map[string][]string{}
	for _, a := range lib.Bundle(manual.DefaultLanguage) {
		known[a.Slug] = articleAnchors(t, manual.DefaultLanguage, a)
	}

	type ref struct{ slug, anchor, file string }
	var refs []ref

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".js") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(raw)
		rel, _ := filepath.Rel(root, path)

		for _, m := range jsxRef.FindAllStringSubmatch(src, -1) {
			refs = append(refs, ref{slug: m[1], anchor: m[2], file: rel})
		}
		for _, m := range helpFieldRef.FindAllStringSubmatch(src, -1) {
			refs = append(refs, ref{slug: m[1], anchor: m[2], file: rel})
		}
		if block := tabHelpBlock.FindStringSubmatch(src); block != nil {
			for _, m := range tabHelpEntry.FindAllStringSubmatch(block[1], -1) {
				refs = append(refs, ref{slug: m[1], anchor: m[2], file: rel})
			}
		}
		if block := stepHelpBlock.FindStringSubmatch(src); block != nil {
			slugMatch := stepHelpSlug.FindStringSubmatch(block[1])
			if slugMatch == nil {
				t.Errorf("%s: STEP_HELP has no slug", rel)
				return nil
			}
			body := block[1]
			if at := strings.Index(body, "anchors:"); at >= 0 {
				for _, m := range stepHelpAnchor.FindAllStringSubmatch(body[at:], -1) {
					refs = append(refs, ref{slug: slugMatch[1], anchor: m[1], file: rel})
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(refs) == 0 {
		t.Fatalf("no contextual help targets found under %s — has the HelpButton syntax changed? "+
			"An empty result here would silently stop guarding every deep link.", root)
	}

	for _, r := range refs {
		anchors, ok := known[r.slug]
		if !ok {
			t.Errorf("%s: help target %q is not an article in this manual", r.file, r.slug)
			continue
		}
		if r.anchor == "" {
			continue // whole-article link
		}
		if sort.SearchStrings(anchors, r.anchor) >= len(anchors) ||
			anchors[sort.SearchStrings(anchors, r.anchor)] != r.anchor {
			t.Errorf("%s: help target %s#%s — that article has no {#%s} anchor (has %v)",
				r.file, r.slug, r.anchor, r.anchor, anchors)
		}
	}
	t.Logf("checked %d contextual help targets", len(refs))
}
