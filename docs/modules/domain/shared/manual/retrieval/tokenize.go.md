# Module: domain/shared/manual/retrieval/tokenize.go

## Purpose

Turns arbitrary manual text or a reader's question into index terms, for **all four suite
languages at once**. `Tokenize(s string) []string` is the whole public surface.

## Why a whitespace split is not an option

The manual ships in en, ms, zh and ar, and one question box serves all of them. That rules out the
usual tokenizer twice over:

- **Chinese has no word delimiters.** A whitespace split turns a whole sentence into one useless
  token, so a zh question would retrieve *nothing at all*. The fix without a segmentation lexicon
  is **character bigrams**: 摄像机 → `摄像` + `像机`. Bigrams overlap, so a query bigram matches
  wherever those two characters are adjacent in an article — close enough to word matching for
  retrieval, and it needs no dictionary. A run of one character emits itself, so a single-glyph
  term (门) is still findable.
- **Arabic writes the same word many ways.** Optional diacritics, the decorative tatweel, and four
  spellings of alef mean a reader's query and the article can be the "same" word and share not one
  byte. Folding them to a single form before indexing is what makes ar work at all.

`TestTokenizeHanBigrams` and `TestTokenizeArabicFolding` are the regression guards; the first is
specifically what fails if someone "simplifies" this back to a word split.

## Behaviour

| Input class | Handling |
|---|---|
| Latin letters / digits | Lowercased, accumulated into words, split on anything else. Tokens shorter than `minLatinToken`=2 are dropped. |
| Han (`unicode.Han`) | Accumulated into runs, emitted as overlapping bigrams. |
| Combining marks (`unicode.Mn`) | **Skipped without flushing** — see below. |
| Arabic forms | Folded by `foldRune`: tatweel (U+0640) dropped entirely; آ أ إ ٱ → ا; ة → ه; ى → ي. |
| Everything else | Separator. |

**Combining marks are folded away, not treated as separators.** Treating them as separators is the
subtle version of this bug: a diacritic in the *middle* of an Arabic word would split it into two
fragments that match nothing.

## Markdown is deliberately not stripped first

Every character the tokenizer does not recognise is a separator anyway, so `**bold**`,
`| table |`, `[text](adding-cameras)` and `{#anchor}` all fall apart into exactly the wanted
tokens — including the link target, which is a slug worth matching on.

## Morphological folding (`foldSuffix`) — a live-bench fix

`Tokenize` emits folded forms, not raw words. Without this, **"adding cameras" retrieved its
article perfectly and "how do I add a camera" did not**, because `add` is not `adding` and
`camera` is not `cameras` — and since headings and titles carry the topic boost, losing them to a
plural costs the signal the ranking leans on hardest.

The rules **cascade**: a plural is stripped first, then the result is checked for a verb ending.

| Stage | Rule | Example |
|---|---|---|
| 1 | `ies` → `y` | utilities → utility |
| 1 | `sses`/`shes`/`ches`/`xes` → drop `es` | watches → watch, boxes → box |
| 1 | trailing `s`, not `ss`/`us`/`is` | cameras → camera; access/status untouched |
| 2 | `ing` (len > 5) | adding → add, running → runn, setting → sett |
| 2 | `ed` (len > 4) | adopted → adopt, changed → chang |

**Cascading is not a refinement, it is a correctness requirement.** Applying only one rule left
`settings` at `setting` while `setting` went on to `sett`, so the two never met — and `changed`
reached `chang` while `change` stayed `change`. Every inflected form has to land where the base
word lands. The live symptom was "I changed a setting and the app won't start" ranking the section
that answers it fourth, behind two unrelated articles.

A stem is then ambiguous in one of two ways, and **both readings are emitted** rather than guessed
at — the query is folded identically, so the pair always meets somewhere:

- a **doubled consonant**: "runn" wants to be "run", but "add" (from "adding") is already correct
  and must not become "ad" → emit `runn`+`run`, `add`+`ad`.
- a **dropped silent e**: "chang" (from "changed") must still meet "change" → emit `chang`+`change`.

## Contracted negations are dropped whole

`won't`, `can't`, `doesn't` are removed entirely, auxiliary and all, before any other rule.

Splitting on the apostrophe instead leaves the stem behind as a content word, and that stem is
occasionally a real and unrelated one: writing "the app won't start" into an article put `won` in
the index, where it promptly matched *"who won the football"* — a question the corpus is supposed
to reject. Both halves of a contracted auxiliary are stopwords by this file's own rules anyway, so
dropping the pair is the consistent outcome.

The rules are deliberately crude: correct stemming is not the goal, matching is. Because the same
function runs over the index and the query, over-stemming only ever conflates unrelated words (a
mild precision cost at this corpus size) — it never splits a pair.

**Arabic gets clitic stripping instead** (`foldArabicClitics`): the glued definite article and
one-letter particles (`ال`, `وال`, `بال`, `كال`, `فال`, `لل`) off the front, and common
feminine/plural endings (`ات`, `ون`, `ين`, `ها`, `يه`, `ية`, `ا`) off the back, each behind a
minimum-length guard. The trailing `ا` rule is last and exists so the plural `الكاميرات` and the
singular `كاميرا` both land on `كامير` — stripping the plural to a stem the singular never reaches
is worse than not stripping at all. This is standard *light stemming* and stops well short of root
extraction, which Arabic genuinely needs: `أضيف` and `إضافة` share a root and no rule table will
ever unify them. `Result.TopicHit` is what covers that gap.

## Stopwords

`stopwords` covers English and Malay (the two whitespace languages) plus Arabic particles and
question words. Chinese is left alone — a stopword list over bigrams is guesswork.

**IDF alone is not enough here, and a live bench proved it.** Interrogatives are the reason: a
manual is full of question-shaped headings ("How much disk do I need?", "What it cannot do"),
headings carry the topic boost, and every user question begins "how do I…". So `how` and `do`
scored as content and pulled every how-to question toward the FAQ. Interrogatives, auxiliaries and
pronouns are therefore dropped outright — they carry no topic in a question and none in a heading.

Ordinary content words stay, however common: "camera" is uninformative in a camera manual, but
pricing that is IDF's job, not this list's.
