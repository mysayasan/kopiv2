package retrieval

import (
	"strings"
	"unicode"
)

// Tokenizing four languages with one function.
//
// The manual ships in en, ms, zh and ar, and the same question box serves all four. That rules
// out the usual "split on whitespace" tokenizer twice over:
//
//   - Chinese has no word delimiters. A whitespace split turns a whole sentence into one useless
//     token, so a zh question would retrieve nothing at all. The standard fix without dragging in
//     a segmentation dictionary is CHARACTER BIGRAMS: 摄像机 becomes 摄像 + 像机. Bigrams overlap,
//     so a query bigram matches wherever the same two characters are adjacent in an article,
//     which is close enough to word matching for retrieval and needs no lexicon.
//   - Arabic writes the same word many ways. Optional diacritics, the decorative tatweel, and
//     four spellings of alef mean a reader's query and the article can be the "same" word and
//     share not one byte. Folding them to one form before indexing is what makes ar work at all.
//
// Markdown is deliberately NOT stripped first: every character this tokenizer does not recognise
// is a separator anyway, so `**bold**`, `| table |`, `[text](adding-cameras)` and `{#anchor}` all
// fall apart into exactly the tokens wanted — including the link target, which is a slug worth
// matching on.

// minLatinToken drops single letters. They carry no signal and appear everywhere; CJK bigrams are
// exempt because a lone character IS a word there.
const minLatinToken = 2

// Tokenize turns arbitrary text into the index's terms: lowercased latin/digit words, folded
// Arabic words, and overlapping CJK character bigrams.
func Tokenize(s string) []string {
	s = strings.ToLower(s)
	out := make([]string, 0, len(s)/4)

	var latin []rune
	var han []rune

	flushLatin := func() {
		if len(latin) == 0 {
			return
		}
		tok := string(latin)
		latin = latin[:0]
		if len([]rune(tok)) < minLatinToken || isStopword(tok) {
			return
		}
		out = append(out, foldSuffix(tok)...)
	}
	// flushHan emits overlapping bigrams; a run of one character emits itself, so a single-glyph
	// term is still findable.
	flushHan := func() {
		switch {
		case len(han) == 0:
			return
		case len(han) == 1:
			out = append(out, string(han))
		default:
			for i := 0; i+1 < len(han); i++ {
				out = append(out, string(han[i:i+2]))
			}
		}
		han = han[:0]
	}

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		// Combining marks are folded away rather than treated as separators. Treating them as
		// separators is the subtle version of this bug: a diacritic in the MIDDLE of an Arabic
		// word would split it into two fragments that match nothing.
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		// A contracted negation ("won't", "can't", "doesn't") is dropped WHOLE, auxiliary and
		// all. Splitting on the apostrophe instead leaves the stem behind as a content word, and
		// that stem is occasionally a real and unrelated one: "the app won't start" left `won`
		// in the index, which then matched "who won the football".
		if (r == '\'' || r == '’') && i+1 < len(runes) && runes[i+1] == 't' {
			latin = latin[:0]
			i++
			continue
		}
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushHan()
			folded, keep := foldRune(r)
			if !keep {
				continue
			}
			latin = append(latin, folded)
		default:
			flushLatin()
			flushHan()
		}
	}
	flushLatin()
	flushHan()
	return out
}

// foldSuffix normalizes the English word forms a question and a heading disagree about, returning
// every variant the token should be indexed and searched under.
//
// This is the fix for a defect a live bench found and the unit tests did not. "adding cameras"
// retrieved the Adding cameras article perfectly; "how do I add a camera" — how a person actually
// asks — retrieved troubleshooting fragments and a settings tab, because `add` is not `adding` and
// `camera` is not `cameras`. Headings and titles carry 2–3× weight, so losing them to a plural
// costs the whole signal the ranking is built on.
//
// The rules are deliberately crude. Correct linguistic stemming is not the goal — matching is —
// and since the SAME function runs over the index and the query, over-stemming only ever conflates
// unrelated words (a mild precision cost at this corpus size), it never splits a pair.
//
// Arabic gets its own clitic stripping; everything else non-ASCII is left alone.
func foldSuffix(tok string) []string {
	if isArabicWord(tok) {
		return []string{foldArabicClitics(tok)}
	}
	if !isASCIIWord(tok) || len(tok) < 4 {
		return []string{tok}
	}

	// The rules CASCADE: a plural is stripped first, then the result is checked for a verb
	// ending. Applying only one rule was a real defect — "settings" stopped at `setting` while
	// "setting" went on to `sett`, so the two never met, and `changed` reached `chang` while
	// `change` stayed `change`. Every form has to end up at the same place as the base word.
	out := []string{}
	base := tok
	switch {
	case strings.HasSuffix(base, "ies") && len(base) > 4:
		base = base[:len(base)-3] + "y" // utilities → utility
	case strings.HasSuffix(base, "sses"), strings.HasSuffix(base, "shes"),
		strings.HasSuffix(base, "ches"), strings.HasSuffix(base, "xes"):
		base = base[:len(base)-2] // watches → watch, boxes → box
	case strings.HasSuffix(base, "s") && len(base) > 3 &&
		!strings.HasSuffix(base, "ss") && !strings.HasSuffix(base, "us") && !strings.HasSuffix(base, "is"):
		base = base[:len(base)-1] // cameras → camera; access/status/this untouched
	}
	if base != tok {
		out = append(out, base)
	}

	var stem string
	switch {
	case strings.HasSuffix(base, "ing") && len(base) > 5:
		stem = base[:len(base)-3] // adding → add, running → runn, setting → sett
	case strings.HasSuffix(base, "ed") && len(base) > 4:
		stem = base[:len(base)-2] // adopted → adopt, stopped → stopp, changed → chang
	default:
		if len(out) == 0 {
			out = append(out, base)
		}
		return out
	}
	out = append(out, stem)

	// The stem is ambiguous in one of two ways, and emitting BOTH readings settles it without a
	// lexicon — the query is folded identically, so the pair always meets somewhere.
	//
	//   - a doubled consonant: "runn" wants to be "run", but "add" (from "adding") is already
	//     right and must not become "ad".
	//   - a dropped silent e: "chang" (from "changed") must still meet "change".
	if undoubled, ok := undouble(stem); ok {
		return append(out, undoubled)
	}
	return append(out, stem+"e")
}

// undouble strips a trailing doubled consonant, reporting whether it applied.
func undouble(stem string) (string, bool) {
	n := len(stem)
	if n < 3 || stem[n-1] != stem[n-2] || isVowel(stem[n-1]) {
		return stem, false
	}
	return stem[:n-1], true
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}

// foldArabicClitics strips the attached particles Arabic writes as part of the word.
//
// Arabic glues the definite article and several one-letter prepositions and conjunctions onto the
// front of a noun, so الكاميرا ("the camera"), وكاميرا ("and a camera") and كاميرا are the same
// word to a reader and three different words to an index. The common feminine/plural endings do
// the same at the back. This is "light stemming" — the standard shallow treatment — and stops
// well short of root extraction, which Arabic really needs and which no rule table provides.
//
// The minimum length checks matter: over-stripping a short word turns two unrelated words into
// the same two-letter stem.
func foldArabicClitics(tok string) string {
	r := []rune(tok)
	for _, prefix := range []string{"وال", "بال", "كال", "فال", "لل", "ال"} {
		p := []rune(prefix)
		if len(r) >= len(p)+3 && string(r[:len(p)]) == prefix {
			r = r[len(p):]
			break
		}
	}
	// "ا" last, so the plural الكاميرات and the singular كاميرا both land on كامير — without it
	// the plural strips to a stem the singular never reaches, which is worse than not stripping.
	for _, suffix := range []string{"ات", "ون", "ين", "ها", "يه", "ية", "ا"} {
		s := []rune(suffix)
		if len(r) >= len(s)+3 && string(r[len(r)-len(s):]) == suffix {
			r = r[:len(r)-len(s)]
			break
		}
	}
	return string(r)
}

// isArabicWord reports whether the token is written in Arabic script.
func isArabicWord(tok string) bool {
	for _, r := range tok {
		if !unicode.Is(unicode.Arabic, r) {
			return false
		}
	}
	return tok != ""
}

// isASCIIWord reports whether every byte is a plain ASCII letter or digit.
func isASCIIWord(tok string) bool {
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// foldRune normalizes the Arabic spellings that a reader and a translator will disagree about.
// keep is false for characters that carry no meaning at all (tatweel is pure decoration).
func foldRune(r rune) (rune, bool) {
	switch r {
	case 0x0640: // tatweel — a stretched joining line, never meaningful
		return 0, false
	case 0x0622, 0x0623, 0x0625, 0x0671: // آ أ إ ٱ → ا
		return 0x0627, true
	case 0x0629: // ة → ه
		return 0x0647, true
	case 0x0649: // ى → ي
		return 0x064A, true
	}
	return r, true
}

// stopwords are the words that appear in nearly every article and in nearly every question.
//
// IDF alone is NOT enough here, and a live bench proved it. Interrogatives are the reason: a
// manual is full of question-shaped headings ("How much disk do I need?", "What it cannot do"),
// headings carry 3× weight, and every user question starts "how do I…". So "how" and "do" were
// scoring as content and pulling every how-to question toward the FAQ — `how do I add a camera`
// ranked a disk-sizing answer above the article titled "Adding cameras". Their IDF was low; the
// heading multiplier made low enough to matter.
//
// Interrogatives, auxiliaries and pronouns are therefore dropped outright. They carry no topic
// in a question and no topic in a heading, which is exactly the profile of a word that should not
// be indexed at all. Content words stay, however common — "camera" is uninformative in a camera
// manual, but that is IDF's job to price, not this list's.
//
// English and Malay (the two whitespace languages) plus the Arabic particles and question words.
// Chinese is left alone because bigrams make a stopword list guesswork.
var stopwords = map[string]bool{
	// en — articles, conjunctions, prepositions
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"from": true, "into": true, "onto": true, "out": true, "off": true, "about": true,
	// en — interrogatives and auxiliaries (the live-bench fix)
	"how": true, "what": true, "why": true, "when": true, "which": true, "who": true,
	"where": true, "whose": true, "does": true, "did": true, "are": true, "was": true, "is": true,
	"were": true, "been": true, "being": true, "can": true, "could": true, "should": true,
	"would": true, "will": true, "shall": true, "may": true, "might": true, "must": true,
	"have": true, "has": true, "had": true, "there": true, "then": true,
	// en — pronouns
	"you": true, "your": true, "yours": true, "our": true, "ours": true, "its": true,
	"they": true, "them": true, "their": true, "him": true, "her": true, "his": true,
	// ms
	"yang": true, "dan": true, "untuk": true, "dengan": true, "adalah": true,
	"ini": true, "itu": true, "pada": true, "saya": true, "anda": true,
	"bagaimana": true, "apa": true, "mengapa": true, "bila": true, "siapa": true,
	"mana": true, "boleh": true, "akan": true, "atau": true, "juga": true, "tidak": true,
	// ar
	"في": true, "من": true, "على": true, "الى": true, "هذا": true, "هذه": true, "التي": true,
	"كيف": true, "ماذا": true, "لماذا": true, "متى": true, "اين": true, "هل": true, "عن": true,
}

func isStopword(tok string) bool { return stopwords[tok] }
