package diagram

import (
	"strings"
	"testing"
)

// The badge decision is the figure this grammar was designed against, so it is the one the tests
// are written against too: it uses every flow shape, both branch kinds, a node link, and the
// convergence that makes the duress path visible.
const badgeFlow = `
title : What happens when a badge is presented

step  intake : Badge presented at reader
ask   known  : Credential known?
end   unknown : Deny · unknown card
ask   sched  : Schedule allows now?
end   closed : Deny · outside schedule
ask   duress : Duress PIN entered?
alert silent => door-rules#duress : Unlock, and raise a silent alarm
ok    grant  : Unlock · granted
step  audit  : Written to the audit trail

intake -> known
known  -> unknown : no
known  -> sched : yes
sched  -> closed : no
sched  -> duress : yes
duress -> silent : yes
duress -> grant : no
grant  -> audit
silent --> audit : also recorded
`

func TestParseFlow(t *testing.T) {
	d, err := Parse(KindFlow, badgeFlow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Title != "What happens when a badge is presented" {
		t.Errorf("title = %q", d.Title)
	}
	if len(d.Nodes) != 9 {
		t.Fatalf("nodes = %d, want 9", len(d.Nodes))
	}
	if len(d.Edges) != 9 {
		t.Fatalf("edges = %d, want 9", len(d.Edges))
	}

	// The link is structure, not prose: it must survive parsing intact so manualcheck can
	// resolve it the way it resolves a contextual "?" button.
	var silent Node
	for _, n := range d.Nodes {
		if n.ID == "silent" {
			silent = n
		}
	}
	if silent.Shape != "alert" || silent.Link != "door-rules#duress" {
		t.Errorf("silent = %+v", silent)
	}
	if silent.Label != "Unlock, and raise a silent alarm" {
		t.Errorf("silent label = %q", silent.Label)
	}
	if got := d.Links(); len(got) != 1 || got[0] != "door-rules#duress" {
		t.Errorf("links = %v", got)
	}

	last := d.Edges[len(d.Edges)-1]
	if !last.Dashed || last.From != "silent" || last.To != "audit" {
		t.Errorf("last edge = %+v, want a dashed silent -> audit", last)
	}
}

// A translation may only change what comes after the colon. This is the property the whole
// checkable-diagram idea rests on, so it is asserted directly rather than only through
// manualcheck.
func TestFingerprintIgnoresProseAndOrder(t *testing.T) {
	translated := strings.NewReplacer(
		"What happens when a badge is presented", "Apa yang berlaku apabila kad dibaca",
		"Badge presented at reader", "Kad dibaca di pembaca",
		"Credential known?", "Kelayakan dikenali?",
		"Deny · unknown card", "Tolak · kad tidak dikenali",
		": no", ": tidak",
		": yes", ": ya",
	).Replace(badgeFlow)

	en, err := Parse(KindFlow, badgeFlow)
	if err != nil {
		t.Fatalf("en: %v", err)
	}
	ms, err := Parse(KindFlow, translated)
	if err != nil {
		t.Fatalf("ms: %v", err)
	}
	if en.Fingerprint() != ms.Fingerprint() {
		t.Errorf("translating the labels changed the fingerprint:\n en: %s\n ms: %s", en.Fingerprint(), ms.Fingerprint())
	}

	// …and dropping an edge must change it, or the check guards nothing.
	short, err := Parse(KindFlow, strings.Replace(translated, "duress -> grant : tidak\n", "", 1))
	if err != nil {
		t.Fatalf("short: %v", err)
	}
	if en.Fingerprint() == short.Fingerprint() {
		t.Error("a missing edge produced the same fingerprint")
	}
}

// An unlabelled arrow and a labelled one are different diagrams even when the label is a word
// the fingerprint cannot read, so losing a label in one language is caught.
func TestFingerprintSeesALostLabel(t *testing.T) {
	full, err := Parse(KindFlow, badgeFlow)
	if err != nil {
		t.Fatal(err)
	}
	stripped, err := Parse(KindFlow, strings.Replace(badgeFlow, "known  -> sched : yes", "known  -> sched", 1))
	if err != nil {
		t.Fatal(err)
	}
	if full.Fingerprint() == stripped.Fingerprint() {
		t.Error("dropping an edge label produced the same fingerprint")
	}
}

func TestParseSpec(t *testing.T) {
	const src = "" +
		"title : Ports a paired node uses\n" +
		"row discovery `49531/udp` : Multicast discovery. Only a control plane holding the fleet key can see this node.\n" +
		"row control `49532/tcp` : The node dials the parent. Mutual TLS, client certificate from the fleet CA.\n" +
		"row media `49534/tcp` : Live video relayed to the control plane, on the same mutual TLS.\n"

	d, err := Parse(KindSpec, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	values := d.Values()
	for id, want := range map[string]string{
		"discovery": "49531/udp",
		"control":   "49532/tcp",
		"media":     "49534/tcp",
	} {
		if values[id] != want {
			t.Errorf("value %s = %q, want %q", id, values[id], want)
		}
	}

	// A value is structure. A translator who "corrects" a port number must break the build.
	bent, err := Parse(KindSpec, strings.Replace(src, "49532/tcp", "49352/tcp", 1))
	if err != nil {
		t.Fatal(err)
	}
	if d.Fingerprint() == bent.Fingerprint() {
		t.Error("a changed port produced the same fingerprint")
	}
}

func TestParseSeqAndArch(t *testing.T) {
	seq, err := Parse(KindSeq, `
actor node : The appliance
actor parent : The control plane

node -> parent : claim code + fleet-key signature
parent --> node : signed certificate
`)
	if err != nil {
		t.Fatalf("seq: %v", err)
	}
	if len(seq.Nodes) != 2 || len(seq.Edges) != 2 || !seq.Edges[1].Dashed {
		t.Errorf("seq = %+v", seq)
	}

	arch, err := Parse(KindArch, `
ext camera : IP camera
box capture : Capture
store disk : Recordings on disk

camera -> capture : RTSP
capture -> disk : segments
`)
	if err != nil {
		t.Fatalf("arch: %v", err)
	}
	if len(arch.Nodes) != 3 || arch.Nodes[2].Shape != "store" {
		t.Errorf("arch = %+v", arch)
	}
}

// Strictness is the whole contract with the frontend renderer: anything the build cannot fully
// understand must never reach a reader's appliance.
func TestParseRejects(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		src  string
		want string
	}{
		{"no label", KindFlow, "step intake", "no label"},
		{"empty label", KindFlow, "step intake :", "empty label"},
		{"unknown shape", KindFlow, "cloud intake : Something", "not a flow shape"},
		{"arch shape in a flow", KindFlow, "box intake : Something", "not a flow shape"},
		{"duplicate id", KindFlow, "step a : One\nstep a : Two\na -> a : x", "duplicate id"},
		{"edge to nowhere", KindFlow, "step a : One\nstep b : Two\na -> c", `edge to "c"`},
		{"stranded node", KindFlow, "step a : One\nstep b : Two", "no edges"},
		{"self edge", KindFlow, "step a : One\na -> a", "points at itself"},
		{"bad id", KindFlow, "step a.b : One\nstep c : Two\na.b -> c", "may only use letters"},
		{"spec with an arrow", KindSpec, "row a `1` : One\nrow b `2` : Two\na -> b", "no arrows"},
		{"spec row with no value", KindSpec, "row a : One", "states a literal value"},
		{"value outside a spec", KindFlow, "step a `1` : One", "only a spec row"},
		{"unclosed value", KindSpec, "row a `1 : One", "unclosed"},
		{"junk before the label", KindFlow, "step a wat : One", `unexpected "wat"`},
		{"link with no target", KindFlow, "step a => : One", "no article to link to"},
		{"two titles", KindFlow, "title : A\ntitle : B\nstep a : One", "a second title"},
		{"nothing at all", KindFlow, "\n\n", "no nodes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.kind, tc.src)
			if err == nil {
				t.Fatalf("parsed, want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// A label is prose and may contain anything, including the punctuation the grammar uses. Once
// the colon is passed, parsing stops looking at syntax.
func TestLabelMayContainSyntax(t *testing.T) {
	d, err := Parse(KindFlow, "step a : Set `mtlsPort` to 49532 -> then restart => done\nstep b : Next\na -> b")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Nodes[0].Label != "Set `mtlsPort` to 49532 -> then restart => done" {
		t.Errorf("label = %q", d.Nodes[0].Label)
	}
	if d.Nodes[0].Value != "" || d.Nodes[0].Link != "" {
		t.Errorf("prose leaked into structure: %+v", d.Nodes[0])
	}
}

func TestFences(t *testing.T) {
	body := "Intro.\n\n```flow\nstep a : One\nstep b : Two\na -> b\n```\n\n" +
		"```go\n// not a diagram\n```\n\n" +
		"```spec\nrow p `1` : One\n```\n"

	got := Fences(body)
	if len(got) != 2 {
		t.Fatalf("fences = %d, want 2", len(got))
	}
	if got[0].Kind != KindFlow || got[1].Kind != KindSpec {
		t.Errorf("kinds = %v, %v", got[0].Kind, got[1].Kind)
	}
	if got[0].Line != 3 {
		t.Errorf("first fence opens at line %d, want 3", got[0].Line)
	}
	for _, raw := range got {
		if _, err := Parse(raw.Kind, raw.Body); err != nil {
			t.Errorf("%s fence: %v", raw.Kind, err)
		}
	}
}

// A code block that quotes a fence opener must not be read as one — a manual explaining this
// very grammar will contain exactly that.
func TestFencesIgnoreQuotedOpeners(t *testing.T) {
	body := "```text\nWrite ```flow to draw a decision path.\n```\n"
	if got := Fences(body); len(got) != 0 {
		t.Errorf("found %d fences inside a text block, want 0", len(got))
	}
}
