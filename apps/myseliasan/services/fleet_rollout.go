package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/control"
)

// Staged version rollout (W2-5, finding F-07).
//
// Upgrading a fleet by pressing "update" on fifty appliances at once is how an estate
// discovers a bad build all at the same time. This updates them a RING at a time — a canary
// first — and every ring has to prove itself before the next one is allowed to start.
//
// WHAT "PROVE ITSELF" MEANS HERE, and why it is not just "the command returned 200". The
// node accepts the update, downloads it, swaps its binary and restarts, so a successful
// reply means only that it agreed to try. A ring passes when every node in it has:
//
//	1. come back on the control channel, and
//	2. REPORTED the target version itself (not been assumed to have it), and
//	3. held that state for the settle window.
//
// Each of those catches a different way an upgrade goes wrong: (1) a node that never comes
// back, (2) a node that came back on the OLD version because the swap silently failed, and
// (3) a node that boots, looks healthy, and dies a minute later. Checking only the first is
// the version of this feature that reports success while the fleet burns.
//
// HALT, NOT ROLLBACK — and this is a deliberate departure from the plan's sketch. Rolling a
// binary back is easy; rolling a DATABASE back is not, because migrations in this suite are
// forward-only (infra/db/bootstrap.Migration has no down step). An automatic rollback would
// therefore take a node that has already migrated its schema and hand it a binary that has
// never seen it — turning a bad release into a corrupted appliance, unattended, on every
// node in the ring at once. So a failed ring STOPS the rollout with a reason, leaves the
// rest of the fleet untouched on the version it was already running, and waits for a human.
// The fleet is recovered by rolling FORWARD to a fixed build, which this same machinery does.

// Rollout lifecycle states.
const (
	// RolloutStateDraft is planned but not started; the plan can still be discarded.
	RolloutStateDraft = "draft"
	// RolloutStateRunning is actively working a ring.
	RolloutStateRunning = "running"
	// RolloutStateHalted means a ring failed its health gate. Nothing further is attempted.
	RolloutStateHalted = "halted"
	// RolloutStateCompleted means every ring passed.
	RolloutStateCompleted = "completed"
	// RolloutStateCancelled means an operator stopped it. Nodes already updated stay updated.
	RolloutStateCancelled = "cancelled"
)

// Per-node states within a rollout.
const (
	RolloutNodePending = "pending"
	// RolloutNodeUpdating means the node accepted the update command and is applying it.
	RolloutNodeUpdating = "updating"
	// RolloutNodeSucceeded means the node came back reporting the target version.
	RolloutNodeSucceeded = "succeeded"
	// RolloutNodeFailed means it did not, in time, or refused the command.
	RolloutNodeFailed = "failed"
	// RolloutNodeUnsupported means the node cannot self-update at all — a container image
	// or a package-managed install, where replacing the binary in place is either undone on
	// restart or fights the package manager. Such a node is EXCLUDED from the rings rather
	// than left in one to fail: a plan that can only halt on its first ring is not a plan,
	// and finding this out from a halted canary teaches the operator nothing they could not
	// have been told up front.
	RolloutNodeUnsupported = "unsupported"
	// RolloutNodeSkipped means it was already on the target version when its turn came.
	// Distinct from succeeded on purpose: this rollout did not upgrade it, and a report
	// that claims credit for work it did not do misleads whoever reads it next.
	RolloutNodeSkipped = "skipped"
)

const (
	// defaultRolloutRingSize keeps the first ring small. The canary is the point.
	defaultRolloutRingSize = 1
	// defaultRolloutSettleSeconds is how long a ring must hold healthy before the next
	// one starts.
	defaultRolloutSettleSeconds = 300
	// defaultRolloutNodeTimeoutSeconds bounds one node's whole update — download, swap,
	// restart, reconnect, report. Generous: an appliance on a slow link genuinely takes
	// minutes, and calling a slow node failed halts a rollout that was fine.
	defaultRolloutNodeTimeoutSeconds = 900
	// rolloutUpdatePath is the node endpoint that applies a pinned version.
	rolloutUpdatePath = "/api/system/update/apply"
	// rolloutStatusPath is the node endpoint that reports whether it can self-update at all.
	rolloutStatusPath = "/api/system/update"
)

// ErrRolloutNotFound is returned for an unknown rollout id.
var ErrRolloutNotFound = errors.New("rollout not found")

// RolloutPlanRequest is what an operator asks for.
type RolloutPlanRequest struct {
	TargetVersion string `json:"targetVersion"`
	RingSize      int    `json:"ringSize"`
	SettleSeconds int    `json:"settleSeconds"`
	// NodeTimeoutSeconds bounds one node's update; 0 takes the default.
	NodeTimeoutSeconds int `json:"nodeTimeoutSeconds"`
	// SiteId restricts the rollout to one site (0 = the whole fleet).
	SiteId int64  `json:"siteId"`
	Note   string `json:"note"`
}

// RolloutView is a rollout plus its nodes, for the API.
type RolloutView struct {
	*entities.FleetRollout
	Nodes []*entities.RolloutNode `json:"nodes"`
	// Counts is state → number of nodes, for a header line that does not need the list.
	Counts map[string]int `json:"counts"`
}

// rolloutNodeLister is the slice of the registry a rollout needs: the fleet, and nothing
// that could release a node.
type rolloutNodeLister interface {
	List(ctx context.Context) ([]*entities.ManagedNode, error)
}

// IFleetRolloutService is the rollout surface.
type IFleetRolloutService interface {
	Plan(ctx context.Context, req RolloutPlanRequest, by int64) (*RolloutView, error)
	Start(ctx context.Context, id int64) (*RolloutView, error)
	Cancel(ctx context.Context, id int64, reason string) (*RolloutView, error)
	Get(ctx context.Context, id int64) (*RolloutView, error)
	List(ctx context.Context) ([]*entities.FleetRollout, error)
	// Advance drives the active rollout one step. Safe to call on a timer.
	Advance(ctx context.Context) error
}

// FleetRolloutService plans and drives staged upgrades.
type FleetRolloutService struct {
	rollouts dbsql.IGenericRepo[entities.FleetRollout]
	members  dbsql.IGenericRepo[entities.RolloutNode]
	nodes    rolloutNodeLister
	sender   ControlSender
	presence func(nodeID string) bool
	audit    IAuditService
	logf     func(string, ...any)
	now      func() time.Time

	// driving serialises Advance. The timer and an operator's action can both call it, and
	// two drivers working one rollout would ask the same node to update twice.
	driving sync.Mutex
}

// NewFleetRolloutService builds the service. presence reports whether a node currently
// holds a live control channel; audit and logf are optional.
func NewFleetRolloutService(db dbsql.IDbCrud, nodes rolloutNodeLister, sender ControlSender, presence func(string) bool, audit IAuditService, logf func(string, ...any)) *FleetRolloutService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &FleetRolloutService{
		rollouts: dbsql.NewGenericRepo[entities.FleetRollout](db),
		members:  dbsql.NewGenericRepo[entities.RolloutNode](db),
		nodes:    nodes,
		sender:   sender,
		presence: presence,
		audit:    audit,
		logf:     logf,
		now:      time.Now,
	}
}

// Plan works out the rings and records the rollout in draft.
//
// Ring composition is deterministic — nodes sorted by id — rather than random or
// "whatever the database returned". A rollout an operator can predict is one they can
// schedule around, and re-planning the same fleet twice producing different canaries would
// make every comparison between two rollouts meaningless.
func (s *FleetRolloutService) Plan(ctx context.Context, req RolloutPlanRequest, by int64) (*RolloutView, error) {
	target := strings.TrimPrefix(strings.TrimSpace(req.TargetVersion), "v")
	if target == "" {
		return nil, errors.New("a target version is required")
	}
	if active, err := s.activeRollout(ctx); err != nil {
		return nil, err
	} else if active != nil {
		// One at a time. Two rollouts driving the same fleet would fight over which version
		// each node should be on, and the loser would be whichever finished second.
		return nil, fmt.Errorf("rollout %d is already %s; finish or cancel it first", active.Id, active.State)
	}

	all, err := s.nodes.List(ctx)
	if err != nil {
		return nil, err
	}
	candidates := make([]*entities.ManagedNode, 0, len(all))
	for _, n := range all {
		if n == nil || !nodeKindHoldsSightings(n.Kind) {
			// Only camera nodes have the self-update primitive this drives.
			continue
		}
		if req.SiteId > 0 && n.SiteId != req.SiteId {
			continue
		}
		candidates = append(candidates, n)
	}
	if len(candidates) == 0 {
		return nil, errors.New("no updatable nodes match this plan")
	}
	sort.SliceStable(candidates, func(a, b int) bool { return candidates[a].NodeId < candidates[b].NodeId })

	// Ask each reachable node whether it can replace its own binary at all, BEFORE placing
	// it in a ring. A container-image or package-managed install cannot, and including one
	// guarantees the rollout halts on whichever ring it lands in — teaching the operator
	// something they could have been told while planning. A node that cannot be reached is
	// included: it may well be updatable, and refusing to plan around a node that is merely
	// offline right now would make rollouts impossible on any real estate.
	updatable := make([]*entities.ManagedNode, 0, len(candidates))
	blocked := make([]*entities.ManagedNode, 0)
	reasons := map[string]string{}
	for _, n := range candidates {
		ok, why := s.canSelfUpdate(ctx, n.NodeId)
		if ok {
			updatable = append(updatable, n)
			continue
		}
		blocked = append(blocked, n)
		reasons[n.NodeId] = why
	}
	if len(updatable) == 0 {
		return nil, fmt.Errorf("none of the %d matching nodes can replace their own binary (%s)",
			len(candidates), reasons[blocked[0].NodeId])
	}

	ringSize := req.RingSize
	if ringSize <= 0 {
		ringSize = defaultRolloutRingSize
	}
	settle := req.SettleSeconds
	if settle <= 0 {
		settle = defaultRolloutSettleSeconds
	}
	nodeTimeout := req.NodeTimeoutSeconds
	if nodeTimeout <= 0 {
		nodeTimeout = defaultRolloutNodeTimeoutSeconds
	}
	ringCount := (len(updatable) + ringSize - 1) / ringSize

	now := s.now().UTC().Unix()
	rollout := entities.FleetRollout{
		TargetVersion:      target,
		NodeKind:           "camera",
		State:              RolloutStateDraft,
		RingSize:           ringSize,
		RingCount:          ringCount,
		SettleSeconds:      settle,
		NodeTimeoutSeconds: nodeTimeout,
		Note:               strings.TrimSpace(req.Note),
		CreatedBy:          by,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	id, err := s.rollouts.Create(ctx, "", rollout)
	if err != nil {
		return nil, err
	}
	rollout.Id = int64(id)

	for i, n := range updatable {
		member := entities.RolloutNode{
			RolloutId: rollout.Id,
			NodeId:    n.NodeId,
			NodeName:  nodeLabel(n),
			SiteId:    n.SiteId,
			Ring:      i/ringSize + 1,
			State:     RolloutNodePending,
		}
		if _, err := s.members.Create(ctx, "", member); err != nil {
			return nil, err
		}
	}
	// Ring 0 is "not in any ring". These rows exist so the plan SAYS which appliances it is
	// leaving behind and why — a rollout that silently covered nine nodes out of twelve
	// would report complete success over a third of the fleet it never touched.
	for _, n := range blocked {
		member := entities.RolloutNode{
			RolloutId: rollout.Id,
			NodeId:    n.NodeId,
			NodeName:  nodeLabel(n),
			SiteId:    n.SiteId,
			Ring:      0,
			State:     RolloutNodeUnsupported,
			Detail:    reasons[n.NodeId],
		}
		if _, err := s.members.Create(ctx, "", member); err != nil {
			return nil, err
		}
	}
	s.record(ctx, "fleet.rollout.plan", rollout.Id, "success",
		fmt.Sprintf("planned %s across %d nodes in %d rings (%d cannot self-update)", target, len(updatable), ringCount, len(blocked)), by)
	return s.Get(ctx, rollout.Id)
}

// canSelfUpdate asks one node whether it is able to replace its own binary.
//
// A node that cannot be reached is treated as CAPABLE. That is the deliberate direction:
// the alternative — excluding every node that happens to be offline while an operator is
// planning — would quietly shrink a rollout to whichever appliances were awake at that
// moment, and report full coverage of the fleet it did reach. An unreachable node that
// turns out to be un-updatable fails its own ring later, loudly, with its own reason.
func (s *FleetRolloutService) canSelfUpdate(ctx context.Context, nodeID string) (bool, string) {
	if s.sender == nil {
		return true, ""
	}
	if s.presence != nil && !s.presence(nodeID) {
		return true, ""
	}
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := s.sender.SendRequest(reqCtx, nodeID, control.Request{
		Method: http.MethodGet,
		Path:   rolloutStatusPath,
		Role:   "admin",
		Actor:  "fleet-rollout",
	})
	if err != nil || resp.Status < 200 || resp.Status >= 300 {
		return true, "" // could not ask; judged later on its own ring
	}
	var envelope struct {
		Result struct {
			CanSelfUpdate bool   `json:"canSelfUpdate"`
			Managed       string `json:"managed"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		return true, ""
	}
	if envelope.Result.CanSelfUpdate {
		return true, ""
	}
	why := "this node cannot replace its own binary"
	switch envelope.Result.Managed {
	case "docker":
		why = "runs from a container image — upgrade it by deploying a new image"
	case "package":
		why = "installed by a package manager — upgrade it through the package manager"
	default:
		why = "cannot replace its own binary (its application directory is not writable)"
	}
	return false, why
}

// Start moves a draft rollout to running. The first Advance does the work.
func (s *FleetRolloutService) Start(ctx context.Context, id int64) (*RolloutView, error) {
	rollout, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if rollout.State != RolloutStateDraft {
		return nil, fmt.Errorf("rollout %d is %s, not draft", id, rollout.State)
	}
	now := s.now().UTC().Unix()
	rollout.State = RolloutStateRunning
	rollout.CurrentRing = 1
	rollout.StartedAt = now
	rollout.UpdatedAt = now
	if _, err := s.rollouts.UpdateById(ctx, "", *rollout); err != nil {
		return nil, err
	}
	s.record(ctx, "fleet.rollout.start", id, "success", "started rollout to "+rollout.TargetVersion, 0)
	if err := s.Advance(ctx); err != nil {
		s.logf("rollout %d: first advance: %v", id, err)
	}
	return s.Get(ctx, id)
}

// Cancel stops a rollout. Nodes already updated stay updated — there is no undo, and
// pretending otherwise is the dangerous half of this feature.
func (s *FleetRolloutService) Cancel(ctx context.Context, id int64, reason string) (*RolloutView, error) {
	rollout, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if rollout.State == RolloutStateCompleted || rollout.State == RolloutStateCancelled {
		return nil, fmt.Errorf("rollout %d is already %s", id, rollout.State)
	}
	now := s.now().UTC().Unix()
	rollout.State = RolloutStateCancelled
	rollout.HaltReason = strings.TrimSpace(reason)
	rollout.FinishedAt = now
	rollout.UpdatedAt = now
	if _, err := s.rollouts.UpdateById(ctx, "", *rollout); err != nil {
		return nil, err
	}
	s.record(ctx, "fleet.rollout.cancel", id, "success", strings.TrimSpace(reason), 0)
	return s.Get(ctx, id)
}

// Get returns a rollout with its nodes.
func (s *FleetRolloutService) Get(ctx context.Context, id int64) (*RolloutView, error) {
	rollout, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	members, err := s.membersOf(ctx, id)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, m := range members {
		counts[m.State]++
	}
	return &RolloutView{FleetRollout: rollout, Nodes: members, Counts: counts}, nil
}

// List returns every rollout, newest first.
func (s *FleetRolloutService) List(ctx context.Context) ([]*entities.FleetRollout, error) {
	rows, _, err := s.rollouts.Get(ctx, "", 200, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.DESC}})
	return rows, err
}

// Advance drives the running rollout one step: ask the current ring's pending nodes to
// update, judge the ones already asked, then decide whether the ring has passed.
//
// It is written to be called repeatedly on a timer and to hold no state between calls —
// everything it needs is on the rows. A control plane that restarts mid-rollout picks up
// exactly where it left off, including a half-elapsed settle window.
func (s *FleetRolloutService) Advance(ctx context.Context) error {
	s.driving.Lock()
	defer s.driving.Unlock()

	// A ring that passes lets the next one start in the SAME pass rather than waiting for
	// the next tick. Otherwise every ring boundary silently costs one tick of delay, which
	// on a fifty-node estate is most of the rollout's wall clock spent doing nothing. The
	// bound is the ring count: each iteration either finishes the work it can do now or
	// moves the ring on exactly once, so it cannot spin.
	for i := 0; i <= maxRolloutStepsPerPass; i++ {
		moved, err := s.advanceOnce(ctx)
		if err != nil || !moved {
			return err
		}
	}
	return nil
}

// maxRolloutStepsPerPass bounds how many ring boundaries one Advance may cross. A fleet
// where every node is already on the target can legitimately walk several rings in a pass.
const maxRolloutStepsPerPass = 64

// advanceOnce does one pass over the current ring. It reports whether it moved the rollout
// on to a NEW ring, which is the only case where there is immediately more work to do.
func (s *FleetRolloutService) advanceOnce(ctx context.Context) (bool, error) {
	rollout, err := s.activeRollout(ctx)
	if err != nil || rollout == nil {
		return false, err
	}
	if rollout.State != RolloutStateRunning {
		return false, nil
	}
	members, err := s.membersOf(ctx, rollout.Id)
	if err != nil {
		return false, err
	}
	ring := make([]*entities.RolloutNode, 0, rollout.RingSize)
	for _, m := range members {
		if m.Ring == rollout.CurrentRing {
			ring = append(ring, m)
		}
	}
	if len(ring) == 0 {
		return false, s.finish(ctx, rollout, RolloutStateCompleted, "")
	}

	live := s.nodeVersions(ctx)
	now := s.now().UTC().Unix()

	for _, m := range ring {
		switch m.State {
		case RolloutNodePending:
			s.askNode(ctx, rollout, m, live, now)
		case RolloutNodeUpdating:
			s.judgeNode(ctx, rollout, m, live, now)
		}
	}

	// Re-read the ring's states after this pass.
	failed := 0
	settled := 0
	for _, m := range ring {
		switch m.State {
		case RolloutNodeFailed:
			failed++
		case RolloutNodeSucceeded, RolloutNodeSkipped:
			settled++
		}
	}
	if failed > 0 {
		reason := fmt.Sprintf("ring %d: %d of %d nodes did not reach %s", rollout.CurrentRing, failed, len(ring), rollout.TargetVersion)
		for _, m := range ring {
			if m.State == RolloutNodeFailed && m.Detail != "" {
				reason += " — " + m.NodeName + ": " + m.Detail
				break
			}
		}
		return false, s.finish(ctx, rollout, RolloutStateHalted, reason)
	}
	if settled < len(ring) {
		return false, nil // still working
	}

	// Every node in the ring is on the target version. Now the settle window: an upgrade
	// that boots and dies a minute later is indistinguishable from a good one until time
	// passes, so the next ring waits.
	if rollout.RingReadyAt == 0 {
		rollout.RingReadyAt = now
		rollout.UpdatedAt = now
		_, err := s.rollouts.UpdateById(ctx, "", *rollout)
		return false, err
	}
	// A node that dropped off the channel DURING the settle window has not settled — it
	// has failed late, which is exactly what the window is for.
	for _, m := range ring {
		if m.State != RolloutNodeSucceeded {
			continue
		}
		if s.presence != nil && !s.presence(m.NodeId) {
			m.State = RolloutNodeFailed
			m.Detail = "dropped off the control channel during the settle window"
			m.FinishedAt = now
			_, _ = s.members.UpdateById(ctx, "", *m)
			return false, s.finish(ctx, rollout, RolloutStateHalted,
				fmt.Sprintf("ring %d: %s went offline after updating", rollout.CurrentRing, m.NodeName))
		}
		if v, ok := live[m.NodeId]; ok && v != "" && v != rollout.TargetVersion {
			m.State = RolloutNodeFailed
			m.Detail = "reverted to " + v + " during the settle window"
			m.FinishedAt = now
			_, _ = s.members.UpdateById(ctx, "", *m)
			return false, s.finish(ctx, rollout, RolloutStateHalted,
				fmt.Sprintf("ring %d: %s is no longer on %s", rollout.CurrentRing, m.NodeName, rollout.TargetVersion))
		}
	}
	if now-rollout.RingReadyAt < int64(rollout.SettleSeconds) {
		return false, nil // still settling
	}

	if rollout.CurrentRing >= rollout.RingCount {
		return false, s.finish(ctx, rollout, RolloutStateCompleted, "")
	}
	rollout.CurrentRing++
	rollout.RingReadyAt = 0
	rollout.UpdatedAt = now
	_, err = s.rollouts.UpdateById(ctx, "", *rollout)
	s.logf("rollout %d: ring %d passed, starting ring %d", rollout.Id, rollout.CurrentRing-1, rollout.CurrentRing)
	return err == nil, err
}

// askNode sends one node its update command.
func (s *FleetRolloutService) askNode(ctx context.Context, rollout *entities.FleetRollout, m *entities.RolloutNode, live map[string]string, now int64) {
	current := live[m.NodeId]
	m.FromVersion = current
	m.AskedAt = now

	// Already there. Recorded as skipped rather than succeeded: this rollout did not move
	// it, and a report that claims otherwise is a report that overstates what was tested.
	if current != "" && current == rollout.TargetVersion {
		m.State = RolloutNodeSkipped
		m.Detail = "already on " + rollout.TargetVersion
		m.FinishedAt = now
		_, _ = s.members.UpdateById(ctx, "", *m)
		return
	}
	if s.presence != nil && !s.presence(m.NodeId) {
		// Not connected, so it cannot be asked. Left PENDING rather than failed: a node
		// that is briefly offline when its turn comes should get the next tick, and the
		// per-node timeout is what eventually calls it.
		if m.AskedAt > 0 && now-m.AskedAt > int64(rollout.NodeTimeoutSeconds) {
			s.failNode(ctx, m, "node was not connected within the update window", now)
		}
		_, _ = s.members.UpdateById(ctx, "", *m)
		return
	}

	body, _ := json.Marshal(map[string]string{"version": rollout.TargetVersion})
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := s.sender.SendRequest(reqCtx, m.NodeId, control.Request{
		Method:  http.MethodPost,
		Path:    rolloutUpdatePath,
		Role:    "admin",
		Actor:   "fleet-rollout",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    body,
	})
	switch {
	case err != nil:
		s.failNode(ctx, m, "could not send the update command: "+err.Error(), now)
		return
	case resp.Status < 200 || resp.Status >= 300:
		s.failNode(ctx, m, fmt.Sprintf("node refused the update (HTTP %d): %s", resp.Status, nodeErrorLine(resp.Body)), now)
		return
	}
	m.State = RolloutNodeUpdating
	_, _ = s.members.UpdateById(ctx, "", *m)
	s.logf("rollout %d: asked %s to install %s", rollout.Id, m.NodeName, rollout.TargetVersion)
}

// judgeNode decides whether a node that was asked to update has arrived.
//
// The test is that the node REPORTS the target version on its own control-channel hello.
// Nothing here trusts the command's 200, and nothing infers success from the node merely
// being reachable: a failed swap leaves a perfectly healthy appliance running the old
// build, which is the failure this whole ring exists to catch.
func (s *FleetRolloutService) judgeNode(ctx context.Context, rollout *entities.FleetRollout, m *entities.RolloutNode, live map[string]string, now int64) {
	if v, ok := live[m.NodeId]; ok && v == rollout.TargetVersion {
		if s.presence != nil && !s.presence(m.NodeId) {
			return // reported it, but is not back on the channel yet
		}
		m.State = RolloutNodeSucceeded
		m.FinishedAt = now
		_, _ = s.members.UpdateById(ctx, "", *m)
		return
	}
	if m.AskedAt > 0 && now-m.AskedAt > int64(rollout.NodeTimeoutSeconds) {
		got := live[m.NodeId]
		detail := fmt.Sprintf("did not report %s within %ds", rollout.TargetVersion, rollout.NodeTimeoutSeconds)
		if got != "" && got != rollout.TargetVersion {
			// The most informative failure there is: it came back, and it came back on the
			// version it started on. That is a swap that did not take, not a node that died.
			detail = fmt.Sprintf("came back running %s, not %s", got, rollout.TargetVersion)
		}
		s.failNode(ctx, m, detail, now)
	}
}

func (s *FleetRolloutService) failNode(ctx context.Context, m *entities.RolloutNode, detail string, now int64) {
	m.State = RolloutNodeFailed
	m.Detail = detail
	m.FinishedAt = now
	_, _ = s.members.UpdateById(ctx, "", *m)
}

// finish closes a rollout in a terminal state.
func (s *FleetRolloutService) finish(ctx context.Context, rollout *entities.FleetRollout, state, reason string) error {
	now := s.now().UTC().Unix()
	rollout.State = state
	rollout.HaltReason = reason
	rollout.FinishedAt = now
	rollout.UpdatedAt = now
	if _, err := s.rollouts.UpdateById(ctx, "", *rollout); err != nil {
		return err
	}
	outcome := "success"
	if state == RolloutStateHalted {
		outcome = "error"
		s.logf("rollout %d HALTED: %s", rollout.Id, reason)
	}
	s.record(ctx, "fleet.rollout."+state, rollout.Id, outcome, reason, 0)
	return nil
}

// activeRollout returns the rollout that is draft or running, or nil.
func (s *FleetRolloutService) activeRollout(ctx context.Context) (*entities.FleetRollout, error) {
	rows, _, err := s.rollouts.Get(ctx, "", 50, 0,
		[]sqldataenums.Filter{{FieldName: "State", Compare: sqldataenums.In, Value: []string{RolloutStateDraft, RolloutStateRunning}}},
		[]sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.DESC}})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r != nil {
			return r, nil
		}
	}
	return nil, nil
}

func (s *FleetRolloutService) load(ctx context.Context, id int64) (*entities.FleetRollout, error) {
	rows, _, err := s.rollouts.Get(ctx, "", 1, 0,
		[]sqldataenums.Filter{{FieldName: "Id", Compare: sqldataenums.Equal, Value: id}}, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil {
		return nil, ErrRolloutNotFound
	}
	return rows[0], nil
}

func (s *FleetRolloutService) membersOf(ctx context.Context, id int64) ([]*entities.RolloutNode, error) {
	rows, _, err := s.members.Get(ctx, "", 1000, 0,
		[]sqldataenums.Filter{{FieldName: "RolloutId", Compare: sqldataenums.Equal, Value: id}},
		[]sqldataenums.Sorter{{FieldName: "Ring", Sort: sqldataenums.ASC}, {FieldName: "Id", Sort: sqldataenums.ASC}})
	return rows, err
}

// nodeVersions maps node id → the version it last REPORTED. A node that has never told us
// is absent rather than present-and-empty, so "unknown" cannot be mistaken for "old".
func (s *FleetRolloutService) nodeVersions(ctx context.Context) map[string]string {
	out := map[string]string{}
	all, err := s.nodes.List(ctx)
	if err != nil {
		return out
	}
	for _, n := range all {
		if n != nil && strings.TrimSpace(n.Version) != "" {
			out[n.NodeId] = strings.TrimPrefix(strings.TrimSpace(n.Version), "v")
		}
	}
	return out
}

func (s *FleetRolloutService) record(ctx context.Context, action string, id int64, outcome, detail string, by int64) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, AuditEntry{
		Action:     action,
		ActorId:    by,
		TargetType: "rollout",
		TargetId:   fmt.Sprintf("%d", id),
		Outcome:    outcome,
		Detail:     detail,
	})
}

// nodeErrorLine turns a node's error response into something a halt reason can carry.
//
// The node answers errors as {"statsCode":400,"message":"..."} — so pasting the raw body
// into the halt reason puts JSON in front of an operator who is trying to find out why
// their fleet upgrade stopped. The message is the part they need; the envelope is noise.
func nodeErrorLine(body []byte) string {
	var envelope struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Message) != "" {
		body = []byte(envelope.Message)
	}
	text := strings.TrimSpace(string(body))
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}
