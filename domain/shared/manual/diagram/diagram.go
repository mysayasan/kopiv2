// Package diagram is the grammar for the manual's drawn figures.
//
// A manual article draws a picture by writing a fenced block — ```flow, ```arch, ```seq or
// ```spec — whose lines declare nodes and edges. The frontend turns that into inline SVG using
// the app's own --ui-* tokens; this package is what the BUILD uses to know the fence is sound.
//
// # Why a grammar and not an image
//
// A figure shipped as a .png or a hand-drawn .svg cannot be translated, cannot follow the app's
// theme, cannot mirror for Arabic, and cannot be diffed. Every one of those matters here: the
// manual ships in four languages including an RTL one, both themes, and prints to paper. Writing
// the picture as text in the article solves all four at once, and gets the labels translated by
// the same person translating the prose around them, in the same file.
//
// # One rule holds the whole thing together
//
//	Everything before the label is STRUCTURE and must be byte-identical in every language.
//	Everything after it is PROSE and must be translated.
//
// Structure is node ids, shapes, edges, link targets and spec values — none of which is language.
// That split is what makes a translated diagram checkable at all: manualcheck fingerprints the
// structure of each language's copy of a figure and fails the build when they diverge, so an
// Arabic diagram cannot quietly lose an edge (see manualcheck/diagrams.go).
//
// # This parser is deliberately strict
//
// It rejects anything it does not fully understand rather than skipping it. The frontend renderer
// is the lenient one, because by the time a fence reaches a reader this parser has already passed
// it in CI. Inverting that — a tolerant build and a strict renderer — is how you ship a figure
// that renders as a blank box on a customer's appliance.
package diagram

import (
	"fmt"
	"sort"
	"strings"
)

// Kind is the fence language: the word after the opening ``` .
type Kind string

const (
	// KindFlow is a decision path: what the system does, and where it branches.
	KindFlow Kind = "flow"
	// KindArch is a component drawing: what talks to what, over which port.
	KindArch Kind = "arch"
	// KindSeq is an ordered exchange between participants, drawn as lifelines.
	KindSeq Kind = "seq"
	// KindSpec is a reference table of literal values — ports, paths, limits, defaults.
	KindSpec Kind = "spec"
)

// Kinds lists every fence language, in the order they are documented.
var Kinds = []Kind{KindFlow, KindArch, KindSeq, KindSpec}

// IsKind reports whether a fence info string names a diagram.
func IsKind(s string) bool {
	for _, k := range Kinds {
		if string(k) == s {
			return true
		}
	}
	return false
}

// shapes lists the node verbs each kind accepts. A shape is a role, not a decoration: the
// renderer draws `ask` as a diamond and `end` in the deny palette because those carry meaning,
// so an author picking a shape is describing the step rather than styling it.
var shapes = map[Kind][]string{
	// step: an action. ask: a decision. ok: the good outcome. end: a refusal or stop.
	// alert: an outcome that also raises something — the duress unlock, the suppressed alarm.
	KindFlow: {"step", "ask", "ok", "end", "alert"},
	// box: a component of this appliance. store: data at rest. ext: something outside the
	// box — another appliance, a camera, an operator's browser.
	KindArch: {"box", "store", "ext"},
	// actor: one participant's lifeline.
	KindSeq: {"actor"},
	// row: one reference value.
	KindSpec: {"row"},
}

// Node is one drawn thing.
type Node struct {
	// ID is the language-independent handle edges refer to. Never translated.
	ID string `json:"id"`
	// Shape is the verb that declared it; see shapes above.
	Shape string `json:"shape"`
	// Link makes the node clickable: "slug" or "slug#anchor" into another article. This is what
	// turns a figure from an illustration into navigation.
	Link string `json:"link,omitempty"`
	// Value is the literal a spec row states — a port, a path, a default. Not translated, and
	// asserted against real source constants by manualcheck.SpecValues.
	Value string `json:"value,omitempty"`
	// Label is the prose. The only part a translator touches.
	Label string `json:"label"`
}

// Edge is one arrow.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Dashed marks a reply, an optional path, or an asynchronous hop, declared with `-->`.
	Dashed bool `json:"dashed,omitempty"`
	// Label is the prose on the arrow — "yes", "no", "polls every 30s". Translated.
	Label string `json:"label,omitempty"`
}

// Diagram is one parsed fence.
type Diagram struct {
	Kind  Kind   `json:"kind"`
	Title string `json:"title,omitempty"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges,omitempty"`
}

// Raw is a diagram fence located in an article body, before parsing.
type Raw struct {
	Kind Kind
	// Line is the 1-based line number of the opening fence, so an error can name a place.
	Line int
	Body string
}

// Fences finds every diagram fence in an article body.
//
// It walks fences rather than regexp-ing for them because a ``` block may legitimately CONTAIN a
// line that looks like a fence opener — a manual about markdown eventually does — and only a
// scan that tracks open/closed state gets that right.
func Fences(body string) []Raw {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var out []Raw
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "```") {
			continue
		}
		info := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		start := i + 1
		var buf []string
		i++
		for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			buf = append(buf, lines[i])
			i++
		}
		if IsKind(info) {
			out = append(out, Raw{Kind: Kind(info), Line: start, Body: strings.Join(buf, "\n")})
		}
	}
	return out
}

// Parse reads one fence body. Every error names the offending line so a failing build points at
// a place in a file rather than at a fence.
func Parse(kind Kind, body string) (Diagram, error) {
	if !IsKind(string(kind)) {
		return Diagram{}, fmt.Errorf("unknown diagram kind %q (want one of %v)", kind, Kinds)
	}
	d := Diagram{Kind: kind}
	seen := map[string]bool{}

	for n, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue // blank, or an author's comment
		}
		at := n + 1

		switch {
		case strings.HasPrefix(line, "title "), line == "title":
			title, err := parseTitle(line)
			if err != nil {
				return Diagram{}, fmt.Errorf("line %d: %w", at, err)
			}
			if d.Title != "" {
				return Diagram{}, fmt.Errorf("line %d: a second title; a diagram has one", at)
			}
			d.Title = title

		case isEdgeLine(line):
			e, err := parseEdge(line)
			if err != nil {
				return Diagram{}, fmt.Errorf("line %d: %w", at, err)
			}
			if kind == KindSpec {
				return Diagram{}, fmt.Errorf("line %d: a spec block is a table and has no arrows", at)
			}
			d.Edges = append(d.Edges, e)

		default:
			node, err := parseNode(kind, line)
			if err != nil {
				return Diagram{}, fmt.Errorf("line %d: %w", at, err)
			}
			if seen[node.ID] {
				return Diagram{}, fmt.Errorf("line %d: duplicate id %q", at, node.ID)
			}
			seen[node.ID] = true
			d.Nodes = append(d.Nodes, node)
		}
	}

	if err := d.validate(); err != nil {
		return Diagram{}, err
	}
	return d, nil
}

// isEdgeLine reports whether a line declares an arrow. An edge is recognised by an arrow token
// standing alone between two ids, which is why a label containing "->" is not mistaken for one:
// the label starts after the ':' and is never scanned.
func isEdgeLine(line string) bool {
	head, _, _ := strings.Cut(line, ":")
	for _, f := range strings.Fields(head) {
		if f == "->" || f == "-->" {
			return true
		}
	}
	return false
}

func parseTitle(line string) (string, error) {
	_, label, ok := strings.Cut(line, ":")
	if !ok {
		return "", fmt.Errorf("a title needs a colon: `title : What this shows`")
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return "", fmt.Errorf("empty title")
	}
	return label, nil
}

// parseEdge reads `from -> to` or `from --> to : label`.
func parseEdge(line string) (Edge, error) {
	head, label, hasLabel := strings.Cut(line, ":")
	fields := strings.Fields(head)
	if len(fields) != 3 {
		return Edge{}, fmt.Errorf("an edge is `from -> to` or `from -> to : label`, got %q", strings.TrimSpace(head))
	}
	e := Edge{From: fields[0], To: fields[2], Dashed: fields[1] == "-->"}
	if hasLabel {
		e.Label = strings.TrimSpace(label)
		if e.Label == "" {
			return Edge{}, fmt.Errorf("edge %s -> %s has a colon but no label; drop the colon", e.From, e.To)
		}
	}
	if err := checkID(e.From); err != nil {
		return Edge{}, err
	}
	if err := checkID(e.To); err != nil {
		return Edge{}, err
	}
	if e.From == e.To {
		return Edge{}, fmt.Errorf("%s points at itself", e.From)
	}
	return e, nil
}

// parseNode reads `<shape> <id> [=> link] [`value`] : <label>`.
//
// The structure is read positionally, token by token, and the label is everything after the
// first standalone ':'. That is why a label may contain colons, arrows and backticks freely — by
// the time the label starts, parsing has stopped looking at syntax.
func parseNode(kind Kind, line string) (Node, error) {
	head, label, ok := strings.Cut(line, ":")
	if !ok {
		return Node{}, fmt.Errorf("no label; a node is `%s some-id : Its label`", shapes[kind][0])
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return Node{}, fmt.Errorf("empty label")
	}

	fields := strings.Fields(head)
	if len(fields) < 2 {
		return Node{}, fmt.Errorf("a node needs a shape and an id, got %q", strings.TrimSpace(head))
	}
	n := Node{Shape: fields[0], ID: fields[1], Label: label}
	if !allowedShape(kind, n.Shape) {
		return Node{}, fmt.Errorf("%q is not a %s shape (want one of %v)", n.Shape, kind, shapes[kind])
	}
	if err := checkID(n.ID); err != nil {
		return Node{}, err
	}

	rest := fields[2:]
	for i := 0; i < len(rest); i++ {
		switch {
		case rest[i] == "=>":
			if i+1 >= len(rest) {
				return Node{}, fmt.Errorf("%s: `=>` with no article to link to", n.ID)
			}
			if n.Link != "" {
				return Node{}, fmt.Errorf("%s: a second link target", n.ID)
			}
			n.Link = rest[i+1]
			i++
		case strings.HasPrefix(rest[i], "`"):
			value, next, err := readQuoted(rest, i)
			if err != nil {
				return Node{}, fmt.Errorf("%s: %w", n.ID, err)
			}
			if n.Value != "" {
				return Node{}, fmt.Errorf("%s: a second value", n.ID)
			}
			n.Value, i = value, next
		default:
			return Node{}, fmt.Errorf("%s: unexpected %q before the label", n.ID, rest[i])
		}
	}

	if kind == KindSpec && n.Value == "" {
		return Node{}, fmt.Errorf("%s: a spec row states a literal value in backticks, e.g. `49532/tcp`", n.ID)
	}
	if kind != KindSpec && n.Value != "" {
		return Node{}, fmt.Errorf("%s: only a spec row carries a value", n.ID)
	}
	return n, nil
}

// readQuoted consumes a `backticked value` that may span several whitespace-separated tokens,
// returning the value and the index of its last token.
func readQuoted(fields []string, start int) (string, int, error) {
	joined := strings.TrimPrefix(fields[start], "`")
	if strings.HasSuffix(fields[start], "`") && len(fields[start]) > 1 {
		return strings.TrimSuffix(joined, "`"), start, nil
	}
	join := func(tail string) string {
		if joined == "" {
			return tail
		}
		return joined + " " + tail
	}
	for i := start + 1; i < len(fields); i++ {
		if strings.HasSuffix(fields[i], "`") {
			return join(strings.TrimSuffix(fields[i], "`")), i, nil
		}
		joined = join(fields[i])
	}
	return "", 0, fmt.Errorf("unclosed ` around a value")
}

func allowedShape(kind Kind, shape string) bool {
	for _, s := range shapes[kind] {
		if s == shape {
			return true
		}
	}
	return false
}

// checkID keeps ids to the character set a fingerprint, an SVG id and a URL fragment all handle
// without escaping — and, more to the point, to characters that exist on every keyboard a
// translator might be using. A translated file must reproduce these EXACTLY.
func checkID(id string) error {
	if id == "" {
		return fmt.Errorf("empty id")
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return fmt.Errorf("id %q may only use letters, digits, - and _", id)
		}
	}
	return nil
}

// validate catches the mistakes that survive line-by-line parsing.
func (d Diagram) validate() error {
	if len(d.Nodes) == 0 {
		return fmt.Errorf("no nodes")
	}
	known := make(map[string]bool, len(d.Nodes))
	for _, n := range d.Nodes {
		known[n.ID] = true
	}
	for _, e := range d.Edges {
		if !known[e.From] {
			return fmt.Errorf("edge from %q, which is not declared", e.From)
		}
		if !known[e.To] {
			return fmt.Errorf("edge to %q, which is not declared", e.To)
		}
	}
	if d.Kind == KindSpec {
		return nil
	}
	// A node nothing reaches and nothing leaves is almost always a typo'd id in an edge rather
	// than a deliberately isolated box, and it draws as a stranded rectangle.
	if len(d.Nodes) > 1 {
		touched := map[string]bool{}
		for _, e := range d.Edges {
			touched[e.From] = true
			touched[e.To] = true
		}
		for _, n := range d.Nodes {
			if !touched[n.ID] {
				return fmt.Errorf("%q has no edges — nothing reaches it and nothing leaves it", n.ID)
			}
		}
	}
	return nil
}

// Links lists every article target the diagram's nodes point at, so manualcheck can resolve them
// with the same rules it applies to a contextual "?" button.
func (d Diagram) Links() []string {
	var out []string
	for _, n := range d.Nodes {
		if n.Link != "" {
			out = append(out, n.Link)
		}
	}
	return out
}

// Values maps spec row ids to the literal each states, which is what an app's own test asserts
// against the real constants in its source. This is the check that keeps a reference table from
// becoming fiction one refactor after it was written.
func (d Diagram) Values() map[string]string {
	out := map[string]string{}
	for _, n := range d.Nodes {
		if n.Value != "" {
			out[n.ID] = n.Value
		}
	}
	return out
}

// Fingerprint is the diagram's STRUCTURE, with every translated word removed: kind, node ids,
// shapes, links, spec values, and the edges between them.
//
// Two language copies of the same figure must produce the same fingerprint. What it deliberately
// cannot see is which prose sits on which arrow — an edge contributes whether it is labelled, not
// what the label says — so a translator who swaps "yes" and "no" is not caught here. That is the
// same limit the anchor check has lived with, and the same answer applies: structure is machine
// checkable, meaning is what review is for.
func (d Diagram) Fingerprint() string {
	var b strings.Builder
	b.WriteString(string(d.Kind))

	nodes := append([]Node(nil), d.Nodes...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	for _, n := range nodes {
		fmt.Fprintf(&b, "\nnode %s %s %s %s", n.ID, n.Shape, n.Link, n.Value)
	}

	edges := append([]Edge(nil), d.Edges...)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	for _, e := range edges {
		fmt.Fprintf(&b, "\nedge %s %s %t %t", e.From, e.To, e.Dashed, e.Label != "")
	}
	return b.String()
}
