package services

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/control"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// --- in-memory repos ---------------------------------------------------------------
//
// The rollout is driven entirely off its rows, so the fakes have to behave like a table
// (ids, filters, updates by id) rather than like a list. Getting that wrong would let a
// test pass against a driver that only works because everything is in one slice.

type memRolloutRepo struct {
	dbsql.IGenericRepo[entities.FleetRollout]
	mu   sync.Mutex
	rows []*entities.FleetRollout
	next int64
}

func (r *memRolloutRepo) Get(_ context.Context, _ string, limit, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.FleetRollout, uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*entities.FleetRollout{}
	for _, row := range r.rows {
		if rolloutMatches(row, filters) {
			cp := *row
			out = append(out, &cp)
		}
	}
	// Newest first, matching the DESC sort every caller asks for.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && uint64(len(out)) > limit {
		out = out[:limit]
	}
	return out, uint64(len(out)), nil
}

func rolloutMatches(row *entities.FleetRollout, filters []sqldataenums.Filter) bool {
	for _, f := range filters {
		switch f.FieldName {
		case "Id":
			if row.Id != f.Value.(int64) {
				return false
			}
		case "State":
			states, ok := f.Value.([]string)
			if !ok {
				return false
			}
			found := false
			for _, s := range states {
				if row.State == s {
					found = true
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func (r *memRolloutRepo) Create(_ context.Context, _ string, m entities.FleetRollout) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	m.Id = r.next
	r.rows = append(r.rows, &m)
	return uint64(m.Id), nil
}

func (r *memRolloutRepo) UpdateById(_ context.Context, _ string, m entities.FleetRollout) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, row := range r.rows {
		if row.Id == m.Id {
			cp := m
			r.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, nil
}

type memMemberRepo struct {
	dbsql.IGenericRepo[entities.RolloutNode]
	mu   sync.Mutex
	rows []*entities.RolloutNode
	next int64
}

func (r *memMemberRepo) Get(_ context.Context, _ string, limit, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.RolloutNode, uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*entities.RolloutNode{}
	for _, row := range r.rows {
		keep := true
		for _, f := range filters {
			if f.FieldName == "RolloutId" && row.RolloutId != f.Value.(int64) {
				keep = false
			}
		}
		if keep {
			cp := *row
			out = append(out, &cp)
		}
	}
	if limit > 0 && uint64(len(out)) > limit {
		out = out[:limit]
	}
	return out, uint64(len(out)), nil
}

func (r *memMemberRepo) Create(_ context.Context, _ string, m entities.RolloutNode) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	m.Id = r.next
	r.rows = append(r.rows, &m)
	return uint64(m.Id), nil
}

func (r *memMemberRepo) UpdateById(_ context.Context, _ string, m entities.RolloutNode) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, row := range r.rows {
		if row.Id == m.Id {
			cp := m
			r.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, nil
}

func (r *memMemberRepo) byNode(nodeID string) *entities.RolloutNode {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.NodeId == nodeID {
			cp := *row
			return &cp
		}
	}
	return nil
}

// --- fleet fake ---------------------------------------------------------------------

// rolloutFleet is the node registry plus the version each node currently REPORTS, which
// the test moves by hand to simulate an appliance restarting on a new build.
type rolloutFleet struct {
	mu        sync.Mutex
	nodes     []*entities.ManagedNode
	connected map[string]bool
}

func (f *rolloutFleet) List(context.Context) ([]*entities.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*entities.ManagedNode, 0, len(f.nodes))
	for _, n := range f.nodes {
		cp := *n
		out = append(out, &cp)
	}
	return out, nil
}

func (f *rolloutFleet) setVersion(nodeID, version string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.NodeId == nodeID {
			n.Version = version
		}
	}
}

func (f *rolloutFleet) setConnected(nodeID string, up bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected[nodeID] = up
}

func (f *rolloutFleet) isConnected(nodeID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected[nodeID]
}

// rolloutSender records the update commands the driver sent, and can refuse them.
type rolloutSender struct {
	mu     sync.Mutex
	sent   []string // "nodeID version" — apply commands only, not capability probes
	status int
	body   string
	err    error
	// cannotSelfUpdate / managed script the capability probe per node.
	cannotSelfUpdate map[string]bool
	managed          map[string]string
}

func (s *rolloutSender) SendRequest(_ context.Context, nodeID string, req control.Request) (control.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The capability probe is a GET; only the apply POST is an update command. Counting
	// both would make "how many nodes were asked to update" meaningless, which is the
	// number most of these tests turn on.
	if req.Method == http.MethodGet {
		can := "true"
		if s.cannotSelfUpdate[nodeID] {
			can = "false"
		}
		return control.Response{Status: http.StatusOK,
			Body: []byte(`{"result":{"canSelfUpdate":` + can + `,"managed":"` + s.managed[nodeID] + `"}}`)}, nil
	}
	s.sent = append(s.sent, nodeID+" "+string(req.Body))
	if s.err != nil {
		return control.Response{}, s.err
	}
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	return control.Response{Status: status, Body: []byte(s.body)}, nil
}

func (s *rolloutSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

// --- harness ------------------------------------------------------------------------

type rolloutRig struct {
	svc     *FleetRolloutService
	fleet   *rolloutFleet
	sender  *rolloutSender
	members *memMemberRepo
	clock   int64
}

func newRolloutRig(t *testing.T, versions map[string]string) *rolloutRig {
	t.Helper()
	fleet := &rolloutFleet{connected: map[string]bool{}}
	ids := []string{}
	for id := range versions {
		ids = append(ids, id)
	}
	// Deterministic order: the plan sorts by node id, and so must the fixture.
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	for _, id := range ids {
		fleet.nodes = append(fleet.nodes, &entities.ManagedNode{NodeId: id, Name: strings.ToUpper(id), Kind: "camera", Version: versions[id]})
		fleet.connected[id] = true
	}
	sender := &rolloutSender{cannotSelfUpdate: map[string]bool{}, managed: map[string]string{}}
	members := &memMemberRepo{}
	rig := &rolloutRig{fleet: fleet, sender: sender, members: members, clock: 1_000_000}
	rig.svc = &FleetRolloutService{
		rollouts: &memRolloutRepo{},
		members:  members,
		nodes:    fleet,
		sender:   sender,
		presence: fleet.isConnected,
		logf:     func(string, ...any) {},
		now:      func() time.Time { return time.Unix(rig.clock, 0).UTC() },
	}
	return rig
}

func (r *rolloutRig) advance(t *testing.T) {
	t.Helper()
	if err := r.svc.Advance(context.Background()); err != nil {
		t.Fatalf("advance: %v", err)
	}
}

func (r *rolloutRig) state(t *testing.T, id int64) *RolloutView {
	t.Helper()
	v, err := r.svc.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return v
}

// plan + start, returning the rollout id.
func (r *rolloutRig) begin(t *testing.T, req RolloutPlanRequest) int64 {
	t.Helper()
	view, err := r.svc.Plan(context.Background(), req, 7)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := r.svc.Start(context.Background(), view.Id); err != nil {
		t.Fatalf("start: %v", err)
	}
	return view.Id
}

// --- tests --------------------------------------------------------------------------

// TestRolloutUpdatesOneRingAtATime is the shape of the whole feature: ring 2 must not be
// touched until ring 1 has proved itself. An upgrade that goes out to everything at once
// is the thing this exists to prevent, and it would still pass every other test here.
func TestRolloutUpdatesOneRingAtATime(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0", "c": "1.0.0"})
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1, SettleSeconds: 60})

	view := rig.state(t, id)
	if view.RingCount != 3 || view.CurrentRing != 1 {
		t.Fatalf("rings = %d, current = %d, want 3 / 1", view.RingCount, view.CurrentRing)
	}
	if rig.sender.count() != 1 {
		t.Fatalf("update commands sent = %d, want exactly the canary", rig.sender.count())
	}
	if got := rig.members.byNode("a").State; got != RolloutNodeUpdating {
		t.Fatalf("canary state = %q, want %q", got, RolloutNodeUpdating)
	}
	if got := rig.members.byNode("b").State; got != RolloutNodePending {
		t.Fatalf("ring-2 node was touched during ring 1 (state %q)", got)
	}

	// The canary comes back on the new version, but the settle window has not elapsed.
	rig.fleet.setVersion("a", "1.1.0")
	rig.advance(t)
	if rig.sender.count() != 1 {
		t.Fatal("ring 2 started before the settle window elapsed")
	}

	rig.clock += 61
	rig.advance(t)
	if rig.state(t, id).CurrentRing != 2 {
		t.Fatalf("current ring = %d, want 2 after the canary settled", rig.state(t, id).CurrentRing)
	}
	if rig.sender.count() != 2 {
		t.Fatalf("update commands = %d, want the canary plus one ring-2 node", rig.sender.count())
	}
}

// TestRolloutPinsTheTargetVersionInTheCommand. A node that resolved "latest" for itself
// would defeat the entire point of a canary, so the version must be in the command.
func TestRolloutPinsTheTargetVersionInTheCommand(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0"})
	rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0"})
	rig.sender.mu.Lock()
	defer rig.sender.mu.Unlock()
	if len(rig.sender.sent) != 1 || !strings.Contains(rig.sender.sent[0], `"version":"1.1.0"`) {
		t.Fatalf("sent %q, want the pinned target version in the body", rig.sender.sent)
	}
}

// TestRolloutHaltsWhenANodeComesBackOnTheOLDVersion. This is the failure a naive
// implementation cannot see: the command succeeded, the node is online and healthy, and the
// swap silently did not take. Trusting the 200 would call this a success and roll the bad
// build to the whole estate.
func TestRolloutHaltsWhenANodeComesBackOnTheOLDVersion(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1, NodeTimeoutSeconds: 300})

	// It never reports the new version; the timeout expires.
	rig.clock += 301
	rig.advance(t)

	view := rig.state(t, id)
	if view.State != RolloutStateHalted {
		t.Fatalf("state = %q, want halted", view.State)
	}
	if !strings.Contains(view.HaltReason, "A") {
		t.Fatalf("halt reason %q does not name the node that failed", view.HaltReason)
	}
	if got := rig.members.byNode("a").Detail; !strings.Contains(got, "1.0.0") || !strings.Contains(got, "1.1.0") {
		t.Fatalf("failure detail = %q, want it to say what it came back on and what was wanted", got)
	}
	if got := rig.members.byNode("b").State; got != RolloutNodePending {
		t.Fatalf("ring 2 was updated after ring 1 failed (state %q)", got)
	}
	if rig.sender.count() != 1 {
		t.Fatalf("update commands = %d — a halted rollout must not touch another node", rig.sender.count())
	}
}

// TestRolloutHaltsWhenANodeDiesDuringTheSettleWindow. The window exists for the upgrade
// that boots, looks perfect, and falls over a minute later.
func TestRolloutHaltsWhenANodeDiesDuringTheSettleWindow(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1, SettleSeconds: 120})

	rig.fleet.setVersion("a", "1.1.0")
	rig.advance(t) // records success + opens the settle window

	if got := rig.members.byNode("a").State; got != RolloutNodeSucceeded {
		t.Fatalf("state after reporting the target = %q, want succeeded", got)
	}
	rig.clock += 30
	rig.fleet.setConnected("a", false) // it dies mid-window
	rig.advance(t)

	view := rig.state(t, id)
	if view.State != RolloutStateHalted {
		t.Fatalf("state = %q, want halted — a node that dies during settle has not settled", view.State)
	}
	if !strings.Contains(view.HaltReason, "offline") {
		t.Fatalf("halt reason = %q, want it to say the node went offline", view.HaltReason)
	}
}

// TestRolloutHaltsWhenANodeRevertsDuringTheSettleWindow covers the other late failure: a
// node that reports the new version, then a supervisor restarts it onto the old one.
func TestRolloutHaltsWhenANodeRevertsDuringTheSettleWindow(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1, SettleSeconds: 120})

	rig.fleet.setVersion("a", "1.1.0")
	rig.advance(t)
	rig.clock += 30
	rig.fleet.setVersion("a", "1.0.0")
	rig.advance(t)

	if got := rig.state(t, id).State; got != RolloutStateHalted {
		t.Fatalf("state = %q, want halted after a revert", got)
	}
	if got := rig.members.byNode("a").Detail; !strings.Contains(got, "reverted") {
		t.Fatalf("detail = %q, want it to name the revert", got)
	}
}

// TestRolloutHaltsWhenANodeRefusesTheCommand — e.g. the node rejects a downgrade, or has
// no release for its platform. The reason must survive to the operator.
func TestRolloutHaltsWhenANodeRefusesTheCommand(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	rig.sender.status = http.StatusBadRequest
	rig.sender.body = "no release published for version 9.9.9"
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "9.9.9", RingSize: 1})

	view := rig.state(t, id)
	if view.State != RolloutStateHalted {
		t.Fatalf("state = %q, want halted", view.State)
	}
	if !strings.Contains(rig.members.byNode("a").Detail, "no release published") {
		t.Fatalf("detail = %q, want the node's own reason carried through", rig.members.byNode("a").Detail)
	}
}

// TestRolloutSkipsANodeAlreadyOnTheTarget, and records it as skipped rather than
// succeeded — this rollout did not move it, and a report that claims otherwise overstates
// how much of the fleet the canary actually tested.
func TestRolloutSkipsANodeAlreadyOnTheTarget(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.1.0", "b": "1.0.0"})
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1, SettleSeconds: 1})

	if got := rig.members.byNode("a").State; got != RolloutNodeSkipped {
		t.Fatalf("state = %q, want skipped", got)
	}
	if rig.sender.count() != 0 {
		t.Fatal("a node already on the target must not be asked to update")
	}
	rig.clock += 5
	rig.advance(t)
	if rig.state(t, id).CurrentRing != 2 {
		t.Fatal("a skipped ring should still pass and let the next one start")
	}
}

// TestRolloutTreatsUnknownVersionAsUnjudgeable. A node that has never reported its version
// must not be assumed old (and silently upgraded blind) NOR assumed current (and skipped).
// It is asked, and then judged on what it reports.
func TestRolloutTreatsUnknownVersionAsUnjudgeable(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": ""})
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0", NodeTimeoutSeconds: 300})

	if got := rig.members.byNode("a").State; got != RolloutNodeUpdating {
		t.Fatalf("state = %q, want it to be asked rather than skipped", got)
	}
	// Still silent when the clock runs out: that is a failure, not a pass.
	rig.clock += 301
	rig.advance(t)
	if got := rig.state(t, id).State; got != RolloutStateHalted {
		t.Fatalf("state = %q, want halted — a node that never reports cannot be called healthy", got)
	}
}

// TestRolloutCompletesWhenEveryRingPasses.
func TestRolloutCompletesWhenEveryRingPasses(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1, SettleSeconds: 10})

	for _, node := range []string{"a", "b"} {
		rig.fleet.setVersion(node, "1.1.0")
		rig.advance(t)
		rig.clock += 11
		rig.advance(t)
	}
	view := rig.state(t, id)
	if view.State != RolloutStateCompleted {
		t.Fatalf("state = %q, want completed (counts %v)", view.State, view.Counts)
	}
	if view.Counts[RolloutNodeSucceeded] != 2 {
		t.Fatalf("succeeded = %d, want 2", view.Counts[RolloutNodeSucceeded])
	}
	if view.FinishedAt == 0 {
		t.Fatal("a completed rollout should record when it finished")
	}
}

// TestRolloutRefusesASecondConcurrentPlan. Two rollouts driving one fleet would disagree
// about which version each node should be on, and the loser is whichever finished second.
func TestRolloutRefusesASecondConcurrentPlan(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0"})
	rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0"})
	if _, err := rig.svc.Plan(context.Background(), RolloutPlanRequest{TargetVersion: "1.2.0"}, 1); err == nil {
		t.Fatal("a second rollout was planned while one was running")
	}
}

// TestRolloutWaitsForAnOfflineNodeRatherThanFailingItImmediately. A node that happens to be
// disconnected when its turn arrives gets the next tick; only the timeout condemns it.
func TestRolloutWaitsForAnOfflineNodeRatherThanFailingItImmediately(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0"})
	rig.fleet.setConnected("a", false)
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0", NodeTimeoutSeconds: 300})

	if got := rig.members.byNode("a").State; got != RolloutNodePending {
		t.Fatalf("state = %q, want it left pending while offline", got)
	}
	if got := rig.state(t, id).State; got != RolloutStateRunning {
		t.Fatalf("rollout state = %q, want it still running", got)
	}
	// It comes back before the timeout and is then asked.
	rig.fleet.setConnected("a", true)
	rig.advance(t)
	if got := rig.members.byNode("a").State; got != RolloutNodeUpdating {
		t.Fatalf("state = %q, want updating once it reconnected", got)
	}
}

// TestRolloutCancelLeavesUpdatedNodesAlone. There is no undo, and the API must not imply
// one — the honest contract is that cancelling stops further work.
func TestRolloutCancelLeavesUpdatedNodesAlone(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1, SettleSeconds: 10})
	rig.fleet.setVersion("a", "1.1.0")
	rig.advance(t)

	view, err := rig.svc.Cancel(context.Background(), id, "changed my mind")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if view.State != RolloutStateCancelled {
		t.Fatalf("state = %q, want cancelled", view.State)
	}
	if got := rig.members.byNode("a").State; got != RolloutNodeSucceeded {
		t.Fatalf("the already-updated node changed to %q on cancel", got)
	}
	before := rig.sender.count()
	rig.clock += 100
	rig.advance(t)
	if rig.sender.count() != before {
		t.Fatal("a cancelled rollout kept updating nodes")
	}
}

// TestRolloutHoldsTheSettleWindowAcrossRepeatedTicks. The driver runs on a timer, so the
// window has to survive being asked again and again — not merely be checked once. Without
// this, a rollout would walk every ring in a few seconds while still reporting that it
// had settled each one.
func TestRolloutHoldsTheSettleWindowAcrossRepeatedTicks(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	id := rig.begin(t, RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1, SettleSeconds: 300})

	rig.fleet.setVersion("a", "1.1.0")
	rig.advance(t) // succeeds + opens the window

	for i := 0; i < 5; i++ {
		rig.clock += 30 // 150s total: still inside the 300s window
		rig.advance(t)
		if got := rig.state(t, id).CurrentRing; got != 1 {
			t.Fatalf("advanced to ring %d after %ds of a 300s settle window", got, (i+1)*30)
		}
		if rig.sender.count() != 1 {
			t.Fatalf("ring 2 was asked to update %ds into the settle window", (i+1)*30)
		}
	}
	rig.clock += 200
	rig.advance(t)
	if got := rig.state(t, id).CurrentRing; got != 2 {
		t.Fatalf("ring = %d, want 2 once the window elapsed", got)
	}
}

// TestRolloutDoesNotDriveADraftPlan. Planning is not starting: a draft exists so an
// operator can look at the rings before committing, and a driver that worked them anyway
// would upgrade the fleet from a plan nobody approved.
func TestRolloutDoesNotDriveADraftPlan(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	view, err := rig.svc.Plan(context.Background(), RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1}, 1)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for i := 0; i < 3; i++ {
		rig.clock += 600
		rig.advance(t)
	}
	if rig.sender.count() != 0 {
		t.Fatalf("a draft rollout sent %d update commands", rig.sender.count())
	}
	if got := rig.state(t, view.Id).State; got != RolloutStateDraft {
		t.Fatalf("state = %q, want it left in draft", got)
	}
	if got := rig.members.byNode("a").State; got != RolloutNodePending {
		t.Fatalf("node state = %q, want pending", got)
	}
}

// TestRolloutExcludesNodesThatCannotSelfUpdate. A container-image or package-managed node
// can never replace its own binary, so leaving it in a ring guarantees the rollout halts
// on it. The bench found this the expensive way: a canary that failed with "self-update is
// not available for this install type", teaching the operator something the plan could
// have told them before they started.
func TestRolloutExcludesNodesThatCannotSelfUpdate(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0", "c": "1.0.0"})
	rig.sender.cannotSelfUpdate["b"] = true
	rig.sender.managed["b"] = "docker"

	view, err := rig.svc.Plan(context.Background(), RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1}, 1)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if view.RingCount != 2 {
		t.Fatalf("rings = %d, want 2 — the un-updatable node must not get one", view.RingCount)
	}
	blocked := rig.members.byNode("b")
	if blocked.State != RolloutNodeUnsupported || blocked.Ring != 0 {
		t.Fatalf("node b = state %q ring %d, want unsupported and out of the rings", blocked.State, blocked.Ring)
	}
	// It must SAY so, and say what to do instead. A plan that silently covers two nodes out
	// of three reports complete success over a fleet it never touched.
	if !strings.Contains(blocked.Detail, "container image") {
		t.Fatalf("detail = %q, want it to explain how that node is actually upgraded", blocked.Detail)
	}
	if view.Counts[RolloutNodeUnsupported] != 1 {
		t.Fatalf("counts = %v, want the excluded node visible in the summary", view.Counts)
	}
}

// TestRolloutRefusesAPlanWhereNothingCanBeUpdated. A plan that can only halt is not a plan.
func TestRolloutRefusesAPlanWhereNothingCanBeUpdated(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0"})
	rig.sender.cannotSelfUpdate["a"] = true
	rig.sender.managed["a"] = "package"

	_, err := rig.svc.Plan(context.Background(), RolloutPlanRequest{TargetVersion: "1.1.0"}, 1)
	if err == nil {
		t.Fatal("a rollout was planned across a fleet where no node can self-update")
	}
	if !strings.Contains(err.Error(), "package manager") {
		t.Fatalf("error = %q, want it to name how those nodes are upgraded instead", err)
	}
}

// TestRolloutIncludesAnOfflineNodeItCouldNotAsk. Excluding every node that happens to be
// offline while an operator plans would quietly shrink the rollout to whichever appliances
// were awake at that moment — and then report full coverage of the fleet it did reach.
func TestRolloutIncludesAnOfflineNodeItCouldNotAsk(t *testing.T) {
	rig := newRolloutRig(t, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	rig.fleet.setConnected("b", false)
	rig.sender.cannotSelfUpdate["b"] = true // would say no, but cannot be asked

	view, err := rig.svc.Plan(context.Background(), RolloutPlanRequest{TargetVersion: "1.1.0", RingSize: 1}, 1)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if view.RingCount != 2 {
		t.Fatalf("rings = %d, want 2 — an unreachable node is still planned for", view.RingCount)
	}
	if got := rig.members.byNode("b").State; got != RolloutNodePending {
		t.Fatalf("offline node state = %q, want pending", got)
	}
}

// TestNodeErrorLineUnwrapsTheNodeEnvelope. The node answers errors as
// {"statsCode":400,"message":"..."}; putting that raw into a halt reason puts JSON in front
// of whoever is trying to find out why their fleet upgrade stopped.
func TestNodeErrorLineUnwrapsTheNodeEnvelope(t *testing.T) {
	got := nodeErrorLine([]byte(`{"statsCode":400,"message":"no release published for version 9.9.9"}`))
	if got != "no release published for version 9.9.9" {
		t.Fatalf("nodeErrorLine = %q, want just the message", got)
	}
	// A body that is not the envelope is passed through rather than lost.
	if got := nodeErrorLine([]byte("plain text failure")); got != "plain text failure" {
		t.Fatalf("nodeErrorLine = %q, want the plain body preserved", got)
	}
}
