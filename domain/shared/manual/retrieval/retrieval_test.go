package retrieval

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mysayasan/kopiv2/domain/shared/manual"
)

func TestTokenizeLatin(t *testing.T) {
	got := Tokenize("Adding **cameras** to a node — see [ONVIF](onvif-management#discovery).")
	// "adding" folds to "add" and also emits "ad": a stem ending in a doubled consonant is
	// ambiguous ("runn" wants to be "run"), so both forms are indexed rather than guessed at.
	// Markdown syntax is not stripped first — it is all separators, so the link target survives
	// as the two tokens worth matching on.
	want := []string{"add", "ad", "camera", "to", "node", "see", "onvif", "onvif", "management", "discovery"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tokens = %v, want %v", got, want)
	}
}

// The regression test for the defect a live bench found: a question asked in natural English
// ("how do I add a camera") must reach the same terms as the heading that answers it ("Adding
// cameras"). Before suffix folding it did not, and the weighted title/heading signal — the thing
// the ranking leans on hardest — was lost to a plural.
func TestTokenizeFoldsEnglishWordForms(t *testing.T) {
	shareATerm := func(a, b string) bool {
		seen := map[string]bool{}
		for _, tok := range Tokenize(a) {
			seen[tok] = true
		}
		for _, tok := range Tokenize(b) {
			if seen[tok] {
				return true
			}
		}
		return false
	}
	for _, pair := range [][2]string{
		{"add a camera", "Adding cameras"},
		{"adopt a node", "Adopting nodes"},
		{"record", "recording"},
		{"detected", "detect"},
		{"run", "running"},
		{"stop", "stopped"},
		{"zone", "zones"},
		{"utility", "utilities"},
		{"box", "boxes"},
		// The rules must CASCADE. Applying only one left "settings" at `setting` while
		// "setting" went on to `sett`, so the two never met — the defect that made
		// "I changed a setting and the app won't start" miss the section that answers it.
		{"setting", "settings"},
		{"a changed value", "change it"},
		{"recordings", "recording"},
	} {
		if !shareATerm(pair[0], pair[1]) {
			t.Errorf("%q and %q share no term: %v vs %v",
				pair[0], pair[1], Tokenize(pair[0]), Tokenize(pair[1]))
		}
	}
}

// A contracted negation is dropped whole. Splitting on the apostrophe left the auxiliary stem
// behind as a content word, and "won" (from "won't") then matched "who won the football".
func TestTokenizeDropsContractedNegations(t *testing.T) {
	for _, phrase := range []string{"the app won't start", "it doesn't start", "I can't see it"} {
		for _, tok := range Tokenize(phrase) {
			if tok == "won" || tok == "doesn" || tok == "can" {
				t.Errorf("%q leaked the auxiliary stem %q", phrase, tok)
			}
		}
	}
	if got := Tokenize("the app won't start"); strings.Join(got, ",") != "app,start" {
		t.Errorf(`Tokenize("the app won't start") = %v, want [app start]`, got)
	}
	// A real "won" must survive.
	if got := Tokenize("who won the football"); !strings.Contains(strings.Join(got, ","), "won") {
		t.Errorf("a genuine 'won' was dropped: %v", got)
	}
}

// Folding must not eat words that merely end in the same letters.
func TestTokenizeFoldingLeavesLookalikesAlone(t *testing.T) {
	for word, want := range map[string]string{
		"access": "access", // -ss is not a plural
		"status": "status", // -us is not a plural
		"onvif":  "onvif",
	} {
		got := Tokenize(word)
		if len(got) == 0 || got[0] != want {
			t.Errorf("Tokenize(%q) = %v, want first token %q", word, got, want)
		}
	}
}

func TestTokenizeDropsStopwordsAndSingleLetters(t *testing.T) {
	for _, tok := range Tokenize("the a I x and with") {
		if tok == "the" || tok == "and" || tok == "with" || len([]rune(tok)) < 2 {
			t.Errorf("token %q should have been dropped", tok)
		}
	}
}

// Chinese has no spaces, so bigrams are the only thing standing between a zh question and zero
// results. This is the test that fails if someone "simplifies" the tokenizer to a word split.
func TestTokenizeHanBigrams(t *testing.T) {
	got := Tokenize("摄像机")
	want := []string{"摄像", "像机"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("han tokens = %v, want %v", got, want)
	}
	if single := Tokenize("门"); len(single) != 1 || single[0] != "门" {
		t.Errorf("single han char = %v, want [门]", single)
	}
}

// The same Arabic word written with diacritics, a tatweel and a different alef must reach the
// index as one term, or a reader's question can share nothing with the article that answers it.
func TestTokenizeArabicFolding(t *testing.T) {
	plain := Tokenize("الكاميرا")
	decorated := Tokenize("الكـامِيرا")
	if strings.Join(plain, ",") != strings.Join(decorated, ",") {
		t.Errorf("folding failed: %v vs %v", plain, decorated)
	}
	if a, b := Tokenize("أجهزة"), Tokenize("اجهزه"); strings.Join(a, ",") != strings.Join(b, ",") {
		t.Errorf("alef/teh-marbuta folding failed: %v vs %v", a, b)
	}
}

const sampleArticle = `# Adding cameras

A camera is added from the Cameras screen. This opening passage is the article intro.

## Discovery {#discovery}

ONVIF discovery finds cameras on the local subnet and lists them for adoption.

## Credentials {#credentials}

Each camera needs a username and password before any stream can be opened.
`

func TestChunkArticleSplitsOnSections(t *testing.T) {
	chunks := ChunkArticle("mymatasan", "MyMataSan", manual.Article{
		Slug: "adding-cameras", Title: "Adding cameras", Language: "en", Body: sampleArticle,
	})
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3 (intro + 2 sections)", len(chunks))
	}
	if chunks[0].Anchor != "" || !strings.Contains(chunks[0].Text, "opening passage") {
		t.Errorf("first chunk should be the anchorless intro, got %+v", chunks[0])
	}
	if chunks[1].Anchor != "discovery" || chunks[1].Heading != "Discovery" {
		t.Errorf("second chunk = %q/%q, want Discovery/discovery", chunks[1].Heading, chunks[1].Anchor)
	}
	if strings.Contains(chunks[1].Text, "{#") {
		t.Error("the anchor marker leaked into the chunk body")
	}
	if got, want := chunks[2].ID(), "mymatasan:adding-cameras#credentials"; got != want {
		t.Errorf("ID = %q, want %q", got, want)
	}
	if got, want := chunks[2].Path(), "MyMataSan › Adding cameras › Credentials"; got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestChunkArticleSplitsOversizedSections(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Big\n\n## Long section {#long}\n\n")
	for i := 0; i < 6; i++ {
		b.WriteString("### Sub heading\n\n")
		b.WriteString(strings.Repeat("filler prose about detection rules and zones. ", 12))
		b.WriteString("\n\n")
	}
	chunks := ChunkArticle("app", "App", manual.Article{Slug: "big", Title: "Big", Language: "en", Body: b.String()})
	if len(chunks) < 2 {
		t.Fatalf("oversized section was not split: %d chunk(s)", len(chunks))
	}
	for i, c := range chunks {
		if c.Anchor != "long" {
			t.Errorf("chunk %d lost the parent anchor: %q", i, c.Anchor)
		}
		if n := len([]rune(c.Text)); n > maxChunkRunes {
			t.Errorf("chunk %d is %d runes, over the %d cap", i, n, maxChunkRunes)
		}
	}
	if chunks[1].Part == 0 {
		t.Error("split pieces after the first must carry a Part number for a unique id")
	}
}

func TestSnippetMarksTheCut(t *testing.T) {
	c := Chunk{Text: strings.Repeat("word ", 400)}
	got := c.Snippet(100)
	if len([]rune(got)) > 110 {
		t.Errorf("snippet is %d runes, want ~100", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("snippet %q does not mark the truncation", got)
	}
	if full := (Chunk{Text: "short"}).Snippet(100); full != "short" {
		t.Errorf("short text was altered: %q", full)
	}
}

// testCorpus is a two-app, three-language manual small enough to reason about.
func testCorpus(t *testing.T) *Corpus {
	t.Helper()
	fsysA := fstest.MapFS{
		"en/10-adding-cameras.md": &fstest.MapFile{Data: []byte(
			"---\ntitle: Adding cameras\ncategory: cameras\norder: 10\n---\n" + sampleArticle)},
		"zh/10-adding-cameras.md": &fstest.MapFile{Data: []byte(
			"---\ntitle: 添加摄像机\ncategory: cameras\norder: 10\n---\n# 添加摄像机\n\n## 发现 {#discovery}\n\nONVIF 发现会在本地子网中找到摄像机。\n")},
		"ar/10-adding-cameras.md": &fstest.MapFile{Data: []byte(
			"---\ntitle: إضافة الكاميرات\ncategory: cameras\norder: 10\n---\n# إضافة الكاميرات\n\n## الاكتشاف {#discovery}\n\nيعثر الاكتشاف على الكاميرات في الشبكة المحلية.\n")},
	}
	fsysB := fstest.MapFS{
		"en/10-adopting-nodes.md": &fstest.MapFile{Data: []byte(
			"---\ntitle: Adopting nodes\ncategory: fleet\norder: 10\n---\n# Adopting nodes\n\n## Claim codes {#claim}\n\nA node is adopted by entering its claim code in the fleet screen.\n")},
	}
	return New(
		Source{App: "myseliasan", Title: "MySeliaSan", Library: manual.New(fsysB, ".")},
		Source{App: "mymatasan", Title: "MyMataSan", Library: manual.New(fsysA, ".")},
	)
}

// The fixture below is four chunks, so questions here are phrased in words the fixture actually
// contains. That is not the test being tuned to pass: unknown query terms are charged at the
// corpus's maximum IDF, and on a four-chunk corpus three unknown words outweigh everything, so a
// naturally-phrased question tests the fixture's size rather than the retriever. Relevance
// thresholds are calibrated against the real 328-chunk manual in apps/myseliasan/services; what
// these tests own is the plumbing — chunking, tokenizing, ranking, fallback.
func TestSearchFindsTheRightSection(t *testing.T) {
	c := testCorpus(t)
	res := c.Search("en", "ONVIF discovery cameras local subnet", 3)
	if len(res) == 0 {
		t.Fatal("no results")
	}
	if got, want := res[0].Chunk.ID(), "mymatasan:adding-cameras#discovery"; got != want {
		t.Errorf("top hit = %q, want %q", got, want)
	}
	if res[0].Chunk.AppTitle != "MyMataSan" {
		t.Errorf("app title = %q — a citation must name the app it belongs to", res[0].Chunk.AppTitle)
	}
}

func TestSearchSpansApps(t *testing.T) {
	c := testCorpus(t)
	res := c.Search("en", "claim code adopted node", 3)
	if len(res) == 0 || res[0].Chunk.App != "myseliasan" {
		t.Fatalf("expected the myseliasan article, got %+v", res)
	}
}

func TestSearchChineseAndArabic(t *testing.T) {
	c := testCorpus(t)
	if res := c.Search("zh", "发现摄像机", 3); len(res) == 0 || res[0].Chunk.Lang != "zh" {
		t.Fatalf("zh search failed: %+v", res)
	}
	// Written with a diacritic and a tatweel the article does not use, so this only finds the
	// article if the folding in Tokenize did its job.
	if res := c.Search("ar", "الاكتشاف الكـامِيرات", 3); len(res) == 0 || res[0].Chunk.Lang != "ar" {
		t.Fatalf("ar search failed: %+v", res)
	}
}

// A question sharing no vocabulary with the manual retrieves nothing at all. The interesting
// case — a fleet-status question that shares an incidental noun — needs a corpus big enough for
// "rare" to mean something and is calibrated in apps/myseliasan/services/agent_docs_test.go.
func TestSearchRejectsUnrelatedQuestions(t *testing.T) {
	c := testCorpus(t)
	if res := c.Search("en", "tomorrow's weather forecast", 3); len(res) != 0 {
		t.Errorf("expected no hits, got %d (top %q)", len(res), res[0].Chunk.ID())
	}
}

// A language with no translation of an article still answers, from English, and says so.
func TestSearchFallsBackToEnglish(t *testing.T) {
	c := testCorpus(t)
	res := c.Search("zh", "claim code", 3)
	if len(res) == 0 {
		t.Fatal("no results — the English fallback did not run")
	}
	if res[0].Chunk.App != "myseliasan" || res[0].Chunk.Lang != "en" {
		t.Errorf("top hit = %s/%s, want the English myseliasan article", res[0].Chunk.App, res[0].Chunk.Lang)
	}
}
