package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/control"
)

// searchStub is one node's scripted answer, keyed by the node path prefix. delay models a
// node that accepts the request and never gets round to answering.
type searchStub struct {
	status int
	body   string
	err    error
	delay  time.Duration
}

// fakeSearchSender answers tunneled requests from a script: nodeId → path prefix → stub.
type fakeSearchSender struct {
	mu      sync.Mutex
	scripts map[string]map[string]searchStub
	seen    []string
}

func (f *fakeSearchSender) SendRequest(ctx context.Context, nodeID string, req control.Request) (control.Response, error) {
	f.mu.Lock()
	f.seen = append(f.seen, nodeID+" "+req.Path)
	f.mu.Unlock()
	byPath := f.scripts[nodeID]
	for prefix, stub := range byPath {
		if strings.HasPrefix(req.Path, prefix) {
			if stub.err != nil {
				return control.Response{}, stub.err
			}
			if stub.delay > 0 {
				// Mirror the real sender: a wedged node is not an error, it is silence
				// until the caller's context gives up.
				select {
				case <-time.After(stub.delay):
				case <-ctx.Done():
					return control.Response{}, ctx.Err()
				}
			}
			return control.Response{Status: stub.status, Body: []byte(stub.body)}, nil
		}
	}
	return control.Response{Status: http.StatusNotFound, Body: []byte("no route")}, nil
}

// fakeSearchAccess grants a fixed access level per node; a node absent from the map is
// unreachable for the role.
type fakeSearchAccess struct {
	INodeAccessService
	grants map[string]NodeAccess
}

func (f *fakeSearchAccess) Resolve(_ context.Context, _ int64, nodeId string) (NodeAccess, error) {
	return f.grants[nodeId], nil
}

type fakeSearchSites []*entities.Site

func (f fakeSearchSites) ListSites(context.Context) ([]*entities.Site, error) { return f, nil }

// okPage renders a node answer in the shape the node endpoints really return —
// controllers.SendResult's {message, durationMs, result} envelope around a SightingPage.
// Getting this wrong in the fixture would have made every test pass against a decoder that
// could not read a single real node.
func okPage(capped bool, oldest int64, items ...map[string]any) string {
	payload := map[string]any{"items": items, "capped": capped, "oldest": oldest}
	body, _ := json.Marshal(map[string]any{"message": "succeed", "result": payload})
	return string(body)
}

func hit(id int64, startedAt int64, label string) map[string]any {
	return map[string]any{"kind": "object", "id": id, "cameraId": 1, "cameraName": "Gate", "label": label, "startedAt": startedAt, "endedAt": startedAt + 5, "confidence": 0.9}
}

func nodeAt(id, name string, siteId int64) *entities.ManagedNode {
	return &entities.ManagedNode{NodeId: id, Name: name, Kind: "camera", SiteId: siteId}
}

func fullAccess(ids ...string) *fakeSearchAccess {
	grants := map[string]NodeAccess{}
	for _, id := range ids {
		grants[id] = NodeAccess{CanRead: true, CanOperate: true, CanWrite: true}
	}
	return &fakeSearchAccess{grants: grants}
}

// coverageFor returns one node's coverage row from a result.
func coverageFor(t *testing.T, cov SearchCoverage, nodeId string) NodeCoverage {
	t.Helper()
	for _, n := range cov.Nodes {
		if n.NodeId == nodeId {
			return n
		}
	}
	t.Fatalf("node %q missing from coverage (%+v)", nodeId, cov.Nodes)
	return NodeCoverage{}
}

// TestFleetSearchReportsUnreachableNodeRatherThanOmittingIt is the defining behaviour of
// this feature. A node that could not be asked must appear in the coverage block with a
// reason, and the result must NOT claim completeness — otherwise an empty answer reads as
// "the fleet never saw it" when it means "half the fleet was never asked".
func TestFleetSearchReportsUnreachableNodeRatherThanOmittingIt(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"up":   {"/api/": {status: 200, body: okPage(false, 100, hit(1, 100, "person"))}},
		"down": {"/api/": {err: ErrNodeOffline}},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("up", "Up", 0), nodeAt("down", "Down", 0)},
		nil, sender, fullAccess("up", "down"))

	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Coverage.Searched != 2 || res.Coverage.Answered != 1 {
		t.Fatalf("searched/answered = %d/%d, want 2/1", res.Coverage.Searched, res.Coverage.Answered)
	}
	if res.Coverage.Complete {
		t.Fatal("a search that could not reach a node must not report itself complete")
	}
	down := coverageFor(t, res.Coverage, "down")
	if down.Status != SearchNodeOffline {
		t.Fatalf("offline node status = %q, want %q", down.Status, SearchNodeOffline)
	}
	if down.Reason == "" {
		t.Fatal("an unreachable node must carry a reason an operator can act on")
	}
	if len(res.Items) != 1 {
		t.Fatalf("items = %d, want the one hit the reachable node returned", len(res.Items))
	}
}

// TestFleetSearchClassifiesNodeRefusals keeps apart the failures whose remedies differ:
// an old build that has no such endpoint, and a node that refused the asserted role.
func TestFleetSearchClassifiesNodeRefusals(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"old":      {"/api/": {status: http.StatusNotFound, body: "not found"}},
		"guarded":  {"/api/": {status: http.StatusForbidden, body: "denied"}},
		"confused": {"/api/": {status: http.StatusInternalServerError, body: "boom"}},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("old", "Old", 0), nodeAt("guarded", "Guarded", 0), nodeAt("confused", "Confused", 0)},
		nil, sender, fullAccess("old", "guarded", "confused"))

	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for nodeId, want := range map[string]string{
		"old":      SearchNodeUnsupported,
		"guarded":  SearchNodeDenied,
		"confused": SearchNodeError,
	} {
		if got := coverageFor(t, res.Coverage, nodeId).Status; got != want {
			t.Errorf("node %s status = %q, want %q", nodeId, got, want)
		}
	}
	if res.Coverage.Answered != 0 {
		t.Fatalf("answered = %d, want 0 — none of these nodes returned anything", res.Coverage.Answered)
	}
}

// TestFleetSearchMergesNewestFirstAndStampsOrigin checks the merge across nodes and that
// every row can be traced back to the node and site it came from — a sighting with no
// origin is unusable in an estate where every node numbers its cameras from 1.
func TestFleetSearchMergesNewestFirstAndStampsOrigin(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"a": {"/api/": {status: 200, body: okPage(false, 100, hit(1, 300, "person"), hit(2, 100, "car"))}},
		"b": {"/api/": {status: 200, body: okPage(false, 200, hit(7, 200, "person"))}},
	}}
	sites := fakeSearchSites{{Id: 5, Name: "North Depot"}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("a", "Alpha", 5), nodeAt("b", "Bravo", 0)},
		sites, sender, fullAccess("a", "b"))

	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	gotOrder := []int64{}
	for _, item := range res.Items {
		gotOrder = append(gotOrder, item.StartedAt)
	}
	want := []int64{300, 200, 100}
	if fmt.Sprint(gotOrder) != fmt.Sprint(want) {
		t.Fatalf("merged order = %v, want newest first %v", gotOrder, want)
	}
	if res.Items[0].NodeName != "Alpha" || res.Items[0].SiteName != "North Depot" {
		t.Fatalf("hit origin = node %q site %q, want Alpha / North Depot", res.Items[0].NodeName, res.Items[0].SiteName)
	}
	if res.Items[1].NodeId != "b" {
		t.Fatalf("second hit came from %q, want b", res.Items[1].NodeId)
	}
	if !res.Coverage.Complete {
		t.Fatal("every node answered and none capped — this search IS complete")
	}
}

// TestFleetSearchCompletenessHorizonTakesTheNEWESTCappedOldest pins the direction of the
// horizon. Two nodes both truncated their answers; the fleet result is only complete back
// to where the SHALLOWEST of them stopped, which is the LATEST of the two horizons. Taking
// the earliest instead would claim coverage the shallow node never provided — the same
// flattering-direction error that made W2-2 under-report outages.
func TestFleetSearchCompletenessHorizonTakesTheNEWESTCappedOldest(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		// "deep" reached back to 100; "shallow" ran out of room at 500.
		"deep":    {"/api/": {status: 200, body: okPage(true, 100, hit(1, 900, "person"), hit(2, 100, "person"))}},
		"shallow": {"/api/": {status: 200, body: okPage(true, 500, hit(3, 800, "person"), hit(4, 500, "person"))}},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("deep", "Deep", 0), nodeAt("shallow", "Shallow", 0)},
		nil, sender, fullAccess("deep", "shallow"))

	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Coverage.Complete {
		t.Fatal("a capped node means the result is not complete")
	}
	if res.Coverage.CompleteThrough != 500 {
		t.Fatalf("completeThrough = %d, want 500 (the newest of the two capped horizons)", res.Coverage.CompleteThrough)
	}
}

// TestFleetSearchTruncationEndsCompletenessAtTheOldestKeptRow covers the OTHER cap: the
// control plane running out of room after the nodes answered in full.
func TestFleetSearchTruncationEndsCompletenessAtTheOldestKeptRow(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"a": {"/api/": {status: 200, body: okPage(false, 100, hit(1, 300, "person"), hit(2, 200, "person"), hit(3, 100, "person"))}},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("a", "Alpha", 0)}, nil, sender, fullAccess("a"))

	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}, Limit: 2})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !res.Truncated || len(res.Items) != 2 {
		t.Fatalf("truncated=%v items=%d, want true/2", res.Truncated, len(res.Items))
	}
	if res.Coverage.Complete {
		t.Fatal("a truncated page cannot be a complete answer")
	}
	if res.Coverage.CompleteThrough != 200 {
		t.Fatalf("completeThrough = %d, want 200 (the oldest row still on the page)", res.Coverage.CompleteThrough)
	}
}

// TestFleetSearchOmitsNodesTheRoleCannotReach checks that inaccessible nodes are absent
// entirely — not listed as denied. Listing them would disclose the estate's shape to a
// role granted a single recorder.
func TestFleetSearchOmitsNodesTheRoleCannotReach(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"mine": {"/api/": {status: 200, body: okPage(false, 0)}},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("mine", "Mine", 0), nodeAt("theirs", "Theirs", 0)},
		nil, sender, fullAccess("mine"))

	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Coverage.Searched != 1 || len(res.Coverage.Nodes) != 1 {
		t.Fatalf("searched=%d nodes=%d, want 1/1", res.Coverage.Searched, len(res.Coverage.Nodes))
	}
	for _, seen := range sender.seen {
		if strings.HasPrefix(seen, "theirs ") {
			t.Fatalf("an inaccessible node was queried anyway: %s", seen)
		}
	}
}

// TestFleetSearchSkipsNodeKindsThatCannotHoldSightings keeps a door controller out of the
// searched count, and reports the skip so "1 of 2 nodes" never reads as a failure.
func TestFleetSearchSkipsNodeKindsThatCannotHoldSightings(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"cam": {"/api/": {status: 200, body: okPage(false, 0)}},
	}}
	door := &entities.ManagedNode{NodeId: "door", Name: "Door", Kind: "door"}
	legacy := &entities.ManagedNode{NodeId: "legacy", Name: "Legacy"} // empty kind == camera
	sender.scripts["legacy"] = map[string]searchStub{"/api/": {status: 200, body: okPage(false, 0)}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("cam", "Cam", 0), door, legacy},
		nil, sender, fullAccess("cam", "door", "legacy"))

	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Coverage.Searched != 2 {
		t.Fatalf("searched = %d, want 2 (the camera node and the kind-less legacy one)", res.Coverage.Searched)
	}
	if res.Coverage.SkippedKind != 1 {
		t.Fatalf("skippedKind = %d, want 1", res.Coverage.SkippedKind)
	}
}

// TestFleetSearchPartialSourceFailureIsNotAnOkNode covers the split-grant case: a role
// that may read a node's object metadata but not its alert log. The node contributed
// something, so it counts as answered — but calling it "ok" would let "this plate was
// never seen here" be said about a node whose identity index was never read.
func TestFleetSearchPartialSourceFailureIsNotAnOkNode(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"a": {
			"/api/observations/search":      {status: 200, body: okPage(false, 100, hit(1, 100, "person"))},
			"/api/vision/alerts/identities": {status: http.StatusForbidden, body: "denied"},
		},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("a", "Alpha", 0)}, nil, sender, fullAccess("a"))

	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	node := coverageFor(t, res.Coverage, "a")
	if node.Status != SearchNodeDenied {
		t.Fatalf("node status = %q, want %q — one source was refused", node.Status, SearchNodeDenied)
	}
	if res.Coverage.Answered != 1 {
		t.Fatalf("answered = %d, want 1 — the node did contribute its object hits", res.Coverage.Answered)
	}
	if res.Coverage.Complete {
		t.Fatal("a refused source means the answer is not complete")
	}
	if len(res.Items) != 1 {
		t.Fatalf("items = %d, want the object hit that did come back", len(res.Items))
	}
}

// TestFleetSearchWithNoReachableNodesIsNotComplete guards the emptiest case of all. A
// search that asked nobody must never present itself as a confident "nothing was seen".
func TestFleetSearchWithNoReachableNodesIsNotComplete(t *testing.T) {
	svc := NewFleetSearchService(fakeNodeList{}, nil, &fakeSearchSender{}, fullAccess())
	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Coverage.Complete {
		t.Fatal("a search with no reachable node must not report completeness")
	}
	if len(res.Items) != 0 || res.Coverage.Searched != 0 {
		t.Fatalf("items=%d searched=%d, want 0/0", len(res.Items), res.Coverage.Searched)
	}
}

// TestFleetSearchAsksEveryNodeForTheFullLimit pins the fan-out budget. Dividing the limit
// across nodes would be cheaper and wrong: the newest N sightings in a fleet can all come
// from one busy node.
func TestFleetSearchAsksEveryNodeForTheFullLimit(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"a": {"/api/": {status: 200, body: okPage(false, 0)}},
		"b": {"/api/": {status: 200, body: okPage(false, 0)}},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("a", "A", 0), nodeAt("b", "B", 0)}, nil, sender, fullAccess("a", "b"))
	if _, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}, Limit: 300}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(sender.seen) != 2 {
		t.Fatalf("tunneled requests = %d, want one per node", len(sender.seen))
	}
	for _, seen := range sender.seen {
		if !strings.Contains(seen, "limit=300") {
			t.Fatalf("node request %q did not ask for the full limit", seen)
		}
	}
}

// TestFleetSearchSiteScopeNarrowsTheFanOut covers the "site" query term from F-10.
func TestFleetSearchSiteScopeNarrowsTheFanOut(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"north": {"/api/": {status: 200, body: okPage(false, 0)}},
		"south": {"/api/": {status: 200, body: okPage(false, 0)}},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("north", "North", 5), nodeAt("south", "South", 6)},
		nil, sender, fullAccess("north", "south"))

	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}, SiteId: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Coverage.Searched != 1 || res.Coverage.Nodes[0].NodeId != "north" {
		t.Fatalf("site scope searched %+v, want only the north node", res.Coverage.Nodes)
	}
}

// TestFleetLabelsUnionsEveryNodeAndReportsTheOnesItCouldNotAsk. The label picker built
// from this list decides which searches an operator can even express, so a node missing
// from it removes its labels from every subsequent search without a word.
func TestFleetLabelsUnionsEveryNodeAndReportsTheOnesItCouldNotAsk(t *testing.T) {
	labels := func(vals ...string) string {
		body, _ := json.Marshal(map[string]any{"message": "succeed", "result": vals})
		return string(body)
	}
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"a":    {"/api/observations/labels": {status: 200, body: labels("person", "car")}},
		"b":    {"/api/observations/labels": {status: 200, body: labels("car", "dog")}},
		"down": {"/api/observations/labels": {err: ErrNodeDisconnected}},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("a", "A", 0), nodeAt("b", "B", 0), nodeAt("down", "Down", 0)},
		nil, sender, fullAccess("a", "b", "down"))

	res, err := svc.Labels(context.Background(), 1, FleetSearchQuery{})
	if err != nil {
		t.Fatalf("labels: %v", err)
	}
	if fmt.Sprint(res.Labels) != fmt.Sprint([]string{"car", "dog", "person"}) {
		t.Fatalf("labels = %v, want the sorted union", res.Labels)
	}
	if res.Coverage.Complete {
		t.Fatal("a label list missing a node's labels is not complete")
	}
	if coverageFor(t, res.Coverage, "down").Status != SearchNodeOffline {
		t.Fatal("the unreachable node must be named in the label coverage too")
	}
}

// TestFleetSearchDecodesTheEnvelopeVariants keeps the wire tolerant of every response
// shape a node in this estate actually produces: the plain {result} envelope, the
// double-wrapped {data:{result}} some paths use, and a bare payload. A decoder that knew
// only one of them would report a perfectly healthy node as broken.
func TestFleetSearchDecodesTheEnvelopeVariants(t *testing.T) {
	page := map[string]any{"items": []map[string]any{hit(1, 100, "person")}, "capped": false, "oldest": 100}
	bare, _ := json.Marshal(page)
	wrapped, _ := json.Marshal(map[string]any{"message": "succeed", "result": page})
	doubled, _ := json.Marshal(map[string]any{"data": map[string]any{"result": page}})
	for name, body := range map[string]string{"bare": string(bare), "result": string(wrapped), "data.result": string(doubled)} {
		sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
			"a": {"/api/": {status: 200, body: body}},
		}}
		svc := NewFleetSearchService(fakeNodeList{nodeAt("a", "A", 0)}, nil, sender, fullAccess("a"))
		res, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}})
		if err != nil {
			t.Fatalf("%s: search: %v", name, err)
		}
		if len(res.Items) != 1 {
			t.Fatalf("%s envelope: items = %d, want 1", name, len(res.Items))
		}
	}
}

// TestSearchSourcesWantedDefaultsToBoth guards the typo case: a narrowed search that
// silently matches nothing is worse than one that ignores the unknown word.
func TestSearchSourcesWantedDefaultsToBoth(t *testing.T) {
	for _, in := range [][]string{nil, {}, {"  "}, {"objekts"}} {
		objects, identities := searchSourcesWanted(in)
		if !objects || !identities {
			t.Fatalf("sources %v resolved to objects=%v identities=%v, want both", in, objects, identities)
		}
	}
	if objects, identities := searchSourcesWanted([]string{"identities"}); objects || !identities {
		t.Fatal("an explicit single source must narrow the search")
	}
}

// TestFleetSearchReportsAWedgedNodeAsATimeout. A node that accepts the request and never
// answers is the case a per-node deadline exists for, and the outcome must be a NAMED
// timeout rather than a node quietly contributing nothing.
func TestFleetSearchReportsAWedgedNodeAsATimeout(t *testing.T) {
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"fine":   {"/api/": {status: 200, body: okPage(false, 100, hit(1, 100, "person"))}},
		"wedged": {"/api/": {status: 200, body: okPage(false, 0), delay: 30 * time.Second}},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("fine", "Fine", 0), nodeAt("wedged", "Wedged", 0)},
		nil, sender, fullAccess("fine", "wedged"))

	// No caller deadline: the SERVICE's own budget is what has to bound this, because a
	// browser waiting on the endpoint imposes none.
	svc.budget = 300 * time.Millisecond
	started := time.Now()
	res, err := svc.Search(context.Background(), 1, FleetSearchQuery{Sources: []string{SearchSourceObjects}})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// The BUDGET has to be what stopped it, not the per-node deadline. Without a
	// whole-search bound, an estate of wedged appliances holds the operator's request open
	// for the per-node timeout multiplied by the number of batches — and still answers.
	if elapsed > 5*time.Second {
		t.Fatalf("search took %s against a 300ms budget — the per-node deadline stopped it, not the budget", elapsed)
	}
	if got := coverageFor(t, res.Coverage, "wedged").Status; got != SearchNodeTimeout {
		t.Fatalf("wedged node status = %q, want %q", got, SearchNodeTimeout)
	}
	if res.Coverage.Complete {
		t.Fatal("a search that timed out against a node is not complete")
	}
	if len(res.Items) != 1 || res.Items[0].NodeName != "Fine" {
		t.Fatalf("items = %+v, want the responsive node's hit", res.Items)
	}
}

// TestFleetLabelsIsBoundedAndReportsTheNodeItGaveUpOn. The label list is the more insidious
// half: it decides which searches an operator can even express, so a wedged node silently
// dropping out of it removes whatever that node saw from every subsequent search — and does
// so before the operator has typed anything to be suspicious about.
func TestFleetLabelsIsBoundedAndReportsTheNodeItGaveUpOn(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"message": "succeed", "result": []string{"person"}})
	sender := &fakeSearchSender{scripts: map[string]map[string]searchStub{
		"fine":   {"/api/observations/labels": {status: 200, body: string(body)}},
		"wedged": {"/api/observations/labels": {status: 200, body: string(body), delay: 30 * time.Second}},
	}}
	svc := NewFleetSearchService(fakeNodeList{nodeAt("fine", "Fine", 0), nodeAt("wedged", "Wedged", 0)},
		nil, sender, fullAccess("fine", "wedged"))

	svc.budget = 300 * time.Millisecond
	done := make(chan struct{})
	var res FleetLabelsResult
	var err error
	go func() {
		res, err = svc.Labels(context.Background(), 1, FleetSearchQuery{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Labels never returned — a wedged node must not hold the label list open")
	}
	if err != nil {
		t.Fatalf("labels: %v", err)
	}
	if got := coverageFor(t, res.Coverage, "wedged").Status; got != SearchNodeTimeout {
		t.Fatalf("wedged node status = %q, want a timeout", got)
	}
	if res.Coverage.Complete {
		t.Fatal("a label list missing a node's labels is not complete")
	}
}
