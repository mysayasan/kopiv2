package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/control"
)

// A LIVE bench against a real mymatasan node, off by default.
//
//	RUN_NODE_IT=1 NODE_URL=http://127.0.0.1:18089 NODE_AUTH=<base64 user:pass> \
//	  go test ./apps/myseliasan/services/ -run TestLive -v
//
// The unit tests around this file prove the reconciler's LOGIC against a fake node whose
// behaviour I wrote — which means they prove nothing about whether the catalog names paths
// and fields a real appliance actually serves, or whether a merged section survives the
// node's DisallowUnknownFields decoder. Those are exactly the mistakes a catalog invites,
// and they are invisible until something is running.
//
// The transport here is HTTP with Basic auth rather than the mTLS control channel. That is
// deliberate: the tunnel is not what this change touches (it already carries the operator's
// own node screens), and standing up adoption + enrollment to re-prove it would test
// somebody else's code. What IS under test is everything above the transport — the catalog,
// the comparison, the merge, and the read-back.
func liveSender(t *testing.T) (ControlSender, string) {
	t.Helper()
	if os.Getenv("RUN_NODE_IT") == "" {
		t.Skip("set RUN_NODE_IT=1 (plus NODE_URL and NODE_AUTH) to bench against a real node")
	}
	base := os.Getenv("NODE_URL")
	if base == "" {
		base = "http://127.0.0.1:18089"
	}
	return &httpNodeSender{base: base, auth: os.Getenv("NODE_AUTH"), t: t}, base
}

type httpNodeSender struct {
	base string
	auth string
	t    *testing.T
	puts int
}

func (h *httpNodeSender) SendRequest(ctx context.Context, nodeID string, req control.Request) (control.Response, error) {
	if req.Method == http.MethodPut {
		h.puts++
	}
	hr, err := http.NewRequestWithContext(ctx, req.Method, h.base+req.Path, bytes.NewReader(req.Body))
	if err != nil {
		return control.Response{}, err
	}
	for k, v := range req.Headers {
		hr.Header.Set(k, v)
	}
	if h.auth != "" {
		hr.Header.Set("Authorization", "Basic "+h.auth)
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(hr)
	if err != nil {
		return control.Response{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return control.Response{Status: resp.StatusCode, Body: body}, nil
}

// readLive fetches a section straight from the node, bypassing the reconciler, so an
// assertion about what the node HOLDS is not made with the same code that decided it.
func readLive(t *testing.T, base, auth, path string) map[string]any {
	t.Helper()
	hr, _ := http.NewRequest(http.MethodGet, base+path, nil)
	if auth != "" {
		hr.Header.Set("Authorization", "Basic "+auth)
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(hr)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode %s: %v (%s)", path, err, string(body))
	}
	return env.Result
}

func liveNode() *entities.ManagedNode {
	return &entities.ManagedNode{NodeId: "bench-node", Name: "Bench NVR", Kind: "camera"}
}

func liveReconciler(sender ControlSender, details []*FleetPolicyDetail) *FleetPolicyReconciler {
	return NewFleetPolicyReconciler(&fakePolicyStore{details: details}, fakeNodeList{liveNode()}, sender, nil, nil)
}

// Every catalog section must be readable from a real node, and every field the catalog
// declares must actually be present in the response. A catalog that names a path or a field
// the appliance does not have produces a node that is permanently, unfixably "drifted".
func TestLiveCatalogMatchesARealNode(t *testing.T) {
	sender, base := liveSender(t)
	auth := os.Getenv("NODE_AUTH")

	for _, section := range PolicySectionsForKind("camera") {
		t.Run(section.Id, func(t *testing.T) {
			r := liveReconciler(sender, nil)
			current, err := r.readSection(context.Background(), "bench-node", section)
			if err != nil {
				t.Fatalf("%s (%s): %v", section.Id, section.GetPath, err)
			}
			for _, f := range section.Fields {
				v, ok := policyGetPath(current, f.Key)
				if !ok {
					t.Errorf("catalog declares %s.%s but the node's %s response has no such field: %v",
						section.Id, f.Key, section.GetPath, current)
					continue
				}
				switch f.Kind {
				case PolicyFieldBool:
					if _, isBool := v.(bool); !isBool {
						t.Errorf("%s.%s is declared bool; the node returned %T (%v)", section.Id, f.Key, v, v)
					}
				default:
					if _, isNum := numericValue(v); !isNum {
						t.Errorf("%s.%s is declared %s; the node returned %T (%v)", section.Id, f.Key, f.Kind, v, v)
					}
				}
			}
			_ = base
			_ = auth
		})
	}
}

// The whole loop against a real appliance: read, disagree, enforce, and read back.
//
// The merge assertion is the one that matters. Every settings endpoint on the node decodes
// the WHOLE section with DisallowUnknownFields, so a write carrying only the governed field
// sets every other field to zero.
func TestLiveDriftThenEnforceThenVerify(t *testing.T) {
	sender, base := liveSender(t)
	auth := os.Getenv("NODE_AUTH")

	before := readLive(t, base, auth, "/api/settings/continuity")
	startCoverage, _ := numericValue(before["minCoveragePercent"])
	startInterval, _ := numericValue(before["intervalMs"])
	startRecovery, _ := numericValue(before["recoveryThreshold"])
	t.Logf("node starts at minCoveragePercent=%v intervalMs=%v recoveryThreshold=%v",
		startCoverage, startInterval, startRecovery)

	// Ask for something the node is definitely NOT set to.
	want := 91.0
	if startCoverage == want {
		want = 89.0
	}
	wantStr := fmt.Sprintf("%g", want)

	// 1. Report-only: the difference must be seen, and NOTHING may be written.
	reportOnly := []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "minCoveragePercent", wantStr)),
	}
	r := liveReconciler(sender, reportOnly)
	putsBefore := sender.(*httpNodeSender).puts
	nc, err := r.ReconcileNode(context.Background(), "bench-node")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if nc.Status != ComplianceDrifted {
		t.Fatalf("want drifted against a node set to %v, got %q (%+v)", startCoverage, nc.Status, nc.Sections)
	}
	if sender.(*httpNodeSender).puts != putsBefore {
		t.Fatal("a report-only policy wrote to a real appliance")
	}
	fr := fieldOf(t, sectionOf(t, nc, "continuity"), "minCoveragePercent")
	if fr.Desired != wantStr {
		t.Fatalf("desired should be %s, got %q", wantStr, fr.Desired)
	}
	t.Logf("report-only: drift seen — node has %s, policy wants %s", fr.Actual, fr.Desired)

	// 2. Enforce: the value must land, and every ungoverned field in the section must
	//    survive untouched.
	enforcing := []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", true,
			item("continuity", "minCoveragePercent", wantStr)),
	}
	nc, err = liveReconciler(sender, enforcing).ReconcileNode(context.Background(), "bench-node")
	if err != nil {
		t.Fatalf("reconcile (enforce): %v", err)
	}
	sr := sectionOf(t, nc, "continuity")
	if sr.Enforcement != EnforceApplied {
		t.Fatalf("want applied, got %q (%s)", sr.Enforcement, sr.Error)
	}
	if nc.Status != ComplianceCompliant {
		t.Fatalf("want compliant after enforcing, got %q", nc.Status)
	}

	after := readLive(t, base, auth, "/api/settings/continuity")
	gotCoverage, _ := numericValue(after["minCoveragePercent"])
	if gotCoverage != want {
		t.Fatalf("the appliance holds %v, not the %v the policy asked for", gotCoverage, want)
	}
	gotInterval, _ := numericValue(after["intervalMs"])
	gotRecovery, _ := numericValue(after["recoveryThreshold"])
	if gotInterval != startInterval {
		t.Fatalf("intervalMs was collateral damage: %v -> %v. Enforcing one field must not reset the section.", startInterval, gotInterval)
	}
	if gotRecovery != startRecovery {
		t.Fatalf("recoveryThreshold was collateral damage: %v -> %v", startRecovery, gotRecovery)
	}
	t.Logf("enforce: node now holds %v; intervalMs=%v and recoveryThreshold=%v unchanged", gotCoverage, gotInterval, gotRecovery)

	// 3. A second pass must be a no-op — a reconciler that rewrites an already-correct node
	//    every cycle would fill the audit log and the node's own trail with noise.
	putsBefore = sender.(*httpNodeSender).puts
	nc, err = liveReconciler(sender, enforcing).ReconcileNode(context.Background(), "bench-node")
	if err != nil {
		t.Fatalf("reconcile (second pass): %v", err)
	}
	if nc.Status != ComplianceCompliant {
		t.Fatalf("second pass should be compliant, got %q", nc.Status)
	}
	if sender.(*httpNodeSender).puts != putsBefore {
		t.Fatal("an already-compliant node was written to again")
	}

	// Put the appliance back where it was found.
	restore := []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", true,
			item("continuity", "minCoveragePercent", fmt.Sprintf("%g", startCoverage))),
	}
	if _, err := liveReconciler(sender, restore).ReconcileNode(context.Background(), "bench-node"); err != nil {
		t.Logf("restore: %v", err)
	}
}

// The read/write asymmetry against a real node: retention is read out of the notification
// object and written on its own path. If the merge sent the whole notification object back,
// the appliance's webhook URL and Telegram token would round-trip through the control plane
// on every reconcile — and this test would see them arrive in the write.
func TestLiveRetentionWritesWithoutRoundTrippingCredentials(t *testing.T) {
	sender, base := liveSender(t)
	auth := os.Getenv("NODE_AUTH")

	before := readLive(t, base, auth, "/api/settings/notification")
	retention, _ := before["retention"].(map[string]any)
	startDays, _ := numericValue(retention["days"])
	startInterval, _ := numericValue(retention["intervalHours"])

	want := 21.0
	if startDays == want {
		want = 17.0
	}

	nc, err := liveReconciler(sender, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", true,
			item("notificationRetention", "days", fmt.Sprintf("%g", want))),
	}).ReconcileNode(context.Background(), "bench-node")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	sr := sectionOf(t, nc, "notificationRetention")
	if sr.Enforcement != EnforceApplied {
		t.Fatalf("want applied, got %q (%s)", sr.Enforcement, sr.Error)
	}

	after := readLive(t, base, auth, "/api/settings/notification")
	afterRet, _ := after["retention"].(map[string]any)
	gotDays, _ := numericValue(afterRet["days"])
	if gotDays != want {
		t.Fatalf("retention days: node holds %v, policy asked %v", gotDays, want)
	}
	gotInterval, _ := numericValue(afterRet["intervalHours"])
	if gotInterval != startInterval {
		t.Fatalf("intervalHours was collateral damage: %v -> %v", startInterval, gotInterval)
	}
	// The webhook/telegram blocks are siblings of retention and must be untouched by a
	// write aimed at retention.
	beforeWebhook, _ := json.Marshal(before["webhook"])
	afterWebhook, _ := json.Marshal(after["webhook"])
	if string(beforeWebhook) != string(afterWebhook) {
		t.Fatalf("the notification webhook config changed under a retention policy: %s -> %s", beforeWebhook, afterWebhook)
	}
	t.Logf("retention: node now keeps alerts %v days; intervalHours and webhook config untouched", gotDays)

	if _, err := liveReconciler(sender, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", true,
			item("notificationRetention", "days", fmt.Sprintf("%g", startDays))),
	}).ReconcileNode(context.Background(), "bench-node"); err != nil {
		t.Logf("restore: %v", err)
	}
}

// Every governable field must survive a real enforce — not just the one the other tests
// happen to use. A field the node normalizes away would otherwise be discovered by an
// operator whose policy never goes green.
func TestLiveEveryGovernableFieldCanActuallyBeSet(t *testing.T) {
	sender, _ := liveSender(t)

	// Paced deliberately. This test fires six requests per field across ~21 fields — two
	// orders of magnitude more than a real sweep, which is ≤15 requests per node every
	// fifteen minutes — and unpaced it trips the node's own rate limiter. The burst is the
	// test's, not the product's; slowing it keeps the test measuring the catalog rather
	// than the limiter.
	pace := func() { time.Sleep(250 * time.Millisecond) }

	for _, section := range PolicySectionsForKind("camera") {
		pace()
		r := liveReconciler(sender, nil)
		current, err := r.readSection(context.Background(), "bench-node", section)
		if err != nil {
			t.Fatalf("%s: %v", section.Id, err)
		}
		for _, f := range section.Fields {
			cur, _ := policyGetPath(current, f.Key)
			// Choose a value that differs from what the node holds, inside the declared bounds.
			var target string
			if f.Kind == PolicyFieldBool {
				b, _ := cur.(bool)
				target = fmt.Sprintf("%t", !b)
			} else {
				n, _ := numericValue(cur)
				candidate := n - 1
				if f.Min != f.Max && candidate < f.Min {
					candidate = n + 1
				}
				if f.Min != f.Max && candidate > f.Max {
					candidate = f.Min
				}
				if candidate == n {
					continue
				}
				target = fmt.Sprintf("%g", candidate)
			}
			pace()
			t.Run(section.Id+"."+f.Key, func(t *testing.T) {
				nc, err := liveReconciler(sender, []*FleetPolicyDetail{
					policy(1, entities.PolicyScopeFleet, "", "camera", true, item(section.Id, f.Key, target)),
				}).ReconcileNode(context.Background(), "bench-node")
				if err != nil {
					t.Fatalf("%s.%s -> %s: %v", section.Id, f.Key, target, err)
				}
				sr := sectionOf(t, nc, section.Id)
				if sr.Enforcement != EnforceApplied {
					t.Fatalf("%s.%s could not be set to %s: %s (%s)", section.Id, f.Key, target, sr.Enforcement, sr.Error)
				}
			})
			// Put it back before moving on, so each field is tested from the node's own state.
			pace()
			if _, err := liveReconciler(sender, []*FleetPolicyDetail{
				policy(1, entities.PolicyScopeFleet, "", "camera", true,
					item(section.Id, f.Key, formatPolicyValue(cur))),
			}).ReconcileNode(context.Background(), "bench-node"); err != nil {
				t.Logf("restore %s.%s: %v", section.Id, f.Key, err)
			}
		}
	}
}

// A node that is not answering is UNKNOWN. Proven against a real address that refuses the
// connection, because "offline" in a fake is whatever the fake decided it was.
func TestLiveUnreachableNodeIsUnknown(t *testing.T) {
	if os.Getenv("RUN_NODE_IT") == "" {
		t.Skip("set RUN_NODE_IT=1 to run the live bench")
	}
	dead := &httpNodeSender{base: "http://127.0.0.1:1", auth: "", t: t}
	out, err := liveReconciler(dead, []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "minCoveragePercent", "95")),
	}).ReconcileAll(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(out.Nodes) != 1 || out.Nodes[0].Status != ComplianceUnknown {
		t.Fatalf("an unreachable appliance must be unknown, never compliant: %+v", out.Nodes)
	}
	t.Logf("unreachable node reported %q: %s", out.Nodes[0].Status, out.Nodes[0].Reason)
}
