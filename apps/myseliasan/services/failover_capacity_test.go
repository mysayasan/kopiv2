package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

// W3-7 shipped with a stated gap: a drill proved the spare could REACH the staged cameras and
// said nothing about whether it could encode them. These cover the half that was missing.

// capacityOn wires the spare's answer to GET /api/capacity — the appliance's own estimate,
// which is the only place any of these numbers may come from.
func (r *failoverRig) capacityOn(node string, estimatedMax, currentCameras int) {
	r.sender.reply(node+" GET /api/capacity", map[string]any{
		"estimatedMax":     estimatedMax,
		"currentCameras":   currentCameras,
		"confidence":       "estimated",
		"limitingWorkload": "inference",
	})
}

// drillable wires a drill in which every staged camera opens. The point of these tests is a
// plan that passes its drill outright and still must not be called ready.
func (r *failoverRig) drillable(protected string, cameraCount int) {
	cams := []map[string]any{}
	for i := 0; i < cameraCount; i++ {
		cams = append(cams, map[string]any{
			"name": fmt.Sprintf("cam%d", i), "state": "staged", "checkStatus": "ok",
		})
	}
	// The drill path carries the PROTECTED appliance's id, because a spare may hold several
	// camera sets and the request has to say which one to open.
	r.sender.reply("node-b POST /api/standby/"+protected+"/drill", map[string]any{
		"sourceNodeId": protected, "state": "staged", "readiness": "ready",
		"reachable": cameraCount, "total": cameraCount, "cameras": cams,
	})
}

// THE ONE THIS ITEM EXISTS FOR. Every camera reachable, drill passed, and the spare still
// cannot carry them. "Ready" here would be the same lie as calling an untested plan ready,
// arrived at more expensively.
func TestASpareThatCannotCarryTheCamerasIsNotReadyEvenWhenEveryOneIsReachable(t *testing.T) {
	rig := newFailoverRig()
	rig.stageable(30)
	rig.drillable("node-a", 30)
	// The spare estimates 20 cameras and already has 4 of its own.
	rig.capacityOn("node-b", 20, 4)
	plan := rig.plan(t, nil)

	if _, err := rig.svc.Stage(context.Background(), plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	view, err := rig.svc.Drill(context.Background(), plan.Id, 1)
	if err != nil {
		t.Fatalf("drill: %v", err)
	}

	if view.Capacity.State != CapacityOver {
		t.Fatalf("capacity verdict = %q, want %q (max 20, own 4, wanted 30)",
			view.Capacity.State, CapacityOver)
	}
	if view.Ready {
		t.Fatal("a plan whose spare would be over its own estimate was reported READY — a " +
			"drill proves the cameras can be reached, never that they can be carried")
	}
	if view.ReadyState != FailoverReadyOvercommitted {
		t.Fatalf("readyState = %q, want %q — 'partial' would send somebody to a network, "+
			"which is the wrong place", view.ReadyState, FailoverReadyOvercommitted)
	}
	if view.Capacity.Headroom != 20-(4+30) {
		t.Fatalf("headroom = %d, want %d", view.Capacity.Headroom, 20-(4+30))
	}
}

// The spare's OWN cameras count. They do not stop being recorded because somebody else's
// arrived, and a verdict that ignored them would promise a spare could take forty while it
// was already carrying thirty.
func TestTheSparesOwnCamerasAreCountedAgainstIt(t *testing.T) {
	rig := newFailoverRig()
	rig.stageable(10)
	rig.drillable("node-a", 10)
	rig.capacityOn("node-b", 12, 0)
	plan := rig.plan(t, nil)
	if _, err := rig.svc.Stage(context.Background(), plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	view, err := rig.svc.Drill(context.Background(), plan.Id, 1)
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	if view.Capacity.State == CapacityOver {
		t.Fatalf("an empty spare with room for 12 cannot be over on 10: %+v", view.Capacity)
	}

	// Same plan, same spare, but the spare is already carrying eight of its own.
	rig2 := newFailoverRig()
	rig2.stageable(10)
	rig2.drillable("node-a", 10)
	rig2.capacityOn("node-b", 12, 8)
	plan2 := rig2.plan(t, nil)
	if _, err := rig2.svc.Stage(context.Background(), plan2.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	view2, err := rig2.svc.Drill(context.Background(), plan2.Id, 1)
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	if view2.Capacity.State != CapacityOver {
		t.Fatalf("a spare already carrying 8 of 12 cannot also take 10: %+v", view2.Capacity)
	}
	if view2.Capacity.OwnCameras != 8 {
		t.Fatalf("the spare's own cameras were not reported: %+v", view2.Capacity)
	}
}

// A spare may cover several recorders, and each plan on its own can look comfortable while
// the two of them together cannot be carried. The question is never about one plan alone.
func TestCommitmentsFromOtherPlansOnTheSameSpareAreCounted(t *testing.T) {
	rig := newFailoverRig()
	rig.stageable(10)
	rig.drillable("node-a", 10)
	rig.capacityOn("node-b", 25, 0)

	first := rig.plan(t, nil)
	if _, err := rig.svc.Stage(context.Background(), first.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if v, err := rig.svc.Drill(context.Background(), first.Id, 1); err != nil {
		t.Fatalf("drill: %v", err)
	} else if v.Capacity.State == CapacityOver {
		t.Fatalf("10 cameras onto a spare that can carry 25 is not over: %+v", v.Capacity)
	}

	// A SECOND recorder pointed at the same spare, with its own camera set. Saved directly
	// rather than through the rig's helper, which asserts there is exactly one plan.
	secondView, err := rig.svc.Save(context.Background(), SaveFailoverPlanRequest{
		Name: "C covered by B", ProtectedNodeId: "node-c", StandbyNodeId: "node-b",
		Enabled: true, HoldDownSeconds: 300,
	}, 1)
	if err != nil {
		t.Fatalf("save second plan: %v", err)
	}
	second := secondView.Plan
	rig.sender.reply("node-c POST /api/standby/handoff",
		map[string]any{"cameraCount": 20, "sealed": "SEALED-BUNDLE"})
	cams := []map[string]any{}
	for i := 0; i < 20; i++ {
		cams = append(cams, map[string]any{"name": fmt.Sprintf("c%d", i), "state": "staged"})
	}
	rig.sender.reply("node-b POST /api/standby/stage",
		map[string]any{"sourceNodeId": "node-c", "state": "staged", "readiness": "untested", "cameras": cams})
	if _, err := rig.svc.Stage(context.Background(), second.Id, 1); err != nil {
		t.Fatalf("stage second: %v", err)
	}
	rig.sender.reply("node-b POST /api/standby/node-c/drill", map[string]any{
		"sourceNodeId": "node-c", "state": "staged", "readiness": "ready",
		"reachable": 20, "total": 20, "cameras": cams,
	})
	view, err2 := rig.svc.Drill(context.Background(), second.Id, 1)
	if err2 != nil {
		t.Fatalf("drill second: %v", err2)
	}
	if view.Capacity.Committed != 10 {
		t.Fatalf("the first plan's 10 cameras were not counted against the shared spare: %+v",
			view.Capacity)
	}
	if view.Capacity.State != CapacityOver {
		t.Fatalf("10 + 20 onto a spare that can carry 25 must be over: %+v", view.Capacity)
	}
}

// A parked plan commits nothing: it will not take anything over, so counting its cameras
// would refuse capacity to a plan that could have had it.
func TestAParkedPlanDoesNotHoldCapacity(t *testing.T) {
	mine := &entities.FailoverPlan{Id: 1, StandbyNodeId: "node-b", CameraCount: 5, Enabled: true}
	others := []*entities.FailoverPlan{
		{Id: 2, StandbyNodeId: "node-b", CameraCount: 10, Enabled: false}, // parked
		{Id: 3, StandbyNodeId: "node-b", CameraCount: 7, Enabled: true},   // live
		{Id: 4, StandbyNodeId: "node-z", CameraCount: 40, Enabled: true},  // a different spare
		{Id: 1, StandbyNodeId: "node-b", CameraCount: 5, Enabled: true},   // itself
	}
	if got := committedTo(mine, others); got != 7 {
		t.Fatalf("committed = %d, want 7 — a parked plan holds nothing, another spare's plans "+
			"hold nothing here, and a plan never counts itself twice", got)
	}
}

// A spare that will not put a number on it has not answered. Unknown is its own state and
// explicitly not "fine" — the same rule the drill follows for a node it could not reach.
func TestASpareThatCannotAnswerIsUnknownNotFine(t *testing.T) {
	rig := newFailoverRig()
	rig.stageable(10)
	rig.drillable("node-a", 10)
	// No capacity reply wired: the fake sender answers 404, exactly as an older appliance
	// without the endpoint would.
	plan := rig.plan(t, nil)
	if _, err := rig.svc.Stage(context.Background(), plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	view, err := rig.svc.Drill(context.Background(), plan.Id, 1)
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	if view.Capacity.State != CapacityUnknown {
		t.Fatalf("a spare that did not answer reported %q", view.Capacity.State)
	}
	// ...and it must not block a plan that is otherwise proved. An estimate nobody could
	// obtain is not evidence of a problem, and refusing on it would make every appliance
	// that predates this feature un-protectable.
	if !view.Ready {
		t.Fatalf("a fully drilled plan was blocked by a capacity answer nobody could get: %q",
			view.ReadyState)
	}
}

// Fitting with one camera to spare is not the same as fitting. A capacity figure is an
// estimate — the appliance says so itself — and a plan that only just fits stops fitting the
// day somebody adds a camera.
func TestAPlanThatOnlyJustFitsSaysSo(t *testing.T) {
	cases := []struct {
		max, own, committed, wanted int
		want                        string
	}{
		{20, 0, 0, 10, CapacityFits},
		{20, 0, 0, 17, CapacityTight},
		{20, 0, 0, 20, CapacityTight},
		{20, 0, 0, 21, CapacityOver},
		{20, 5, 5, 10, CapacityTight},
		{0, 0, 0, 10, CapacityUnknown},
	}
	for _, tc := range cases {
		got := capacityVerdict(tc.max, tc.own, tc.committed, tc.wanted)
		if got != tc.want {
			t.Fatalf("max=%d own=%d committed=%d wanted=%d -> %q, want %q",
				tc.max, tc.own, tc.committed, tc.wanted, got, tc.want)
		}
	}
}
