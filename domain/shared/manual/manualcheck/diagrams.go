package manualcheck

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/domain/shared/manual"
	"github.com/mysayasan/kopiv2/domain/shared/manual/diagram"
)

// diagrams guards the drawn figures, which rot in ways prose does not.
//
// A wrong sentence reads as a wrong sentence. A wrong diagram reads as authority — it is the part
// of a page a reader trusts without checking, and the part nobody proofreads in a language they
// do not speak. Three failure modes are silent in production and all three are caught here:
//
//   - A fence the renderer cannot draw. The frontend is deliberately lenient because this check
//     is strict; if a malformed fence gets past the build it reaches an appliance as a blank
//     space where a figure should be.
//   - A translation that lost an edge. Four copies of every figure exist, and only one of them
//     is read by the person who drew it.
//   - A node linking to an article that has been renamed, which fails exactly the way a dead
//     contextual "?" button fails: silently, on click, months later.
func diagrams(t *testing.T, lib *manual.Library, langs []string) {
	t.Helper()
	problems, checked := DiagramProblems(lib, langs)
	for _, p := range problems {
		t.Error(p)
	}
	t.Logf("checked %d figures across %d languages", checked, len(langs))
}

// DiagramProblems is the guard as a plain function: it returns what is wrong instead of failing a
// test, and reports how many figures it looked at.
//
// Splitting it out this way is what makes the guard itself testable. A check welded to *testing.T
// can only be exercised by deliberately failing a test, which in practice means it is never
// exercised at all — and a drift guard nobody has ever watched catch anything is indistinguishable
// from one that quietly stopped working. The count comes back for the same reason: "checked 0
// figures" is the failure this file most needs to be able to notice about itself.
func DiagramProblems(lib *manual.Library, langs []string) (problems []string, checked int) {
	known := map[string][]string{}
	for _, a := range lib.Bundle(manual.DefaultLanguage) {
		known[a.Slug] = anchorsIn(a)
	}

	// English is the structure of record, the same way it is for anchors and article sets.
	base := map[string][]diagram.Diagram{}
	for _, a := range lib.Bundle(manual.DefaultLanguage) {
		var found []diagram.Diagram
		found, problems = parseArticle(problems, manual.DefaultLanguage, a, known)
		base[a.Slug] = found
	}

	for _, lang := range langs {
		for _, a := range lib.Bundle(lang) {
			if a.Language != lang {
				continue // untranslated: LanguageParity's business, not this check's
			}
			var got []diagram.Diagram
			got, problems = parseArticle(problems, lang, a, known)
			if lang == manual.DefaultLanguage {
				checked += len(got)
				continue
			}
			problems = compare(problems, lang, a.Slug, base[a.Slug], got)
		}
	}
	return problems, checked
}

// parseArticle parses every fence in one article and resolves its links, reporting each problem
// against the article and fence it came from.
func parseArticle(problems []string, lang string, a manual.Article, known map[string][]string) ([]diagram.Diagram, []string) {
	var out []diagram.Diagram
	for _, raw := range diagram.Fences(a.Body) {
		d, err := diagram.Parse(raw.Kind, raw.Body)
		if err != nil {
			problems = append(problems, fmt.Sprintf(
				"%s/%s: the %s block at line %d does not parse: %v", lang, a.Slug, fence(raw.Kind), raw.Line, err))
			continue
		}
		problems = checkLinks(problems, lang, a.Slug, raw, d, known)
		out = append(out, d)
	}
	return out, problems
}

// checkLinks resolves a node's `=> slug#anchor` with exactly the rules UIReferences applies to a
// contextual help button, because it is the same promise to the reader: click this, land there.
func checkLinks(problems []string, lang, slug string, raw diagram.Raw, d diagram.Diagram, known map[string][]string) []string {
	for _, link := range d.Links() {
		target, anchor, _ := strings.Cut(link, "#")
		if target == "" {
			problems = append(problems, fmt.Sprintf(
				"%s/%s: the %s block at line %d links to %q, which names no article",
				lang, slug, fence(raw.Kind), raw.Line, link))
			continue
		}
		anchors, ok := known[target]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"%s/%s: the %s block at line %d links to %q, but no such article exists",
				lang, slug, fence(raw.Kind), raw.Line, target))
			continue
		}
		if anchor == "" {
			continue
		}
		if !hasAnchor(anchors, anchor) {
			problems = append(problems, fmt.Sprintf(
				"%s/%s: the %s block at line %d links to %s#%s — that article has no {#%s} anchor (has %v)",
				lang, slug, fence(raw.Kind), raw.Line, target, anchor, anchor, anchors))
		}
	}
	return problems
}

// compare asserts a translation drew the same picture, differing only in its words.
func compare(problems []string, lang, slug string, want, got []diagram.Diagram) []string {
	if len(want) != len(got) {
		return append(problems, fmt.Sprintf(
			"%s/%s: %d figures, but the English article has %d — a diagram was added or dropped in translation",
			lang, slug, len(got), len(want)))
	}
	for i := range want {
		if want[i].Fingerprint() == got[i].Fingerprint() {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s/%s: figure %d (%s) has a different structure from the English original.\n%s",
			lang, slug, i+1, fence(want[i].Kind), structureDiff(want[i], got[i])))
	}
	return problems
}

// fence names a block the way an author wrote it, so a failure can be searched for verbatim.
func fence(k diagram.Kind) string { return "```" + string(k) }

// anchorsIn is the sorted set of a body's explicit {#id} heading anchors. It deliberately does no
// duplicate reporting — that belongs to the Anchors check; here the set is only needed to resolve
// a link.
func anchorsIn(a manual.Article) []string {
	var out []string
	for _, m := range headingAnchor.FindAllStringSubmatch(a.Body, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// structureDiff says which ids and edges differ rather than printing two fingerprints and
// leaving the reader to diff them by eye. A translator reading this failure is the person least
// equipped to work out what "the fingerprints differ" means.
func structureDiff(want, got diagram.Diagram) string {
	var b strings.Builder
	if want.Kind != got.Kind {
		fmt.Fprintf(&b, "  kind: English has ```%s, this has ```%s\n", want.Kind, got.Kind)
	}
	writeSetDiff(&b, "node", nodeKeys(want), nodeKeys(got))
	writeSetDiff(&b, "edge", edgeKeys(want), edgeKeys(got))
	if b.Len() == 0 {
		return "  (the same parts, in a different arrangement)"
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeSetDiff(b *strings.Builder, what string, want, got []string) {
	inGot := make(map[string]bool, len(got))
	for _, k := range got {
		inGot[k] = true
	}
	inWant := make(map[string]bool, len(want))
	for _, k := range want {
		inWant[k] = true
	}
	for _, k := range want {
		if !inGot[k] {
			fmt.Fprintf(b, "  missing %s: %s\n", what, k)
		}
	}
	for _, k := range got {
		if !inWant[k] {
			fmt.Fprintf(b, "  unexpected %s: %s\n", what, k)
		}
	}
}

func nodeKeys(d diagram.Diagram) []string {
	out := make([]string, 0, len(d.Nodes))
	for _, n := range d.Nodes {
		key := fmt.Sprintf("%s %s", n.Shape, n.ID)
		if n.Link != "" {
			key += " => " + n.Link
		}
		if n.Value != "" {
			key += " `" + n.Value + "`"
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func edgeKeys(d diagram.Diagram) []string {
	out := make([]string, 0, len(d.Edges))
	for _, e := range d.Edges {
		arrow := "->"
		if e.Dashed {
			arrow = "-->"
		}
		key := fmt.Sprintf("%s %s %s", e.From, arrow, e.To)
		if e.Label != "" {
			key += " : (labelled)"
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func hasAnchor(anchors []string, want string) bool {
	i := sort.SearchStrings(anchors, want)
	return i < len(anchors) && anchors[i] == want
}

// SpecValues asserts that the literals a manual states are the literals the software uses.
//
// This is the check that separates a reference section from fiction. A ```spec row says
// `49532/tcp`; the app's own test passes the constant the code actually binds, and the two must
// agree. Without it, adding a technical reference to the manual makes the product WORSE than
// having none — a confidently wrong port costs an integrator an afternoon, where a missing one
// costs a support call.
//
// An app calls it with the values it can name from its own source:
//
//	manualcheck.SpecValues(t, manualLib, map[string]string{
//	    "fleet-ports/discovery": fmt.Sprintf("%d/udp", pairing.DefaultPort),
//	    "fleet-ports/control":   fmt.Sprintf("%d/tcp", fleetnode.DefaultMTLSPort),
//	})
//
// The key is "<article slug>/<spec row id>". Every key must resolve to a row that exists, so a
// renamed row fails here rather than quietly stopping being checked — a check that silently
// covers nothing is worse than no check, because it reads as coverage.
func SpecValues(t *testing.T, lib *manual.Library, want map[string]string) {
	t.Helper()
	if len(want) == 0 {
		return
	}

	// Only English is asserted; the other languages are held to it by the fingerprint check,
	// which already treats a spec value as structure.
	have := map[string]string{}
	for _, a := range lib.Bundle(manual.DefaultLanguage) {
		for _, raw := range diagram.Fences(a.Body) {
			if raw.Kind != diagram.KindSpec {
				continue
			}
			d, err := diagram.Parse(raw.Kind, raw.Body)
			if err != nil {
				continue // reported by the Diagrams check
			}
			for id, value := range d.Values() {
				have[a.Slug+"/"+id] = value
			}
		}
	}

	for _, key := range sortedStringKeys(want) {
		got, ok := have[key]
		if !ok {
			t.Errorf("no spec row %q in the manual — the row was renamed or removed, and this value stopped being checked", key)
			continue
		}
		if got != want[key] {
			t.Errorf("spec row %q says %q, but the software uses %q", key, got, want[key])
		}
	}
}

func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
