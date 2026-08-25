package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// --- fakes ---------------------------------------------------------------------------

type fakePlanRepo struct {
	dbsql.IGenericRepo[entities.FailoverPlan]
	rows []*entities.FailoverPlan
	seq  int64
}

func (f *fakePlanRepo) Get(_ context.Context, _ string, _, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.FailoverPlan, uint64, error) {
	out := []*entities.FailoverPlan{}
	for _, row := range f.rows {
		cp := *row
		out = append(out, &cp)
	}
	return out, uint64(len(out)), nil
}

func (f *fakePlanRepo) GetById(_ context.Context, _ string, id uint64) (*entities.FailoverPlan, error) {
	for _, row := range f.rows {
		if row.Id == int64(id) {
			cp := *row
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakePlanRepo) Create(_ context.Context, _ string, model entities.FailoverPlan) (uint64, error) {
	f.seq++
	model.Id = f.seq
	f.rows = append(f.rows, &model)
	return uint64(model.Id), nil
}

func (f *fakePlanRepo) UpdateById(_ context.Context, _ string, model entities.FailoverPlan) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

func (f *fakePlanRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakePlanRepo) only(t *testing.T) *entities.FailoverPlan {
	t.Helper()
	if len(f.rows) != 1 {
		t.Fatalf("expected exactly one plan, got %d", len(f.rows))
	}
	return f.rows[0]
}

type fakeNodeSource struct{ nodes []*entities.ManagedNode }

func (f *fakeNodeSource) List(context.Context) ([]*entities.ManagedNode, error) { return f.nodes, nil }

func (f *fakeNodeSource) node(id string) *entities.ManagedNode {
	for _, n := range f.nodes {
		if n.NodeId == id {
			return n
		}
	}
	return nil
}

// fakeFailoverSender is the fleet tunnel. It records every call in order — the ORDER is part of
// what is being tested, because sealing a camera set to a key means fetching the key first.
type fakeFailoverSender struct {
	mu    sync.Mutex
	calls []string
	// replies maps "<nodeId> <METHOD> <path>" to a canned node response.
	replies map[string]control.Response
	// fail maps the same key to a transport error.
	fail map[string]error
	// bodies records what was sent, so a test can assert the spare was sealed the bundle
	// the protected appliance produced rather than something invented in between.
	bodies map[string][]byte
}

func newFakeFailoverSender() *fakeFailoverSender {
	return &fakeFailoverSender{replies: map[string]control.Response{}, fail: map[string]error{}, bodies: map[string][]byte{}}
}

func (f *fakeFailoverSender) SendRequest(_ context.Context, nodeID string, req control.Request) (control.Response, error) {
	key := nodeID + " " + req.Method + " " + req.Path
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, key)
	f.bodies[key] = req.Body
	if err, ok := f.fail[key]; ok {
		return control.Response{}, err
	}
	if resp, ok := f.replies[key]; ok {
		return resp, nil
	}
	return control.Response{Status: 404, Body: []byte(`{"message":"not found"}`)}, nil
}

func (f *fakeFailoverSender) reply(key string, payload any) {
	body, _ := json.Marshal(map[string]any{"message": "succeed", "result": payload})
	f.replies[key] = control.Response{Status: http.StatusOK, Body: body}
}

func (f *fakeFailoverSender) called(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == key {
			return true
		}
	}
	return false
}

type recordedNotification struct {
	kind      string
	protected string
	standby   string
	detail    string
}

type failoverRig struct {
	svc    IFailoverService
	repo   *fakePlanRepo
	nodes  *fakeNodeSource
	sender *fakeFailoverSender
	notes  []recordedNotification
}

func newFailoverRig() *failoverRig {
	rig := &failoverRig{
		repo:   &fakePlanRepo{},
		sender: newFakeFailoverSender(),
		nodes: &fakeNodeSource{nodes: []*entities.ManagedNode{
			{NodeId: "node-a", Name: "Site A", Kind: "camera", Status: "online", LastSeenAt: time.Now().Unix()},
			{NodeId: "node-b", Name: "Spare", Kind: "camera", Status: "online", LastSeenAt: time.Now().Unix()},
			{NodeId: "node-c", Name: "Site C", Kind: "camera", Status: "online", LastSeenAt: time.Now().Unix()},
			{NodeId: "door-1", Name: "Front door", Kind: "door", Status: "online", LastSeenAt: time.Now().Unix()},
		}},
	}
	rig.svc = newFailoverServiceWith(rig.repo, rig.nodes, rig.sender, nil,
		func(kind string, _ *entities.FailoverPlan, protectedName, standbyName, detail string) {
			rig.notes = append(rig.notes, recordedNotification{kind, protectedName, standbyName, detail})
		}, nil)
	return rig
}

// stageable wires the three canned replies a successful staging needs.
func (r *failoverRig) stageable(cameraCount int) {
	r.sender.reply("node-b GET /api/standby/handoff-key",
		map[string]any{"nodeId": "node-b", "publicKey": "AAAA"})
	r.sender.reply("node-a POST /api/standby/handoff",
		map[string]any{"cameraCount": cameraCount, "sealed": "SEALED-BUNDLE"})
	cams := []map[string]any{}
	for i := 0; i < cameraCount; i++ {
		cams = append(cams, map[string]any{"name": fmt.Sprintf("cam%d", i), "state": "staged"})
	}
	r.sender.reply("node-b POST /api/standby/stage",
		map[string]any{"sourceNodeId": "node-a", "state": "staged", "readiness": "untested", "cameras": cams})
}

func (r *failoverRig) plan(t *testing.T, mutate func(*SaveFailoverPlanRequest)) *entities.FailoverPlan {
	t.Helper()
	req := SaveFailoverPlanRequest{
		Name: "A covered by B", ProtectedNodeId: "node-a", StandbyNodeId: "node-b",
		Enabled: true, HoldDownSeconds: 300,
	}
	if mutate != nil {
		mutate(&req)
	}
	if _, err := r.svc.Save(context.Background(), req, 1); err != nil {
		t.Fatalf("save plan: %v", err)
	}
	return r.repo.only(t)
}

func (r *failoverRig) noted(kind string) bool {
	for _, n := range r.notes {
		if n.kind == kind {
			return true
		}
	}
	return false
}

func (r *failoverRig) countNoted(kind string) int {
	n := 0
	for _, note := range r.notes {
		if note.kind == kind {
			n++
		}
	}
	return n
}

// --- tests ---------------------------------------------------------------------------

func TestFailoverSaveRefusesNonsensePairings(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	base := SaveFailoverPlanRequest{ProtectedNodeId: "node-a", StandbyNodeId: "node-b", Enabled: true, HoldDownSeconds: 300}

	cases := map[string]func(*SaveFailoverPlanRequest){
		"an appliance standing by for itself": func(r *SaveFailoverPlanRequest) { r.StandbyNodeId = "node-a" },
		"a spare that is not in the fleet":    func(r *SaveFailoverPlanRequest) { r.StandbyNodeId = "node-z" },
		"a door controller as the spare":      func(r *SaveFailoverPlanRequest) { r.StandbyNodeId = "door-1" },
		"a door controller as the protected":  func(r *SaveFailoverPlanRequest) { r.ProtectedNodeId = "door-1" },
		// A hold-down shorter than the liveness grace window is a number that cannot be
		// honoured: the node is not declared lost for at least 90 seconds, so a plan
		// promising to act after 10 would be lying on the screen that displays it.
		"a hold-down shorter than the grace window": func(r *SaveFailoverPlanRequest) { r.HoldDownSeconds = 10 },
	}
	for name, mutate := range cases {
		req := base
		mutate(&req)
		if _, err := rig.svc.Save(ctx, req, 1); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if len(rig.repo.rows) != 0 {
		t.Fatalf("a refused plan was still written (%d rows)", len(rig.repo.rows))
	}
}

// Failover must not chain. A fails to B, B fails to C, and a site's cameras end up on an
// appliance two hops from anybody who knows about them.
func TestFailoverSaveRefusesChains(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	rig.plan(t, nil) // node-a protected by node-b

	if _, err := rig.svc.Save(ctx, SaveFailoverPlanRequest{
		ProtectedNodeId: "node-b", StandbyNodeId: "node-c", Enabled: true, HoldDownSeconds: 300,
	}, 1); err == nil {
		t.Fatal("a spare was itself given a protector")
	}
	if _, err := rig.svc.Save(ctx, SaveFailoverPlanRequest{
		ProtectedNodeId: "node-c", StandbyNodeId: "node-a", Enabled: true, HoldDownSeconds: 300,
	}, 1); err == nil {
		t.Fatal("a protected appliance was made somebody's spare")
	}
	if _, err := rig.svc.Save(ctx, SaveFailoverPlanRequest{
		ProtectedNodeId: "node-a", StandbyNodeId: "node-c", Enabled: true, HoldDownSeconds: 300,
	}, 1); err == nil {
		t.Fatal("one appliance was protected by two plans")
	}
}

// The three-step exchange, in order. The middle step is the one this service cannot read,
// and the assertion that matters is that what reaches the spare is exactly what the
// protected appliance produced.
func TestFailoverStageRelaysTheSealedBundleUnchanged(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	rig.stageable(3)

	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	want := []string{
		"node-b GET /api/standby/handoff-key",
		"node-a POST /api/standby/handoff",
		"node-b POST /api/standby/stage",
	}
	if len(rig.sender.calls) < 3 || strings.Join(rig.sender.calls[:3], "|") != strings.Join(want, "|") {
		t.Fatalf("staging made the calls %v, expected %v", rig.sender.calls, want)
	}
	// The spare must be sealed to its OWN key: the recipient named in the handoff request
	// is what binds the bundle, and getting it wrong would seal a site's credentials to
	// somebody else while every step still returned 200.
	var handoffReq map[string]string
	_ = json.Unmarshal(rig.sender.bodies["node-a POST /api/standby/handoff"], &handoffReq)
	if handoffReq["recipientNodeId"] != "node-b" || handoffReq["publicKey"] != "AAAA" {
		t.Fatalf("the protected appliance was asked to seal for %+v", handoffReq)
	}
	var stageReq map[string]string
	_ = json.Unmarshal(rig.sender.bodies["node-b POST /api/standby/stage"], &stageReq)
	if stageReq["sealed"] != "SEALED-BUNDLE" {
		t.Fatalf("what reached the spare was %q, not what the protected appliance sealed", stageReq["sealed"])
	}

	saved := rig.repo.only(t)
	if saved.State != entities.FailoverStateStaged || saved.CameraCount != 3 || saved.LastStagedAt == 0 {
		t.Fatalf("after staging the plan reads %+v", saved)
	}
}

// If the appliance that answers is not the one addressed, the exchange must stop. Every
// later step would succeed, and a site's camera credentials would have been sealed to a
// machine nobody chose.
func TestFailoverStageRefusesAnUnexpectedRespondent(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	rig.stageable(2)
	rig.sender.reply("node-b GET /api/standby/handoff-key",
		map[string]any{"nodeId": "node-x", "publicKey": "AAAA"})

	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err == nil {
		t.Fatal("a camera set was sealed for an appliance that was not the one addressed")
	}
	if rig.sender.called("node-a POST /api/standby/handoff") {
		t.Fatal("the protected appliance was asked to seal its cameras anyway")
	}
}

// THE assertion of the whole feature. A successful copy proves the two appliances can talk
// to each other. It says nothing about whether the spare can reach the CAMERAS — a
// different network path, different credentials, and the thing that actually fails. Only a
// drill answers that.
func TestFailoverStagedIsNotReady(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	rig.stageable(4)
	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}

	views, err := rig.svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if views[0].Ready {
		t.Fatal("a plan that has never been drilled reported itself ready")
	}
	if views[0].ReadyState != FailoverReadyUntested {
		t.Fatalf("a staged, never-drilled plan reported %q", views[0].ReadyState)
	}

	// Now drill it, and only now may it be ready.
	rig.sender.reply("node-b POST /api/standby/node-a/drill",
		map[string]any{"sourceNodeId": "node-a", "readiness": "ready", "reachable": 4, "total": 4})
	if _, err := rig.svc.Drill(ctx, plan.Id, 1); err != nil {
		t.Fatalf("drill: %v", err)
	}
	views, _ = rig.svc.List(ctx)
	if !views[0].Ready || views[0].ReadyState != FailoverReadyReady {
		t.Fatalf("a drilled, fully reachable plan reported ready=%v state=%q", views[0].Ready, views[0].ReadyState)
	}
}

// A partial drill is not readiness, and the state distinguishes "some cameras" from "no
// cameras" because they send somebody to different places.
func TestFailoverDrillOutcomesMapToDistinctStates(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		readiness string
		reachable int
		want      string
		ready     bool
	}{
		{"ready", 4, FailoverReadyReady, true},
		{"partial", 2, FailoverReadyPartial, false},
		{"blind", 0, FailoverReadyBlind, false},
	} {
		rig := newFailoverRig()
		plan := rig.plan(t, nil)
		rig.stageable(4)
		if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
			t.Fatalf("stage: %v", err)
		}
		rig.sender.reply("node-b POST /api/standby/node-a/drill",
			map[string]any{"sourceNodeId": "node-a", "readiness": tc.readiness, "reachable": tc.reachable, "total": 4})
		if _, err := rig.svc.Drill(ctx, plan.Id, 1); err != nil {
			t.Fatalf("drill: %v", err)
		}
		views, _ := rig.svc.List(ctx)
		if views[0].ReadyState != tc.want || views[0].Ready != tc.ready {
			t.Fatalf("a %q drill reported state %q ready=%v", tc.readiness, views[0].ReadyState, views[0].Ready)
		}
	}
}

// A spare that is itself off the network cannot take anything over, whatever its last drill
// said. That fact outranks the drill result, because it makes the drill result stale.
func TestFailoverStandbyDownOutranksAPassedDrill(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	rig.stageable(2)
	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	rig.sender.reply("node-b POST /api/standby/node-a/drill",
		map[string]any{"sourceNodeId": "node-a", "readiness": "ready", "reachable": 2, "total": 2})
	if _, err := rig.svc.Drill(ctx, plan.Id, 1); err != nil {
		t.Fatalf("drill: %v", err)
	}

	rig.nodes.node("node-b").Status = "lost"
	views, _ := rig.svc.List(ctx)
	if views[0].Ready || views[0].ReadyState != FailoverReadyStandbyDown {
		t.Fatalf("with the spare off the network the plan reported ready=%v state=%q",
			views[0].Ready, views[0].ReadyState)
	}
}

// Pressing activate on a plan that was never staged must fail LOUDLY and immediately.
// Discovering in an emergency that nothing was ever copied is the worst possible moment.
func TestFailoverActivateRefusesWhenNothingWasStaged(t *testing.T) {
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	if _, err := rig.svc.Activate(context.Background(), plan.Id, 1, false); err == nil {
		t.Fatal("a plan with nothing staged was activated")
	}
	if rig.sender.called("node-b POST /api/standby/node-a/activate") {
		t.Fatal("the spare was told to take over cameras it was never given")
	}
}

func TestFailoverSweepWaitsOutTheHoldDown(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, func(r *SaveFailoverPlanRequest) { r.HoldDownSeconds = 300 })
	rig.stageable(2)
	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}

	node := rig.nodes.node("node-a")
	node.Status = "lost"
	node.LastSeenAt = time.Now().Unix() - 200 // inside the hold-down

	rig.svc.Sweep(ctx)
	if rig.noted(FailoverNotifyReadyToActivate) {
		t.Fatal("a recorder inside its hold-down already raised the failover alarm")
	}

	node.LastSeenAt = time.Now().Unix() - 400 // past it
	rig.svc.Sweep(ctx)
	if !rig.noted(FailoverNotifyReadyToActivate) {
		t.Fatal("a recorder past its hold-down raised nothing")
	}
	if rig.sender.called("node-b POST /api/standby/node-a/activate") {
		t.Fatal("a plan without automatic takeover took the cameras over by itself")
	}

	// Edge-triggered: the sweep runs every half minute, and a recorder that is down over a
	// weekend must not fill the feed that carries the alarm.
	rig.svc.Sweep(ctx)
	rig.svc.Sweep(ctx)
	if n := rig.countNoted(FailoverNotifyReadyToActivate); n != 1 {
		t.Fatalf("the same outage raised %d alarms", n)
	}
}

func TestFailoverSweepActivatesWhenArmed(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, func(r *SaveFailoverPlanRequest) { r.AutoActivate = true })
	rig.stageable(2)
	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	rig.sender.reply("node-b POST /api/standby/node-a/activate", map[string]any{
		"sourceNodeId": "node-a", "state": "active", "readiness": "active",
		"cameras": []map[string]any{
			{"name": "cam0", "state": "active", "outcome": "recording"},
			{"name": "cam1", "state": "active", "outcome": "recording"},
		},
	})

	node := rig.nodes.node("node-a")
	node.Status = "lost"
	node.LastSeenAt = time.Now().Unix() - 400

	rig.svc.Sweep(ctx)

	if !rig.sender.called("node-b POST /api/standby/node-a/activate") {
		t.Fatal("an armed plan did not take the cameras over")
	}
	saved := rig.repo.only(t)
	if saved.State != entities.FailoverStateActive || !saved.ActivatedAutomatically {
		t.Fatalf("after an automatic takeover the plan reads %+v", saved)
	}
	if !rig.noted(FailoverNotifyActivated) {
		t.Fatal("an automatic takeover told nobody")
	}
}

// An armed plan whose spare holds nothing must NOT silently do nothing. This is the case
// where the promise cannot be kept, and it has to reach a human.
func TestFailoverSweepReportsAnUnkeepablePromise(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	rig.plan(t, func(r *SaveFailoverPlanRequest) { r.AutoActivate = true })

	node := rig.nodes.node("node-a")
	node.Status = "lost"
	node.LastSeenAt = time.Now().Unix() - 400

	rig.svc.Sweep(ctx)

	if !rig.noted(FailoverNotifyActivateFailed) {
		t.Fatal("a recorder went down under a plan that had staged nothing, and nobody was told")
	}
	if rig.sender.called("node-b POST /api/standby/node-a/activate") {
		t.Fatal("the spare was told to take over a set it never received")
	}
}

// The returning appliance is NEVER stopped and the cameras are NEVER handed back on their
// own. Both are deliberate: the control plane cannot tell a dead recorder from a partitioned
// one, and an appliance that returns for thirty seconds would otherwise thrash the building.
func TestFailoverProtectedComingBackNotifiesButChangesNothing(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	rig.stageable(2)
	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	rig.sender.reply("node-b POST /api/standby/node-a/activate", map[string]any{
		"sourceNodeId": "node-a", "state": "active",
		"cameras": []map[string]any{{"name": "cam0", "outcome": "recording"}},
	})
	if _, err := rig.svc.Activate(ctx, plan.Id, 1, false); err != nil {
		t.Fatalf("activate: %v", err)
	}

	rig.svc.Sweep(ctx) // node-a is online again in the fixture
	if !rig.noted(FailoverNotifyProtectedBack) {
		t.Fatal("the protected appliance came back while the spare was recording and nobody was told")
	}
	if rig.sender.called("node-b POST /api/standby/node-a/release") {
		t.Fatal("the cameras were handed back automatically")
	}
	if rig.repo.only(t).State != entities.FailoverStateActive {
		t.Fatal("the plan left the active state without anybody deciding to")
	}
	rig.svc.Sweep(ctx)
	if n := rig.countNoted(FailoverNotifyProtectedBack); n != 1 {
		t.Fatalf("one return produced %d notifications", n)
	}
}

func TestFailoverDeleteRefusesWhileActive(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	rig.stageable(1)
	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	rig.sender.reply("node-b POST /api/standby/node-a/activate", map[string]any{
		"sourceNodeId": "node-a", "state": "active",
		"cameras": []map[string]any{{"name": "cam0", "outcome": "recording"}},
	})
	if _, err := rig.svc.Activate(ctx, plan.Id, 1, false); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := rig.svc.Delete(ctx, plan.Id, 1); err == nil {
		t.Fatal("a plan carrying a building's cameras was deleted")
	}
}

// Deleting a plan must also tell the spare to drop the copy. Otherwise one appliance keeps
// another site's camera credentials for a plan that no longer exists, and nothing anywhere
// says why it has them.
func TestFailoverDeleteTellsTheSpareToForget(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	rig.sender.reply("node-b POST /api/standby/node-a/forget", map[string]any{"sourceNodeId": "node-a"})

	if err := rig.svc.Delete(ctx, plan.Id, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !rig.sender.called("node-b POST /api/standby/node-a/forget") {
		t.Fatal("the spare was left holding the camera set of a deleted plan")
	}
}

// Repointing a plan at a different spare must not carry the old spare's drill result over.
// That green tick was earned by a different machine.
func TestFailoverRepointingClearsTheDrillResult(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	rig.stageable(2)
	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	rig.sender.reply("node-b POST /api/standby/node-a/drill",
		map[string]any{"sourceNodeId": "node-a", "readiness": "ready", "reachable": 2, "total": 2})
	if _, err := rig.svc.Drill(ctx, plan.Id, 1); err != nil {
		t.Fatalf("drill: %v", err)
	}

	if _, err := rig.svc.Save(ctx, SaveFailoverPlanRequest{
		Id: plan.Id, ProtectedNodeId: "node-a", StandbyNodeId: "node-c",
		Enabled: true, HoldDownSeconds: 300,
	}, 1); err != nil {
		t.Fatalf("repoint: %v", err)
	}
	saved := rig.repo.only(t)
	if saved.LastDrillAt != 0 || saved.DrillReadiness != "" || saved.LastStagedAt != 0 {
		t.Fatalf("repointing kept the old spare's results: %+v", saved)
	}
	views, _ := rig.svc.List(ctx)
	if views[0].Ready {
		t.Fatal("a plan repointed at an untested spare still reported ready")
	}
}

// A disabled plan is inert: it is not staged, not drilled, and never acts. Parking one is
// how a spare is taken out for service without losing the record that it was covering a site.
func TestFailoverDisabledPlanIsInert(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	rig.plan(t, func(r *SaveFailoverPlanRequest) { r.Enabled = false; r.AutoActivate = true })
	rig.stageable(2)

	node := rig.nodes.node("node-a")
	node.Status = "lost"
	node.LastSeenAt = time.Now().Unix() - 4000

	rig.svc.Sweep(ctx)
	if len(rig.sender.calls) != 0 {
		t.Fatalf("a disabled plan made %v", rig.sender.calls)
	}
	if len(rig.notes) != 0 {
		t.Fatalf("a disabled plan raised %d notification(s)", len(rig.notes))
	}
	views, _ := rig.svc.List(ctx)
	if views[0].ReadyState != FailoverReadyDisabled {
		t.Fatalf("a disabled plan reported %q", views[0].ReadyState)
	}
}

// THE DEFECT THE LIVE BENCH FOUND, kept as a unit test.
//
// The appliance computes a per-camera outcome while taking over — this one is recording,
// that one could not be created — and does NOT store it, because it is a result rather than
// a state. Rebuilding the view from the database afterwards therefore dropped it, and the
// operator who had just pressed the button in an emergency got a plan that said "active"
// and nothing about which of their cameras was actually being recorded. Every status code
// on that path was 200; the audit trail even had the outcomes in it.
func TestFailoverActivateReturnsThePerCameraOutcome(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	rig.stageable(2)
	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	rig.sender.reply("node-b POST /api/standby/node-a/activate", map[string]any{
		"sourceNodeId": "node-a", "state": "active",
		"cameras": []map[string]any{
			{"name": "cam0", "state": "active", "outcome": "recording"},
			{"name": "cam1", "state": "staged", "outcome": "could not be created here: camera refused"},
		},
	})

	view, err := rig.svc.Activate(ctx, plan.Id, 1, false)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if len(view.Cameras) != 2 {
		t.Fatalf("the takeover reported %d camera(s) to the caller", len(view.Cameras))
	}
	byName := map[string]FailoverCameraView{}
	for _, c := range view.Cameras {
		byName[c.Name] = c
	}
	if byName["cam0"].Outcome != "recording" {
		t.Fatalf("the camera that started recording reported %q", byName["cam0"].Outcome)
	}
	if !strings.Contains(byName["cam1"].Outcome, "could not be created") {
		t.Fatalf("the camera that failed reported %q", byName["cam1"].Outcome)
	}
}

// The same for the drill: the verdict per camera is what sends somebody to the right place,
// and it must reach the caller of the drill rather than only the next page load.
func TestFailoverDrillReturnsThePerCameraVerdict(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	plan := rig.plan(t, nil)
	rig.stageable(2)
	if _, err := rig.svc.Stage(ctx, plan.Id, 1); err != nil {
		t.Fatalf("stage: %v", err)
	}
	rig.sender.reply("node-b POST /api/standby/node-a/drill", map[string]any{
		"sourceNodeId": "node-a", "readiness": "partial", "reachable": 1, "total": 2,
		"cameras": []map[string]any{
			{"name": "cam0", "host": "10.0.0.1", "checkStatus": "ok"},
			{"name": "cam1", "host": "10.0.0.2", "checkStatus": "unauthorized", "checkDetail": "401"},
		},
	})
	view, err := rig.svc.Drill(ctx, plan.Id, 1)
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	if len(view.Cameras) != 2 {
		t.Fatalf("the drill reported %d camera(s) to the caller", len(view.Cameras))
	}
	for _, c := range view.Cameras {
		if c.Name == "cam1" && (c.CheckStatus != "unauthorized" || c.CheckDetail != "401") {
			t.Fatalf("the camera that rejected the login reported %+v", c)
		}
	}
}

// THE SECOND DEFECT THE SCREEN PASS FOUND, kept as a unit test.
//
// "Never drilled" is not "overdue". `now - LastDrillAt` on a plan that has never been
// drilled is fifty-five years, so the sweep drilled every new plan on its first tick — and
// an operator watched the badge they had just seen say "never tested" turn green by itself,
// beside a sentence telling them to press Test. The distinction between COPIED and PROVED,
// which is the entire feature, became invisible in ordinary use.
func TestFailoverSweepDoesNotDrillAPlanTheMomentItIsMade(t *testing.T) {
	ctx := context.Background()
	rig := newFailoverRig()
	rig.plan(t, nil)
	rig.stageable(2)
	rig.sender.reply("node-b POST /api/standby/node-a/drill",
		map[string]any{"sourceNodeId": "node-a", "readiness": "ready", "reachable": 2, "total": 2})

	// The first sweep is allowed to COPY — that is what an enabled plan is for, and nothing
	// goes into service because of it.
	rig.svc.Sweep(ctx)
	if !rig.sender.called("node-b POST /api/standby/stage") {
		t.Fatal("the first sweep did not copy the camera set")
	}
	if rig.sender.called("node-b POST /api/standby/node-a/drill") {
		t.Fatal("a plan was drilled within seconds of being created")
	}
	views, _ := rig.svc.List(ctx)
	if views[0].Ready || views[0].ReadyState != FailoverReadyUntested {
		t.Fatalf("a freshly created plan already reads ready=%v state=%q",
			views[0].Ready, views[0].ReadyState)
	}

	// Once it has been staged long enough, the unattended drill IS the backstop for the
	// plan nobody revisits — so it must still happen.
	saved := rig.repo.only(t)
	saved.LastStagedAt = time.Now().Unix() - int64(failoverFirstDrillDelay.Seconds()) - 60
	rig.svc.Sweep(ctx)
	if !rig.sender.called("node-b POST /api/standby/node-a/drill") {
		t.Fatal("a plan staged well past the first-drill delay was never drilled")
	}
}
