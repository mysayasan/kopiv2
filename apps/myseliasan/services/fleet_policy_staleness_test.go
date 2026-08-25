package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

var errNoPolicies = errors.New("the policy table could not be read")

// A compliance report is a snapshot; the policies it was computed from are not. These tests
// cover what happens in the gap — which shipped as a green fleet governed by nothing.

// THE DEFECT. Delete the last policy and every node went on reporting "compliant", on screen
// and from the API, until somebody happened to press Check now. There is nothing left to be
// compliant WITH, and this screen's own hint sentence is that a verdict nobody established is
// not a good one.
func TestDeletingTheLastPolicyStopsTheFleetClaimingCompliance(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 95)
	store := &fakePolicyStore{details: []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "minCoveragePercent", "95")),
	}}
	r := NewFleetPolicyReconciler(store, fakeNodeList{cameraNode()}, node, nil, nil)

	if _, err := r.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.LastFor(context.Background()); got.Counts[ComplianceCompliant] != 1 {
		t.Fatalf("with a policy the node agrees with, it should be compliant: %+v", got.Counts)
	}

	// The operator deletes it. Nothing else happens — no sweep, because a sweep is a
	// tunneled round trip per section per node and this screen deliberately does not run one
	// behind their back.
	store.details = nil

	got := r.LastFor(context.Background())
	if got.Counts[ComplianceCompliant] != 0 {
		t.Fatalf("a fleet with no policy at all is still being called compliant: %+v — a green "+
			"estate governed by nothing is the exact misreading this feature exists to prevent",
			got.Counts)
	}
	if got.Counts[ComplianceUnmanaged] != 1 {
		t.Fatalf("want the node reported as unmanaged, got %+v", got.Counts)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Status != ComplianceUnmanaged {
		t.Fatalf("the node card still shows %+v", got.Nodes)
	}
	if got.Nodes[0].DriftCount != 0 || len(got.Nodes[0].Sections) != 0 {
		t.Fatal("the per-field detail of a comparison against rules that no longer exist was kept")
	}
	// The sweep DID happen, and when is still a fact worth showing.
	if got.CheckedAt == 0 {
		t.Fatal("the time of the last pass was thrown away with its verdicts")
	}
}

// A parked policy governs nothing, so a fleet whose only policy is disabled is exactly as
// unmanaged as one with no policy at all. Anything else means an operator can silence the
// rules and keep the reassurance.
func TestDisablingTheLastPolicyIsTheSameAsNotHavingOne(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 95)
	detail := policy(1, entities.PolicyScopeFleet, "", "camera", false,
		item("continuity", "minCoveragePercent", "95"))
	store := &fakePolicyStore{details: []*FleetPolicyDetail{detail}}
	r := NewFleetPolicyReconciler(store, fakeNodeList{cameraNode()}, node, nil, nil)
	if _, err := r.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	detail.Policy.Enabled = false

	if got := r.LastFor(context.Background()); got.Counts[ComplianceUnmanaged] != 1 {
		t.Fatalf("a fleet whose only policy is parked reported %+v", got.Counts)
	}
}

// The other half: the policies still exist but they have CHANGED. The old verdicts might
// still be right and re-deriving them costs a round trip per section per node, so they are
// kept — and flagged, because a "last checked" time that is perfectly true while the rules
// have moved underneath it is the most misleading thing on the page.
func TestEditingAPolicyMarksTheVerdictsOutOfDate(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 95)
	detail := policy(1, entities.PolicyScopeFleet, "", "camera", false,
		item("continuity", "minCoveragePercent", "95"))
	detail.Policy.UpdatedAt = 1000
	store := &fakePolicyStore{details: []*FleetPolicyDetail{detail}}
	r := NewFleetPolicyReconciler(store, fakeNodeList{cameraNode()}, node, nil, nil)
	if _, err := r.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.LastFor(context.Background()).Stale {
		t.Fatal("a report taken against the current policies was reported as out of date")
	}

	detail.Policy.UpdatedAt = 2000 // the operator saved an edit

	got := r.LastFor(context.Background())
	if !got.Stale {
		t.Fatal("verdicts reached against rules that have since been edited were served as current")
	}
	if got.StaleSince != 2000 {
		t.Fatalf("the screen cannot say what the verdicts predate: staleSince=%d", got.StaleSince)
	}
	// Kept, not blanked: they may well still be right, and blanking them would lose the only
	// information there is until somebody waits for a sweep.
	if len(got.Nodes) != 1 {
		t.Fatalf("the previous verdicts were discarded rather than flagged: %+v", got.Nodes)
	}
}

// Adding a SECOND policy changes what every node is measured against, even though nothing
// about the first one moved. A fingerprint that only watched timestamps would miss it.
func TestAddingAPolicyAlsoMarksTheVerdictsOutOfDate(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 95)
	store := &fakePolicyStore{details: []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "minCoveragePercent", "95")),
	}}
	r := NewFleetPolicyReconciler(store, fakeNodeList{cameraNode()}, node, nil, nil)
	if _, err := r.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	store.details = append(store.details,
		policy(2, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "failureThreshold", "4")))

	if !r.LastFor(context.Background()).Stale {
		t.Fatal("a new policy nobody has swept against was not flagged")
	}
}

// The two changes a lazier fingerprint would miss, and the reason the id and the enabled flag
// are both in it. Neither of these moves a timestamp or changes how many policies exist.
func TestReplacingAndParkingPoliciesAreBothNoticed(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 95)
	keep := policy(1, entities.PolicyScopeFleet, "", "camera", false,
		item("continuity", "minCoveragePercent", "95"))
	swap := policy(2, entities.PolicyScopeFleet, "", "camera", false,
		item("continuity", "failureThreshold", "2"))
	store := &fakePolicyStore{details: []*FleetPolicyDetail{keep, swap}}
	r := NewFleetPolicyReconciler(store, fakeNodeList{cameraNode()}, node, nil, nil)
	if _, err := r.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Delete one policy and create a different one in the same second. Same count, same
	// stamps, completely different rules.
	replaced := policy(3, entities.PolicyScopeFleet, "", "camera", false,
		item("continuity", "failureThreshold", "9"))
	store.details = []*FleetPolicyDetail{keep, replaced}
	if !r.LastFor(context.Background()).Stale {
		t.Fatal("one policy swapped for another with the same timestamp went unnoticed — which " +
			"is why the fingerprint carries the id and not just the stamp")
	}

	// And parking one of two: still two policies, still the same stamps, but the fleet is
	// now measured against half of what it was.
	if _, err := r.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	replaced.Policy.Enabled = false
	if !r.LastFor(context.Background()).Stale {
		t.Fatal("parking one of two policies went unnoticed — which is why the fingerprint " +
			"carries the enabled flag")
	}
}

// A report we cannot validate is not a report to dress up as current.
func TestAnUnreadablePolicyListMakesTheReportDoubtful(t *testing.T) {
	node := newFakeNode()
	continuityOnNode(node, 95)
	store := &fakePolicyStore{details: []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "minCoveragePercent", "95")),
	}}
	r := NewFleetPolicyReconciler(store, fakeNodeList{cameraNode()}, node, nil, nil)
	if _, err := r.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	store.listErr = errNoPolicies

	if !r.LastFor(context.Background()).Stale {
		t.Fatal("a report that could not be checked against anything was served as current")
	}
}
