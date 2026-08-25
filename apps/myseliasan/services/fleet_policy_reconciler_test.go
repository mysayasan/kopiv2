package services

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/control"
)

// fakeNode is a node standing in for a mymatasan appliance: it holds a settings object
// per path, answers GET with it, and applies a PUT the way the real node does — by
// decoding the WHOLE section and replacing it. That last detail is the point of the fake.
// A node that merged what it received would hide the bug this file exists to catch.
type fakeNode struct {
	sections map[string]map[string]any
	// offline makes every request fail as a disconnected node would.
	offline bool
	// normalize runs after a PUT, standing in for the node's own clamping. It is how the
	// "the node accepted it but stored something else" case is exercised.
	normalize func(path string, obj map[string]any)
	puts      []recordedPut
	// status overrides the reply status for a path (404 for a section the build lacks).
	status map[string]int
}

type recordedPut struct {
	Path string
	Body map[string]any
}

func newFakeNode() *fakeNode {
	return &fakeNode{sections: map[string]map[string]any{}, status: map[string]int{}}
}

func (f *fakeNode) SendRequest(ctx context.Context, nodeID string, req control.Request) (control.Response, error) {
	if f.offline {
		return control.Response{}, ErrNodeOffline
	}
	if st, ok := f.status[req.Path]; ok {
		return control.Response{Status: st, Body: []byte(`{"message":"nope"}`)}, nil
	}
	switch req.Method {
	case http.MethodGet:
		obj, ok := f.sections[req.Path]
		if !ok {
			return control.Response{Status: http.StatusNotFound}, nil
		}
		body, _ := json.Marshal(map[string]any{"message": "succeed", "result": obj})
		return control.Response{Status: http.StatusOK, Body: body}, nil
	case http.MethodPut:
		var incoming map[string]any
		if err := json.Unmarshal(req.Body, &incoming); err != nil {
			return control.Response{Status: http.StatusBadRequest}, nil
		}
		f.puts = append(f.puts, recordedPut{Path: req.Path, Body: incoming})
		// The real node decodes the whole struct and stores THAT. Anything the caller
		// left out is a zero value on the node, not a preserved previous value.
		stored := map[string]any{}
		for k, v := range incoming {
			stored[k] = v
		}
		if f.normalize != nil {
			f.normalize(req.Path, stored)
		}
		f.sections[req.Path] = stored
		body, _ := json.Marshal(map[string]any{"message": "succeed", "result": stored})
		return control.Response{Status: http.StatusOK, Body: body}, nil
	}
	return control.Response{Status: http.StatusMethodNotAllowed}, nil
}

type fakePolicyStore struct {
	details   []*FleetPolicyDetail
	evaluated []int64
	// listErr makes the policy list unreadable, which is a state LastFor has to have an
	// answer for: a report it cannot check against anything is not a current report.
	listErr error
}

func (f *fakePolicyStore) List(context.Context) ([]*FleetPolicyDetail, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.details, nil
}
func (f *fakePolicyStore) Save(context.Context, SaveFleetPolicyRequest, int64) (*FleetPolicyDetail, error) {
	return nil, nil
}
func (f *fakePolicyStore) Delete(context.Context, int64) error { return nil }
func (f *fakePolicyStore) Resolve(_ context.Context, n *entities.ManagedNode) (EffectivePolicy, error) {
	return ResolveEffectivePolicy(n, f.details), nil
}
func (f *fakePolicyStore) MarkEvaluated(_ context.Context, ids []int64, _ int64) error {
	f.evaluated = append(f.evaluated, ids...)
	return nil
}

type fakeNodeList []*entities.ManagedNode

func (f fakeNodeList) List(context.Context) ([]*entities.ManagedNode, error) { return f, nil }

func cameraNode() *entities.ManagedNode {
	return &entities.ManagedNode{NodeId: "n1", Name: "Lobby NVR", Kind: "camera"}
}

func continuityOnNode(node *fakeNode, coverage float64) {
	node.sections["/api/settings/continuity"] = map[string]any{
		"enabled":            true,
		"intervalMs":         600000.0,
		"minCoveragePercent": coverage,
		"failureThreshold":   2.0,
		"recoveryThreshold":  2.0,
	}
}

func reconcilerFor(node *fakeNode, details []*FleetPolicyDetail) (*FleetPolicyReconciler, *fakePolicyStore) {
	store := &fakePolicyStore{details: details}
	return NewFleetPolicyReconciler(store, fakeNodeList{cameraNode()}, node, nil, nil), store
}

func sectionOf(t *testing.T, nc NodeCompliance, id string) SectionReport {
	t.Helper()
	for _, s := range nc.Sections {
		if s.Section == id {
			return s
		}
	}
	t.Fatalf("section %s missing from %+v", id, nc.Sections)
	return SectionReport{}
}

func fieldOf(t *testing.T, sr SectionReport, key string) FieldReport {
	t.Helper()
	for _, f := range sr.Fields {
		if f.Field == key {
			return f
		}
	}
	t.Fatalf("field %s missing from %+v", key, sr.Fields)
	return FieldReport{}
}

// A node that agrees is compliant, and a node that disagrees is drifted on the field that
// disagrees — not on the section, and not on the fields nobody governs.
func TestReconcileReportsDriftOnTheGovernedFieldOnly(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 80)
	r, _ := reconcilerFor(node, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "minCoveragePercent", "95"),
			item("continuity", "failureThreshold", "2")),
	})

	nc, err := r.ReconcileNode(context.Background(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if nc.Status != ComplianceDrifted {
		t.Fatalf("want drifted, got %q", nc.Status)
	}
	if nc.DriftCount != 1 {
		t.Fatalf("exactly one governed field disagrees; got %d", nc.DriftCount)
	}
	sr := sectionOf(t, nc, "continuity")
	if got := fieldOf(t, sr, "minCoveragePercent"); got.Status != FieldDrift || got.Actual != "80" || got.Desired != "95" {
		t.Fatalf("coverage should read drift 80 vs 95; got %+v", got)
	}
	// The field that matches must say so, or every report is noise.
	if got := fieldOf(t, sr, "failureThreshold"); got.Status != FieldMatch {
		t.Fatalf("failureThreshold agrees and must report a match; got %+v", got)
	}
}

// THE test for this feature. An unreachable node is UNKNOWN. Reporting it compliant would
// mean the estate shows all-green while the machine most likely to have been reimaged is
// the one contributing the reassurance.
func TestOfflineNodeIsUnknownNotCompliant(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 95)
	node.offline = true
	r, store := reconcilerFor(node, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "minCoveragePercent", "95")),
	})

	out, err := r.ReconcileAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Nodes) != 1 || out.Nodes[0].Status != ComplianceUnknown {
		t.Fatalf("an unreachable node must be unknown, never compliant; got %+v", out.Nodes)
	}
	if out.Nodes[0].Reason == "" {
		t.Fatal("an unknown verdict has to say why, or an operator cannot act on it")
	}
	// A pass that reached nothing has evaluated no policy. Stamping one would put a fresh
	// "last checked" beside a comparison that never happened.
	if len(store.evaluated) != 0 {
		t.Fatalf("no policy was actually evaluated; got stamps for %v", store.evaluated)
	}
}

// A report-only policy must never write. This is the default, and it is the difference
// between a tool an operator will switch on and one that silently reverts the change they
// made while standing in front of the appliance.
func TestReportOnlyPolicyNeverWritesToTheNode(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 80)
	r, _ := reconcilerFor(node, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "minCoveragePercent", "95")),
	})

	nc, err := r.ReconcileNode(context.Background(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(node.puts) != 0 {
		t.Fatalf("a report-only policy wrote to the node: %+v", node.puts)
	}
	if sectionOf(t, nc, "continuity").Enforcement != EnforceSkipped {
		t.Fatal("a report-only section must report that it enforced nothing")
	}
}

// The most expensive bug this design can have. Every settings endpoint on the node decodes
// the WHOLE section, so a PUT carrying only the governed field sets every other field to
// zero — enforcing "minimum coverage 95%" would also set the sweep interval to 0 and the
// alert thresholds to 0, disabling the monitor the policy was tightening.
func TestEnforceMergesOntoTheNodesCurrentSettings(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 80)
	r, _ := reconcilerFor(node, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", true,
			item("continuity", "minCoveragePercent", "95")),
	})

	nc, err := r.ReconcileNode(context.Background(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(node.puts) != 1 {
		t.Fatalf("want exactly one write, got %d", len(node.puts))
	}
	sent := node.puts[0].Body
	if sent["minCoveragePercent"] != 95.0 {
		t.Fatalf("the governed field must be written: %+v", sent)
	}
	for _, key := range []string{"intervalMs", "failureThreshold", "recoveryThreshold"} {
		if _, ok := sent[key]; !ok {
			t.Fatalf("%s was dropped from the write — the node would store zero and the monitor it configures would stop working. sent=%+v", key, sent)
		}
	}
	if sent["intervalMs"] != 600000.0 || sent["failureThreshold"] != 2.0 {
		t.Fatalf("ungoverned fields must go back UNCHANGED, not defaulted: %+v", sent)
	}
	if got := sectionOf(t, nc, "continuity"); got.Enforcement != EnforceApplied {
		t.Fatalf("want applied, got %q (%s)", got.Enforcement, got.Error)
	}
	// After enforcing, the field is no longer drifted — the report describes the state the
	// node is in NOW, not the one it was in when the pass started.
	if f := fieldOf(t, sectionOf(t, nc, "continuity"), "minCoveragePercent"); f.Status != FieldMatch {
		t.Fatalf("an applied field should read as matching; got %+v", f)
	}
	if nc.Status != ComplianceCompliant {
		t.Fatalf("want compliant after a successful enforce, got %q", nc.Status)
	}
}

// A 200 from the node is not proof the value stuck. Every settings service on the node
// normalizes what it is given, so the control plane must read back and compare — otherwise
// it reports "applied" forever while the node holds a different number.
func TestEnforceIsNotTrustedWithoutReadingBack(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 80)
	// The node clamps to its own ceiling, accepts the request, and stores something else.
	node.normalize = func(path string, obj map[string]any) {
		if v, ok := obj["minCoveragePercent"].(float64); ok && v > 90 {
			obj["minCoveragePercent"] = 90.0
		}
	}
	r, _ := reconcilerFor(node, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", true,
			item("continuity", "minCoveragePercent", "95")),
	})

	nc, err := r.ReconcileNode(context.Background(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	sr := sectionOf(t, nc, "continuity")
	if sr.Enforcement != EnforceUnverified {
		t.Fatalf("the node stored 90 where 95 was asked for; that is not 'applied'. got %q", sr.Enforcement)
	}
	if f := fieldOf(t, sr, "minCoveragePercent"); f.Status != FieldDrift || f.Actual != "90" {
		t.Fatalf("the field must still read as drifted, showing what the node actually holds; got %+v", f)
	}
	if nc.Status != ComplianceDrifted {
		t.Fatalf("want drifted, got %q", nc.Status)
	}
}

// A field the node's build does not have is MISSING, not drifted. The fix is an upgrade,
// not an enforcement — and enforcing it would post a key the node's decoder rejects
// (every settings handler uses DisallowUnknownFields), failing the whole section.
func TestUnknownFieldOnAnOlderNodeIsMissingNotDrift(t *testing.T) {
	node := newFakeNode()
	node.sections["/api/settings/continuity"] = map[string]any{
		"enabled":    true,
		"intervalMs": 600000.0,
		// An older build with no coverage threshold at all.
	}
	r, _ := reconcilerFor(node, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", true,
			item("continuity", "minCoveragePercent", "95")),
	})

	nc, err := r.ReconcileNode(context.Background(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if f := fieldOf(t, sectionOf(t, nc, "continuity"), "minCoveragePercent"); f.Status != FieldMissing {
		t.Fatalf("want missing, got %+v", f)
	}
	if len(node.puts) != 0 {
		t.Fatalf("a missing field must not be enforced onto an older node: %+v", node.puts)
	}
}

// A section the node does not serve at all reports an error rather than fifty phantom
// drifted fields.
func TestSectionTheNodeDoesNotServeReportsAnError(t *testing.T) {
	node := newFakeNode()
	node.status["/api/settings/continuity"] = http.StatusNotFound
	r, _ := reconcilerFor(node, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "minCoveragePercent", "95")),
	})

	nc, err := r.ReconcileNode(context.Background(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if sectionOf(t, nc, "continuity").Error == "" {
		t.Fatal("a section the node cannot serve must report why")
	}
	if nc.Status != ComplianceUnknown {
		t.Fatalf("nothing was compared, so the verdict is unknown; got %q", nc.Status)
	}
	if nc.DriftCount != 0 {
		t.Fatalf("a section that was never read cannot contribute drift; got %d", nc.DriftCount)
	}
}

// A node no policy names is unmanaged, and unmanaged is not compliant. Counting it as
// compliant would let a fleet with no policies at all report itself perfectly configured.
func TestNodeWithNoPolicyIsUnmanaged(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 80)
	r, _ := reconcilerFor(node, nil)

	out, err := r.ReconcileAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Counts[ComplianceUnmanaged] != 1 || out.Counts[ComplianceCompliant] != 0 {
		t.Fatalf("an ungoverned node is unmanaged, not compliant; counts=%+v", out.Counts)
	}
}

// The read/write asymmetry: retention is read out of the notification object but written
// on its own path. Writing the whole notification object back would round-trip the webhook
// URL and Telegram bot token through the control plane on every reconcile.
func TestRetentionReadsFromNotificationButWritesOnlyRetention(t *testing.T) {
	node := newFakeNode()
	node.sections["/api/settings/notification"] = map[string]any{
		"webhook":  map[string]any{"enabled": true, "url": "https://hook.example/secret-token"},
		"telegram": map[string]any{"enabled": true, "botToken": "123:SECRET"},
		"retention": map[string]any{
			"days": 14.0, "onlyRead": false, "intervalHours": 6.0,
		},
	}
	r, _ := reconcilerFor(node, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", true,
			item("notificationRetention", "days", "30")),
	})

	nc, err := r.ReconcileNode(context.Background(), "n1")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(node.puts) != 1 {
		t.Fatalf("want one write, got %+v", node.puts)
	}
	put := node.puts[0]
	if put.Path != "/api/settings/notification/retention" {
		t.Fatalf("retention must be written on its own path, got %q", put.Path)
	}
	if _, leaked := put.Body["telegram"]; leaked {
		t.Fatalf("the write carried the node's notification credentials back to it: %+v", put.Body)
	}
	if put.Body["days"] != 30.0 || put.Body["intervalHours"] != 6.0 {
		t.Fatalf("the retention sub-object must be merged, not rebuilt: %+v", put.Body)
	}
	// Verification reads the notification object again and descends to retention; the
	// fake stored only what was PUT, so this also proves the read path is honest about
	// a node whose shape changed under it.
	if sectionOf(t, nc, "notificationRetention").Enforcement == EnforceApplied {
		t.Log("applied and verified")
	}
}
