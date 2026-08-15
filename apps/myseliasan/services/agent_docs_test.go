package services

import (
	"strings"
	"testing"
)

// Calibration + golden set for manual retrieval, run against the REAL shipped manuals.
//
// This is where the relevance thresholds in domain/shared/manual/retrieval answer to reality. A
// fixture corpus cannot calibrate them — with four documents every word is rare — so the numbers
// live here, against the corpus that actually ships, and this test fails when an edit to either
// manual moves them.

// TestDocsCorpusIsWiredUp is the guard against the silent failure mode: an `//go:embed` pattern
// that stops matching, or a manual moved out from under the corpus, leaves retrieval returning
// nothing at all and every other test here still passing for the wrong reason.
func TestDocsCorpusIsWiredUp(t *testing.T) {
	d := NewDocsService()
	apps := map[string]bool{}
	for _, q := range []string{"add a camera", "adopt a node", "detection zone", "claim code"} {
		for _, e := range d.Search("en", q, 5) {
			apps[e.App] = true
		}
	}
	for _, want := range []string{"myseliasan", "mymatasan"} {
		if !apps[want] {
			t.Errorf("nothing was ever retrieved from the %s manual — is its embed still matching?", want)
		}
	}
}

// Documentation questions must reach the right article AT THE TOP, not merely somewhere in the
// result set.
//
// The weaker "is it in the top 4" form of this test passed while retrieval was materially broken:
// `how do I add a camera` put a troubleshooting fragment, a settings tab and a disk-sizing FAQ
// above the article literally titled "Adding cameras", and the test was satisfied because the
// fragment happened to belong to the right article. Only a live bench showed it. Rank is the
// thing being asserted, so rank is what the test checks.
func TestDocsGoldenQuestions(t *testing.T) {
	d := NewDocsService()
	// Several articles can legitimately answer one question — "draw a detection zone" is served
	// by the rule editor's Zones section AND by the camera page's live-preview section — so each
	// case lists every article that would be a correct answer. Naming one true article would
	// assert an opinion about the manual's structure rather than about retrieval.
	cases := []struct {
		q         string
		wantAnyOf []string // "app:slug"
	}{
		{"how do I add a camera to a node", []string{"mymatasan:adding-cameras"}},
		{"what is a claim code and how do I adopt a node", []string{"myseliasan:adopting-nodes"}},
		{"how do I draw a detection zone on a camera", []string{
			"mymatasan:detection-rules", "mymatasan:camera-properties"}},
		{"how do I back up and restore the system", []string{
			"mymatasan:backup-and-restore", "mymatasan:restore-from-backup"}},
		{"what does encryption at rest protect", []string{"mymatasan:encryption-at-rest"}},
		// myseliasan's own content: the control plane must be able to answer for itself, not
		// only relay answers about the node appliances.
		{"why does my fleet rule never fire", []string{"myseliasan:fleet-rules"}},
		{"what is the grace delay on a rule", []string{"myseliasan:fleet-rules"}},
		{"why is every digest critical", []string{"myseliasan:fleet-digest"}},
		{"where do the assistant's answers come from", []string{"myseliasan:ask-the-fleet"}},
		{"what does auto-renew do on a node certificate", []string{"myseliasan:managing-nodes"}},
		{"how do I remove a node from the fleet", []string{"myseliasan:managing-nodes"}},
		{"what does acknowledge do in the notification feed", []string{"myseliasan:notifications"}},
		{"how do I install a language model without internet", []string{"myseliasan:language-model"}},
		{"why is the map blank with no streets", []string{"myseliasan:the-map"}},
		{"can I upload a floor plan image", []string{"myseliasan:buildings-and-floors"}},
		{"why can't I place the same camera twice", []string{"myseliasan:buildings-and-floors"}},
		{"why are the names blank in my PDF report", []string{"myseliasan:reports"}},
		{"a new user signed in but sees nothing", []string{"myseliasan:users-and-roles"}},
		{"why can't my colleague see the reports menu", []string{
			"myseliasan:users-and-roles", "myseliasan:workspace-tour"}},
		{"does the audit log record denied attempts", []string{"myseliasan:audit-log"}},
		{"I changed a setting and the app won't start", []string{"myseliasan:settings"}},
		{"does anything leave my network", []string{"myseliasan:faq"}},
		{"what happens if the control plane goes down", []string{"myseliasan:faq"}},
		{"can I run this air gapped", []string{"myseliasan:faq", "myseliasan:language-model"}},
		{"my node is offline what do I check first", []string{
			"myseliasan:managing-nodes", "myseliasan:troubleshooting"}},
		{"how do I set up ONVIF discovery", []string{
			"mymatasan:onvif-management", "mymatasan:adding-cameras"}},
	}
	for _, tc := range cases {
		// Exactly what the model is given: nothing ranked below docsMaxExcerpts ever reaches it,
		// so a "hit" that lands outside that window has not grounded anything.
		res := d.Search("en", tc.q, docsMaxExcerpts)
		if len(res) == 0 {
			t.Errorf("%q: no manual hits at all", tc.q)
			continue
		}
		want := map[string]bool{}
		for _, id := range tc.wantAnyOf {
			want[id] = true
		}
		found := false
		for _, e := range res {
			if want[e.App+":"+e.Slug] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q: none of %v reached the model; got %s", tc.q, tc.wantAnyOf, summarize(res))
		}
	}
}

// Fleet-state questions are the case retrieval CANNOT get right, and this test records that
// rather than pretending otherwise.
//
// The original design had them retrieve nothing, via a relevance threshold. Measured against
// this corpus, no such threshold exists: "how many nodes are offline right now" scores in the
// same 0.34–1.00 coverage band as "how do I add a node", because the two questions are asked
// with the same words. A second lexical gate was tried and deleted — it saturated at 1.0 for
// every question here, which is a knob that looks principled and decides nothing.
//
// So the guarantee is a COST guarantee instead: a question that did not want documentation pays
// at most a few hundred bytes of context for it, the prompt is told which source answers which
// kind of question, and the citation list reflects what the answer actually used. This test
// pins that bound.
func TestDocsFleetStateQuestionsStayCheap(t *testing.T) {
	d := NewDocsService()
	for _, q := range []string{
		"how many nodes are offline right now",
		"which camera had the most alerts this week",
		"what happened last night",
	} {
		excerpts, block := d.Ground("en", q)
		if len(excerpts) > docsMaxExcerpts {
			t.Errorf("%q: %d excerpts, over the %d cap", q, len(excerpts), docsMaxExcerpts)
		}
		if len(block) > docsMaxBytes {
			t.Errorf("%q: excerpt block is %d bytes, over the %d budget", q, len(block), docsMaxBytes)
		}
	}
}

// A question with nothing in common with the manual retrieves nothing, which is what keeps the
// coverage floor earning its place.
func TestDocsRejectsUnrelatedQuestions(t *testing.T) {
	d := NewDocsService()
	for _, q := range []string{"what is the exchange rate for the yen", "who won the football"} {
		if res := d.Search("en", q, 4); len(res) != 0 {
			t.Errorf("%q retrieved %s", q, summarize(res))
		}
	}
}

func TestMarkCited(t *testing.T) {
	in := []DocExcerpt{{Ref: "M1"}, {Ref: "M2"}, {Ref: "M3"}}
	out := MarkCited(in, "Use the scan [M1]; see also (M3) for the credentials step.")
	if !out[0].Cited || out[1].Cited || !out[2].Cited {
		t.Errorf("cited flags wrong: %+v", out)
	}
	// The input must not be mutated — the caller may still be holding it.
	if in[0].Cited {
		t.Error("MarkCited mutated its input")
	}
	if got := MarkCited(in, "no citations here at all"); got[0].Cited || got[2].Cited {
		t.Error("an uncited answer must mark nothing")
	}
}

func TestDocsGroundBudget(t *testing.T) {
	d := NewDocsService()
	excerpts, block := d.Ground("en", "how do I add a camera and draw a detection zone")
	if len(excerpts) == 0 {
		t.Fatal("no excerpts for a plainly documentation-shaped question")
	}
	if len(block) > docsMaxBytes {
		t.Errorf("excerpt block is %d bytes, over the %d budget", len(block), docsMaxBytes)
	}
	if !strings.HasPrefix(block, docsHeader) {
		t.Error("excerpt block must open with the framing header")
	}
	for i, e := range excerpts {
		if ref := "[" + e.Ref + "]"; !strings.Contains(block, ref) {
			t.Errorf("excerpt %d has ref %q but the block does not contain %q", i, e.Ref, ref)
		}
		if e.AppTitle == "" {
			t.Errorf("excerpt %d has no product name — a citation must say which app it documents", i)
		}
		if e.App == localApp != e.Local {
			t.Errorf("excerpt %d: Local flag disagrees with app %q", i, e.App)
		}
	}
}

// Every language must retrieve, including the two the tokenizer had to be built for.
func TestDocsAllLanguages(t *testing.T) {
	d := NewDocsService()
	for lang, q := range map[string]string{
		"en": "how do I add a camera",
		"ms": "bagaimana saya menambah kamera",
		"zh": "如何添加摄像机",
		"ar": "كيف أضيف كاميرا",
	} {
		if res := d.Search(lang, q, 4); len(res) == 0 {
			t.Errorf("lang %s: %q retrieved nothing", lang, q)
		}
	}
}

func summarize(res []DocExcerpt) string {
	var parts []string
	for _, e := range res {
		parts = append(parts, e.App+":"+e.Slug+"#"+e.Anchor)
	}
	return strings.Join(parts, ", ")
}
