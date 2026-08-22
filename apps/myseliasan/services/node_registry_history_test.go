package services

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/pairing"
)

// signSelfDrop builds the fleet-key HMAC a node sends with its self-drop notice, the
// same way the node does.
func signSelfDrop(t *testing.T, key, nodeID, nonce string, ts int64) string {
	t.Helper()
	return pairing.SignAssertion([]byte(key), nodeID, nonce, strconv.FormatInt(ts, 10))
}

// These tests are about the WIRING, not the arithmetic: the history is only worth
// anything if the paths that actually change a node's status report it. Every one of
// them exercises the registry, not the recorder — a recorder that works perfectly and
// is never called produces an SLA report of a fleet that apparently never changed.

// recordedObserve is one call the registry made into the history.
type recordedObserve struct {
	NodeID string
	State  string
	Reason string
	At     int64
}

type spyHistory struct {
	observed []recordedObserve
	forgot   []string
	sweeps   []int64
	pruned   int
}

func (s *spyHistory) Observe(_ context.Context, nodeID, state, reason string, at int64) error {
	s.observed = append(s.observed, recordedObserve{NodeID: nodeID, State: state, Reason: reason, At: at})
	return nil
}
func (s *spyHistory) Forget(_ context.Context, nodeID string) error {
	s.forgot = append(s.forgot, nodeID)
	return nil
}
func (s *spyHistory) NoteSweep(_ context.Context, now, _ int64) error {
	s.sweeps = append(s.sweeps, now)
	return nil
}
func (s *spyHistory) Prune(context.Context, int64) error { s.pruned++; return nil }
func (s *spyHistory) Events(context.Context, string, int64, int64) ([]*entities.NodeStateEvent, error) {
	return nil, nil
}
func (s *spyHistory) Gaps(context.Context, int64, int64) ([]*entities.FleetMonitorGap, error) {
	return nil, nil
}

func (s *spyHistory) statesFor(nodeID string) []string {
	out := []string{}
	for _, o := range s.observed {
		if o.NodeID == nodeID {
			out = append(out, o.State)
		}
	}
	return out
}

// The heartbeat must report EVERY node it looked at, on every path — including the two
// that deliberately skip the database write (already-lost, and inside the grace window).
// Those are exactly the nodes whose history would otherwise never start: a node that has
// been lost since before this feature existed changes state never again, so if the
// skip path is silent it is invisible to the SLA report forever.
func TestHeartbeatReportsEveryNodeIncludingTheOnesItSkips(t *testing.T) {
	reg, repo := newTestRegistry()
	spy := &spyHistory{}
	reg.SetStateHistory(spy)
	now := time.Now().Unix()

	repo.rows = []*entities.ManagedNode{
		// Long lost: the heartbeat takes the "already lost, skip the write" path.
		{Id: 1, NodeId: "stale", Status: "lost", LastSeenAt: now - 100000},
		// Unpaired itself: skipped at the top of the loop.
		{Id: 2, NodeId: "gone", Status: "self-dropped", LastSeenAt: now - 100000},
		// Seen a moment ago, unreachable now: inside the grace window, no write.
		{Id: 3, NodeId: "blip", Status: "online", LastSeenAt: now},
		// Silent past the grace window: transitions to lost.
		{Id: 4, NodeId: "dying", Status: "online", LastSeenAt: now - 100000},
	}

	reg.Heartbeat(context.Background())

	for _, want := range []struct{ node, state string }{
		{"stale", "lost"}, {"gone", "self-dropped"}, {"blip", "online"}, {"dying", "lost"},
	} {
		got := spy.statesFor(want.node)
		if len(got) != 1 || got[0] != want.state {
			t.Fatalf("node %q reported %v, want exactly [%s]", want.node, got, want.state)
		}
	}
	for _, o := range spy.observed {
		if o.Reason != entities.NodeStateReasonHeartbeat {
			t.Fatalf("node %q reason = %q, want %q", o.NodeID, o.Reason, entities.NodeStateReasonHeartbeat)
		}
		// Every observation must carry a real timestamp no later than this sweep. Which
		// timestamp — the sweep clock, or the moment contact was lost — is the subject
		// of TestHeartbeatDatesAnOutageToWhenContactWasLost below.
		if o.At <= 0 || o.At > now+5 {
			t.Fatalf("node %q reported at %d, want a timestamp at or before the sweep (~%d)", o.NodeID, o.At, now)
		}
	}
}

// An outage is dated to when CONTACT WAS LOST, not to when the sweep gave up waiting.
// The grace window is at least 90 seconds, so stamping the transition with the sweep
// clock discards at least that much from every single outage — an SLA report that
// under-states downtime by a fixed amount per incident, always in the vendor's favour.
func TestHeartbeatDatesAnOutageToWhenContactWasLost(t *testing.T) {
	reg, repo := newTestRegistry()
	spy := &spyHistory{}
	reg.SetStateHistory(spy)
	now := time.Now().Unix()
	lastSeen := now - 100000

	repo.rows = []*entities.ManagedNode{{Id: 1, NodeId: "a", Status: "online", LastSeenAt: lastSeen}}
	reg.Heartbeat(context.Background())

	if len(spy.observed) != 1 || spy.observed[0].State != entities.NodeStateLost {
		t.Fatalf("observed %+v, want one lost transition", spy.observed)
	}
	if spy.observed[0].At != lastSeen {
		t.Fatalf("outage dated %d, want the last contact at %d (the sweep clock is %d)",
			spy.observed[0].At, lastSeen, now)
	}
}

// A node that has never been seen at all must not date its outage to the epoch — that
// would hand it an outage stretching back to 1970 and bury every other figure.
func TestHeartbeatFallsBackToTheSweepClockWithNoLastContact(t *testing.T) {
	reg, repo := newTestRegistry()
	spy := &spyHistory{}
	reg.SetStateHistory(spy)
	now := time.Now().Unix()

	repo.rows = []*entities.ManagedNode{{Id: 1, NodeId: "a", Status: "online", LastSeenAt: 0}}
	reg.Heartbeat(context.Background())

	if len(spy.observed) != 1 || spy.observed[0].At < now-5 {
		t.Fatalf("observed %+v, want the sweep clock (~%d)", spy.observed, now)
	}
}

// The sweep must stamp monitoring coverage BEFORE it reports any node state, or the gap
// closes after this sweep's observations and the first fresh reading is swallowed by
// the period it is supposed to end.
func TestHeartbeatStampsCoverageEverySweep(t *testing.T) {
	reg, repo := newTestRegistry()
	spy := &spyHistory{}
	reg.SetStateHistory(spy)
	repo.rows = []*entities.ManagedNode{{Id: 1, NodeId: "a", Status: "online", LastSeenAt: time.Now().Unix()}}

	reg.Heartbeat(context.Background())
	reg.Heartbeat(context.Background())

	if len(spy.sweeps) != 2 {
		t.Fatalf("sweeps recorded = %d, want 2", len(spy.sweeps))
	}
	if spy.pruned != 2 {
		t.Fatalf("prune calls = %d, want one per sweep (self-throttled inside)", spy.pruned)
	}
}

// A registry with no history wired must behave exactly as it did before this existed.
// The recorder is optional at construction, and the whole point of that is that a
// missing one costs the report, never the fleet.
func TestHeartbeatWithoutHistoryStillReconciles(t *testing.T) {
	reg, repo := newTestRegistry()
	now := time.Now().Unix()
	repo.rows = []*entities.ManagedNode{{Id: 1, NodeId: "a", Status: "online", LastSeenAt: now - 100000}}

	reg.Heartbeat(context.Background())

	if repo.rows[0].Status != "lost" {
		t.Fatalf("status = %q, want lost — reconciliation must not depend on the recorder", repo.rows[0].Status)
	}
}

// A node dialing in on the control channel is the strongest liveness signal there is,
// and it lands between sweeps. Unreported, a recovery is dated to the next heartbeat
// and the outage is over-reported by up to a full interval.
func TestAcceptControlConnReportsTheRecovery(t *testing.T) {
	reg, repo := newTestRegistry()
	spy := &spyHistory{}
	reg.SetStateHistory(spy)
	repo.rows = []*entities.ManagedNode{{Id: 1, NodeId: "a", Status: "lost", LastSeenAt: 1}}

	if _, err := reg.AcceptControlConn(context.Background(), "a"); err != nil {
		t.Fatalf("AcceptControlConn: %v", err)
	}
	got := spy.statesFor("a")
	if len(got) != 1 || got[0] != entities.NodeStateOnline {
		t.Fatalf("reported %v, want [online]", got)
	}
	if spy.observed[0].Reason != entities.NodeStateReasonControlChannel {
		t.Fatalf("reason = %q, want %q", spy.observed[0].Reason, entities.NodeStateReasonControlChannel)
	}
}

// A rejected connection must not be reported as liveness. An unknown or revoked node
// dialing in is precisely the case where a fabricated "online" would be worst.
func TestAcceptControlConnDoesNotReportARejectedNode(t *testing.T) {
	reg, _ := newTestRegistry()
	spy := &spyHistory{}
	reg.SetStateHistory(spy)

	if _, err := reg.AcceptControlConn(context.Background(), "never-adopted"); err == nil {
		t.Fatal("expected an unknown node to be refused")
	}
	if len(spy.observed) != 0 {
		t.Fatalf("a refused connection was reported as state: %+v", spy.observed)
	}
}

// Releasing a node deletes its history. NodeId is stable per appliance, so keeping it
// means re-adopting the same box inherits a "lost" span running straight through the
// interval it was not in the fleet at all.
func TestReleaseForgetsTheNodesHistory(t *testing.T) {
	reg, repo := newTestRegistry()
	spy := &spyHistory{}
	reg.SetStateHistory(spy)
	repo.rows = []*entities.ManagedNode{{Id: 1, NodeId: "a", Status: "online", BaseUrl: "https://127.0.0.1:1"}}

	_ = reg.Release(context.Background(), "a")

	if len(spy.forgot) != 1 || spy.forgot[0] != "a" {
		t.Fatalf("forgot = %v, want [a]", spy.forgot)
	}
}

// A self-drop is not an outage, but it does have to be recorded — otherwise the node's
// last known state stays "online" and it reports perfect uptime after it left.
func TestMarkSelfDroppedIsRecorded(t *testing.T) {
	reg, repo := newTestRegistry()
	spy := &spyHistory{}
	reg.SetStateHistory(spy)
	ctx := context.Background()
	key, err := reg.GenerateFleetKey(ctx)
	if err != nil {
		t.Fatalf("GenerateFleetKey: %v", err)
	}
	repo.rows = []*entities.ManagedNode{{Id: 1, NodeId: "a", Status: "online"}}

	ts := time.Now().Unix()
	assertion := signSelfDrop(t, key, "a", "nonce-1", ts)
	if err := reg.MarkSelfDropped(ctx, "a", "nonce-1", ts, assertion); err != nil {
		t.Fatalf("MarkSelfDropped: %v", err)
	}
	got := spy.statesFor("a")
	if len(got) != 1 || got[0] != entities.NodeStateSelfDropped {
		t.Fatalf("reported %v, want [self-dropped]", got)
	}
}

// An unauthenticated self-drop notice must change nothing, history included — the
// assertion is the only thing standing between a random caller and the ability to
// retire arbitrary nodes from the record.
func TestUnauthenticatedSelfDropIsNotRecorded(t *testing.T) {
	reg, repo := newTestRegistry()
	spy := &spyHistory{}
	reg.SetStateHistory(spy)
	ctx := context.Background()
	if _, err := reg.GenerateFleetKey(ctx); err != nil {
		t.Fatalf("GenerateFleetKey: %v", err)
	}
	repo.rows = []*entities.ManagedNode{{Id: 1, NodeId: "a", Status: "online"}}

	if err := reg.MarkSelfDropped(ctx, "a", "nonce-1", time.Now().Unix(), "not-a-real-assertion"); err == nil {
		t.Fatal("expected a forged self-drop to be refused")
	}
	if len(spy.observed) != 0 {
		t.Fatalf("a forged self-drop reached the history: %+v", spy.observed)
	}
}
