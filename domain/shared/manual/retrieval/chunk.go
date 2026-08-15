package retrieval

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/mysayasan/kopiv2/domain/shared/manual"
)

// A chunk is one SECTION of an article, not an arbitrary window of characters.
//
// Retrieval systems usually have to invent chunk boundaries, and inventing them badly is how an
// answer ends up quoting half a sentence. This manual does not need them invented: it is already
// written as `## Heading {#anchor}` sections, each a self-contained answer to one question, and
// those anchors are ALREADY the deep-link targets the contextual "?" buttons use. Chunking on
// them means a retrieved passage is exactly the passage a reader can be sent to — the citation
// and the evidence are the same object.

// maxChunkRunes bounds one chunk. Runes, not bytes: a byte budget would silently make Chinese
// chunks a third the size of English ones, since zh is three bytes per character in UTF-8.
const maxChunkRunes = 1200

// Chunk is one retrievable passage.
type Chunk struct {
	// App is the stable owning-app id ("mymatasan"); AppTitle is what a reader sees.
	//
	// This is not decoration. Both manuals ship articles called `welcome`, `first-sign-in`,
	// `setup-wizard`, `workspace-tour` and `using-this-manual`, so a slug alone identifies the
	// wrong page half the time.
	App      string `json:"app"`
	AppTitle string `json:"appTitle,omitempty"`
	Lang     string `json:"lang"`
	Slug     string `json:"slug"`
	// Anchor is the section's `{#id}`, empty for an article's opening passage.
	Anchor       string `json:"anchor,omitempty"`
	ArticleTitle string `json:"articleTitle"`
	Heading      string `json:"heading,omitempty"`
	Category     string `json:"category,omitempty"`
	// Part numbers the pieces of a section too long to keep whole (0 when it was not split).
	Part int    `json:"part,omitempty"`
	Text string `json:"text"`
}

// ID is the chunk's stable identity, app-qualified.
func (c Chunk) ID() string {
	id := c.App + ":" + c.Slug
	if c.Anchor != "" {
		id += "#" + c.Anchor
	}
	if c.Part > 0 {
		id += "~" + strconv.Itoa(c.Part)
	}
	return id
}

// Path is the human-readable trail shown to a reader and given to the model:
// "MyMataSan manual › Adding cameras › Discovery".
func (c Chunk) Path() string {
	parts := make([]string, 0, 3)
	if c.AppTitle != "" {
		parts = append(parts, c.AppTitle)
	}
	if c.ArticleTitle != "" {
		parts = append(parts, c.ArticleTitle)
	}
	if c.Heading != "" {
		parts = append(parts, c.Heading)
	}
	return strings.Join(parts, " › ")
}

// Snippet trims the chunk text to at most maxRunes, cutting at a paragraph boundary when one is
// close enough and marking the cut. A model told "…" knows it is looking at part of a section;
// silently truncated prose reads as a complete thought that happens to end mid-argument.
func (c Chunk) Snippet(maxRunes int) string {
	runes := []rune(c.Text)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return c.Text
	}
	cut := string(runes[:maxRunes])
	// Prefer the last paragraph break in the final quarter, then the last sentence end.
	if idx := strings.LastIndex(cut, "\n\n"); idx > maxRunes*3/4 {
		return strings.TrimSpace(cut[:idx]) + "\n…"
	}
	if idx := strings.LastIndexAny(cut, ".。!?！？"); idx > maxRunes/2 {
		return strings.TrimSpace(cut[:idx+1]) + " …"
	}
	return strings.TrimSpace(cut) + " …"
}

var (
	// headingLine matches an ATX heading and captures its level and text.
	headingLine = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	// anchorSuffix matches the explicit `{#id}` the manual pins its deep links to.
	anchorSuffix = regexp.MustCompile(`\s*\{#([A-Za-z0-9._-]+)\}\s*$`)
)

// ChunkArticle splits one article into retrievable sections.
func ChunkArticle(app, appTitle string, a manual.Article) []Chunk {
	base := Chunk{
		App: app, AppTitle: appTitle, Lang: a.Language,
		Slug: a.Slug, ArticleTitle: a.Title, Category: a.CategoryLabel,
	}

	type section struct {
		heading string
		anchor  string
		lines   []string
	}
	// The opening section holds everything before the first H2 — the H1 and the paragraphs that
	// explain what the article is for, which is often the best answer to a broad question.
	sections := []section{{}}
	inFence := false

	for _, line := range strings.Split(a.Body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence {
			if m := headingLine.FindStringSubmatch(line); m != nil {
				level := len(m[1])
				text, anchor := splitAnchor(m[2])
				if level == 1 {
					// The H1 repeats Title, which is already indexed as a weighted field.
					continue
				}
				if level == 2 {
					sections = append(sections, section{heading: text, anchor: anchor})
					continue
				}
				// H3 and deeper stay inside their parent section unless the section has to be
				// split for size, at which point they become the split points.
			}
		}
		cur := &sections[len(sections)-1]
		cur.lines = append(cur.lines, line)
	}

	var out []Chunk
	for _, s := range sections {
		text := strings.TrimSpace(strings.Join(s.lines, "\n"))
		if text == "" {
			continue
		}
		c := base
		c.Heading, c.Anchor = s.heading, s.anchor
		for i, piece := range splitOversized(text) {
			part := c
			part.Text = piece
			if i > 0 {
				part.Part = i
			}
			out = append(out, part)
		}
	}
	return out
}

// splitAnchor peels the explicit `{#id}` off a heading, returning the visible text and the id.
func splitAnchor(heading string) (string, string) {
	heading = strings.TrimSpace(heading)
	m := anchorSuffix.FindStringSubmatch(heading)
	if m == nil {
		return heading, ""
	}
	return strings.TrimSpace(anchorSuffix.ReplaceAllString(heading, "")), m[1]
}

// splitOversized breaks a section that exceeds maxChunkRunes, preferring H3 boundaries and
// falling back to packing whole paragraphs. It never splits inside a fenced block or a table row,
// because half a table is worse than no table.
func splitOversized(text string) []string {
	if len([]rune(text)) <= maxChunkRunes {
		return []string{text}
	}

	blocks := subHeadingBlocks(text)
	if len(blocks) == 1 {
		blocks = paragraphs(text)
	}

	var out []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, b := range blocks {
		// A single block over the cap is emitted whole rather than hacked apart: the alternative
		// is a fragment starting mid-table. Snippet trims it at read time if the budget demands.
		if cur.Len() > 0 && len([]rune(cur.String()))+len([]rune(b)) > maxChunkRunes {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(b)
	}
	flush()
	return out
}

// subHeadingBlocks splits at H3+ headings, keeping each heading with the text it introduces.
func subHeadingBlocks(text string) []string {
	var blocks []string
	var cur []string
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence && len(cur) > 0 {
			if m := headingLine.FindStringSubmatch(line); m != nil && len(m[1]) >= 3 {
				blocks = append(blocks, strings.TrimSpace(strings.Join(cur, "\n")))
				cur = nil
			}
		}
		cur = append(cur, line)
	}
	if s := strings.TrimSpace(strings.Join(cur, "\n")); s != "" {
		blocks = append(blocks, s)
	}
	return blocks
}

// paragraphs splits on blank lines, treating a fenced block as one paragraph.
func paragraphs(text string) []string {
	var blocks []string
	var cur []string
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence && strings.TrimSpace(line) == "" {
			if s := strings.TrimSpace(strings.Join(cur, "\n")); s != "" {
				blocks = append(blocks, s)
			}
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	if s := strings.TrimSpace(strings.Join(cur, "\n")); s != "" {
		blocks = append(blocks, s)
	}
	return blocks
}
