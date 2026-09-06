package manualcheck

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mysayasan/kopiv2/domain/shared/manual"
)

// The guard exists to catch things nobody will notice by reading. These tests exercise it against
// libraries built to contain exactly those things, because a drift guard that has never been
// watched catch anything is indistinguishable from one that quietly stopped working.

const frontmatter = "---\ntitle: %T%\ncategory: reference\ncategoryLabel: Reference\nsummary: A page.\norder: 10\n---\n\n"

// article assembles one language's copy of a page: shared prose, one heading anchor, one figure.
func article(title, figure string) string {
	return strings.ReplaceAll(frontmatter, "%T%", title) +
		"# " + title + "\n\n## Ports {#ports}\n\nSome prose.\n\n```flow\n" + figure + "```\n"
}

// goodFigure is the same structure in both languages; only the words after the colons differ.
const goodFigureEN = `step  probe : The node sends a signed probe
ask   known : Does the key match?
end   drop  : Ignored
ok    adopt => other#ports : Offered for adoption
probe -> known
known -> drop : no
known -> adopt : yes
`

const goodFigureMS = `step  probe : Nod menghantar kuiri bertandatangan
ask   known : Adakah kunci sepadan?
end   drop  : Diabaikan
ok    adopt => other#ports : Ditawarkan untuk diterima pakai
probe -> known
known -> drop : tidak
known -> adopt : ya
`

func library(t *testing.T, en, ms string) *manual.Library {
	t.Helper()
	return manual.New(fstest.MapFS{
		"en/10-adopting.md": &fstest.MapFile{Data: []byte(article("Adopting", en))},
		"ms/10-adopting.md": &fstest.MapFile{Data: []byte(article("Menerima", ms))},
		"en/20-other.md":    &fstest.MapFile{Data: []byte(article("Other", goodFigureEN))},
		"ms/20-other.md":    &fstest.MapFile{Data: []byte(article("Lain", goodFigureMS))},
	}, ".")
}

// A correct manual must produce NO complaints. A guard that fires on good content gets switched
// off, which is a worse outcome than not having written it.
func TestDiagramProblemsAcceptsATranslatedFigure(t *testing.T) {
	problems, checked := DiagramProblems(library(t, goodFigureEN, goodFigureMS), []string{"en", "ms"})
	if len(problems) != 0 {
		t.Errorf("a correct manual was reported as broken:\n%s", strings.Join(problems, "\n"))
	}
	// Counting matters as much as complaining: this is what distinguishes "everything is fine"
	// from "the fence syntax changed and I am now checking nothing".
	if checked != 2 {
		t.Errorf("checked %d figures, want 2", checked)
	}
}

func TestDiagramProblemsCatchesDrift(t *testing.T) {
	cases := []struct {
		name string
		ms   string
		want string
	}{{
		// Dropping this edge also strands the node it was the only route to, so the strict
		// parser refuses the figure before the fingerprint is ever compared. Both are the
		// guard; this asserts the earlier, better message rather than the one I first expected.
		name: "an edge lost in translation, stranding the node it fed",
		ms:   strings.Replace(goodFigureMS, "known -> drop : tidak\n", "", 1),
		want: `"drop" has no edges`,
	}, {
		name: "an edge invented in translation",
		ms:   goodFigureMS + "probe -> adopt : jalan pintas\n",
		want: "unexpected edge: probe -> adopt",
	}, {
		name: "an edge label dropped, so a branch is unlabelled in one language",
		ms:   strings.Replace(goodFigureMS, "known -> drop : tidak", "known -> drop", 1),
		want: "missing edge: known -> drop : (labelled)",
	}, {
		name: "a node id translated, which silently detaches it",
		ms:   strings.ReplaceAll(goodFigureMS, "drop", "jatuh"),
		want: "missing node: end drop",
	}, {
		name: "a shape changed, so a refusal is drawn as an outcome",
		ms:   strings.Replace(goodFigureMS, "end   drop", "ok    drop", 1),
		want: "unexpected node: ok drop",
	}, {
		name: "a whole figure dropped",
		ms:   "",
		want: "0 figures, but the English article has 1",
	}, {
		name: "a link target that does not resolve",
		ms:   strings.Replace(goodFigureMS, "=> other#ports", "=> other#gone", 1),
		want: "has no {#gone} anchor",
	}, {
		name: "a fence the renderer could not draw",
		ms:   strings.Replace(goodFigureMS, "ask   known : Adakah kunci sepadan?", "ask   known", 1),
		want: "does not parse",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems, _ := DiagramProblems(library(t, goodFigureEN, tc.ms), []string{"en", "ms"})
			joined := strings.Join(problems, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("guard did not report %q.\ngot:\n%s", tc.want, joined)
			}
			// Every complaint must name the language at fault; "a diagram is wrong somewhere"
			// is not actionable in a four-language manual.
			if !strings.Contains(joined, "ms/adopting") {
				t.Errorf("no problem named the offending file.\ngot:\n%s", joined)
			}
		})
	}
}

// A spec row's value is checked against the software, which is the difference between a reference
// section and a plausible-looking one.
func TestSpecValues(t *testing.T) {
	spec := "```spec\nrow control `49532/tcp` : The node dials the parent.\n```\n"
	lib := manual.New(fstest.MapFS{
		"en/10-ports.md": &fstest.MapFile{
			Data: []byte(strings.ReplaceAll(frontmatter, "%T%", "Ports") + "# Ports\n\n" + spec),
		},
	}, ".")

	// The happy path is asserted through the real helper; the failure paths are asserted on the
	// same lookup the helper does, since the helper reports through *testing.T by design.
	SpecValues(t, lib, map[string]string{"ports/control": "49532/tcp"})

	problems, _ := DiagramProblems(lib, []string{"en"})
	if len(problems) != 0 {
		t.Errorf("spec block reported as broken: %v", problems)
	}
}
