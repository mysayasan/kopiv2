package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

type horizonWarning struct {
	nodeID string
	state  string
	detail string
}

// horizonRig is a fleet whose clock and connectivity the test drives.
type horizonRig struct {
	monitor  *ReplayHorizonMonitor
	fleet    fakeNodeList
	online   map[string]bool
	warnings []horizonWarning
	clock    int64
}

const testWindow = 72 * time.Hour

func newHorizonRig(t *testing.T, nodes ...*entities.ManagedNode) *horizonRig {
	t.Helper()
	rig := &horizonRig{fleet: fakeNodeList(nodes), online: map[string]bool{}, clock: 10_000_000}
	rig.monitor = NewReplayHorizonMonitor(rig.fleet,
		func(id string) bool { return rig.online[id] },
		testWindow,
		func(nodeID, nodeName, state, detail string) {
			rig.warnings = append(rig.warnings, horizonWarning{nodeID: nodeID, state: state, detail: detail})
		})
	rig.monitor.now = func() time.Time { return time.Unix(rig.clock, 0).UTC() }
	return rig
}

func (r *horizonRig) sweep(t *testing.T) ReplayHorizonReport {
	t.Helper()
	report, err := r.monitor.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	return report
}

func (r *horizonRig) state(t *testing.T, nodeID string) NodeReplayHorizon {
	t.Helper()
	for _, row := range r.monitor.Last().Nodes {
		if row.NodeId == nodeID {
			return row
		}
	}
	t.Fatalf("node %q missing from the report", nodeID)
	return NodeReplayHorizon{}
}

func awayNode(id string, lastSeen int64) *entities.ManagedNode {
	return &entities.ManagedNode{NodeId: id, Name: strings.ToUpper(id), Kind: "camera", LastSeenAt: lastSeen}
}

// TestHorizonWarnsBeforeTheWindowLapses is the point of the whole monitor. A warning that
// arrives once the events are already unrecoverable is an obituary, not an alert.
func TestHorizonWarnsBeforeTheWindowLapses(t *testing.T) {
	rig := newHorizonRig(t, awayNode("a", 0))
	window := int64(testWindow / time.Second)

	// Just under two-thirds of the window: nothing yet.
	rig.fleet[0].LastSeenAt = rig.clock - (window*2/3 - 60)
	rig.sweep(t)
	if got := rig.state(t, "a").State; got != ReplayHorizonOk {
		t.Fatalf("state = %q just under the warn point, want ok", got)
	}
	if len(rig.warnings) != 0 {
		t.Fatalf("warned early: %+v", rig.warnings)
	}

	// Past two-thirds: warn, while there is still a third of the window to act in.
	rig.fleet[0].LastSeenAt = rig.clock - (window*2/3 + 60)
	rig.sweep(t)
	if got := rig.state(t, "a").State; got != ReplayHorizonApproaching {
		t.Fatalf("state = %q past the warn point, want approaching", got)
	}
	if len(rig.warnings) != 1 || rig.warnings[0].state != ReplayHorizonApproaching {
		t.Fatalf("warnings = %+v, want one approaching warning", rig.warnings)
	}
	// It must still be recoverable at this point — that is what makes the warning useful.
	if rig.state(t, "a").UnrecoverableBefore != 0 {
		t.Error("an approaching node has lost nothing yet; it must not claim an unrecoverable horizon")
	}
}

// TestHorizonEscalatesWhenTheWindowIsPassed, and says WHEN the loss starts rather than
// leaving the reader to do arithmetic.
func TestHorizonEscalatesWhenTheWindowIsPassed(t *testing.T) {
	window := int64(testWindow / time.Second)
	rig := newHorizonRig(t, awayNode("a", 0))
	rig.fleet[0].LastSeenAt = rig.clock - (window + 3600)
	rig.sweep(t)

	row := rig.state(t, "a")
	if row.State != ReplayHorizonLapsed {
		t.Fatalf("state = %q past the window, want lapsed", row.State)
	}
	if row.UnrecoverableBefore != rig.clock-window {
		t.Fatalf("unrecoverableBefore = %d, want the start of the replay window (%d)",
			row.UnrecoverableBefore, rig.clock-window)
	}
	if len(rig.warnings) != 1 || rig.warnings[0].state != ReplayHorizonLapsed {
		t.Fatalf("warnings = %+v, want one lapsed warning", rig.warnings)
	}
	// The message has to name the moment, not just the duration.
	if !strings.Contains(rig.warnings[0].detail, "can no longer be recovered") {
		t.Fatalf("detail = %q, want it to say the events are gone", rig.warnings[0].detail)
	}
}

// TestHorizonWarnsOncePerTransitionNotPerSweep. The sweep runs every fifteen minutes; a
// node down for a day would otherwise produce ~96 identical warnings, and the escalation to
// lapsed would be lost in its own noise.
func TestHorizonWarnsOncePerTransitionNotPerSweep(t *testing.T) {
	window := int64(testWindow / time.Second)
	rig := newHorizonRig(t, awayNode("a", 0))
	rig.fleet[0].LastSeenAt = rig.clock - (window*2/3 + 60)

	for i := 0; i < 5; i++ {
		rig.sweep(t)
	}
	if len(rig.warnings) != 1 {
		t.Fatalf("warnings = %d across five sweeps, want 1", len(rig.warnings))
	}

	// Crossing into lapsed is a NEW thing to say, and must not be suppressed by the
	// earlier warning.
	rig.fleet[0].LastSeenAt = rig.clock - (window + 60)
	rig.sweep(t)
	rig.sweep(t)
	if len(rig.warnings) != 2 || rig.warnings[1].state != ReplayHorizonLapsed {
		t.Fatalf("warnings = %+v, want the escalation to lapsed exactly once", rig.warnings)
	}
}

// TestHorizonIgnoresAConnectedNode. A node holding a live channel is forwarding events
// right now; there is nothing to recover and nothing to warn about, however stale its
// last-seen stamp happens to look.
func TestHorizonIgnoresAConnectedNode(t *testing.T) {
	window := int64(testWindow / time.Second)
	rig := newHorizonRig(t, awayNode("a", 0))
	rig.fleet[0].LastSeenAt = rig.clock - (window * 2)
	rig.online["a"] = true

	rig.sweep(t)
	if got := rig.state(t, "a").State; got != ReplayHorizonOk {
		t.Fatalf("state = %q for a connected node, want ok", got)
	}
	if len(rig.warnings) != 0 {
		t.Fatalf("warned about a connected node: %+v", rig.warnings)
	}
}

// TestHorizonRearmsAfterRecovery. A node that comes back and later goes away again must be
// warned about afresh — otherwise the second outage is silent, which is worse than the
// first because nobody is expecting it.
func TestHorizonRearmsAfterRecovery(t *testing.T) {
	window := int64(testWindow / time.Second)
	rig := newHorizonRig(t, awayNode("a", 0))
	rig.fleet[0].LastSeenAt = rig.clock - (window*2/3 + 60)
	rig.sweep(t)
	if len(rig.warnings) != 1 {
		t.Fatalf("warnings = %d, want the first one", len(rig.warnings))
	}

	// Back online.
	rig.online["a"] = true
	rig.sweep(t)
	if got := rig.state(t, "a").State; got != ReplayHorizonOk {
		t.Fatalf("state = %q after recovery, want ok", got)
	}

	// Away again, past the warn point.
	rig.online["a"] = false
	rig.clock += 10_000
	rig.fleet[0].LastSeenAt = rig.clock - (window*2/3 + 60)
	rig.sweep(t)
	if len(rig.warnings) != 2 {
		t.Fatalf("warnings = %d, want a fresh warning for the second outage", len(rig.warnings))
	}
}

// TestHorizonLeavesANeverSeenNodeAlone. A node adopted a moment ago and never connected has
// no clock to measure against; crying "unrecoverable" over events it never raised would be
// noise, and it is the liveness monitor's business anyway.
func TestHorizonLeavesANeverSeenNodeAlone(t *testing.T) {
	rig := newHorizonRig(t, awayNode("fresh", 0))
	rig.sweep(t)
	if got := rig.state(t, "fresh").State; got != ReplayHorizonOk {
		t.Fatalf("state = %q for a never-seen node, want ok", got)
	}
	if len(rig.warnings) != 0 {
		t.Fatalf("warned about a node that has never connected: %+v", rig.warnings)
	}
}

// TestHorizonCountsTheFleet, so a header can say "2 approaching, 1 lapsed" without walking
// the list.
func TestHorizonCountsTheFleet(t *testing.T) {
	window := int64(testWindow / time.Second)
	rig := newHorizonRig(t, awayNode("ok", 0), awayNode("near", 0), awayNode("gone", 0))
	rig.online["ok"] = true
	rig.fleet[1].LastSeenAt = rig.clock - (window*2/3 + 60)
	rig.fleet[2].LastSeenAt = rig.clock - (window + 60)

	report := rig.sweep(t)
	if report.Approaching != 1 || report.Lapsed != 1 {
		t.Fatalf("counts approaching=%d lapsed=%d, want 1/1", report.Approaching, report.Lapsed)
	}
	if len(report.Nodes) != 3 {
		t.Fatalf("nodes = %d, want every node listed including the healthy one", len(report.Nodes))
	}
	// Every row says which window it was judged against, so a reader never has to guess
	// which number produced the verdict.
	for _, row := range report.Nodes {
		if row.WindowSeconds != window {
			t.Fatalf("node %s reports window %d, want %d", row.NodeId, row.WindowSeconds, window)
		}
	}
}
